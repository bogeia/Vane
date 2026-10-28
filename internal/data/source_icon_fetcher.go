package data

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mmcdole/gofeed"
	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/fetch"
	webassets "github.com/bogeia/vane/web"
)

type sourceIconFetcher struct{ client *fetch.Client }

func NewSourceIconFetcher(cfg config.Config) biz.SourceIconFetcher {
	return &sourceIconFetcher{client: fetch.New(false, cfg.Fetch)}
}

// Reviewed publisher artwork is shared with topic branding; matching is by publisher URL, never the feed host's platform logo.
func publisherArtwork(source biz.Source) string {
	u, err := url.Parse(source.FeedURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if source.Kind == biz.SourceKindHFModels {
		if host != "huggingface.co" {
			return ""
		}
		org := strings.ToLower(strings.Split(strings.Trim(u.Path, "/"), "/")[0])
		return map[string]string{"deepseek-ai": "companies/deepseek.svg", "qwen": "companies/qwen.svg", "minimaxai": "companies/minimax.svg", "zai-org": "companies/zhipu.svg", "thudm": "companies/zhipu.svg", "moonshotai": "companies/kimi.svg"}[org]
	}
	for _, entry := range []struct{ host, path string }{
		{"openai.com", "companies/openai.svg"},
		{"nvidia.com", "companies/nvidia.svg"},
		{"huggingface.co", "companies/hugging-face.svg"},
		{"deepmind.google", "sources/deepmind-color.svg"},
		{"research.google", "sources/google-color.svg"},
		{"blog.google", "sources/google-color.svg"},
		{"microsoft.com", "sources/microsoft-color.svg"},
		{"aws.amazon.com", "sources/aws-color.svg"},
		{"github.blog", "sources/github.svg"},
		{"mistral.ai", "sources/mistral-color.svg"},
		{"cursor.com", "companies/cursor.svg"},
		{"openrouter.ai", "companies/openrouter.svg"},
		{"anthropic.com", "companies/anthropic.svg"},
		{"kimi.com", "companies/kimi.svg"},
		{"bair.berkeley.edu", "sources/bair.png"},
		{"theverge.com", "sources/the-verge.png"},
		{"techcrunch.com", "sources/techcrunch.png"},
		{"arstechnica.com", "sources/ars-technica.png"},
		{"technologyreview.com", "sources/mit-tech-review.ico"},
		{"the-decoder.com", "sources/the-decoder.png"},
		{"simonwillison.net", "sources/simon-willison.ico"},
		{"latent.space", "sources/latent-space.png"},
		{"pytorch.org", "sources/pytorch.webp"},
		{"amazon.science", "sources/amazon-science.png"},
		{"news.mit.edu", "sources/mit-news.ico"},
		{"qbitai.com", "sources/qbitai.ico"},
		{"importai.substack.com", "sources/import-ai.png"},
	} {
		if host == entry.host || strings.HasSuffix(host, "."+entry.host) {
			return entry.path
		}
	}
	return ""
}

type iconCandidate struct {
	URL      string
	Priority int
}

func (f *sourceIconFetcher) DiscoverIcon(ctx context.Context, source biz.Source) (biz.SourceIcon, error) {
	if source.IconURL != "" {
		return f.download(ctx, source.IconURL)
	}
	if asset := publisherArtwork(source); asset != "" {
		content, err := webassets.Logos.ReadFile("public/logos/" + asset)
		if err != nil {
			return biz.SourceIcon{}, err
		}
		return biz.SourceIcon{Content: content, ContentType: map[string]string{".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".webp": "image/webp"}[path.Ext(asset)]}, nil
	}
	candidates := []iconCandidate{}
	add := func(base, raw string, priority int) {
		b, err := url.Parse(base)
		if err != nil {
			return
		}
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || raw == "" {
			return
		}
		resolved := b.ResolveReference(u)
		if (resolved.Scheme != "https" && resolved.Scheme != "http") || resolved.User != nil {
			return
		}
		for _, c := range candidates {
			if c.URL == resolved.String() {
				return
			}
		}
		candidates = append(candidates, iconCandidate{URL: resolved.String(), Priority: priority})
	}
	if source.Kind == biz.SourceKindHFModels {
		u, err := url.Parse(source.FeedURL)
		if err != nil || u.Hostname() != "huggingface.co" {
			return biz.SourceIcon{}, fmt.Errorf("invalid model publisher URL")
		}
		org := strings.Split(strings.Trim(u.Path, "/"), "/")[0]
		response, err := f.client.Get(ctx, "https://huggingface.co/api/organizations/"+url.PathEscape(org)+"/overview", nil)
		if err != nil {
			return biz.SourceIcon{}, err
		}
		var profile struct {
			AvatarURL string `json:"avatarUrl"`
		}
		if err := json.Unmarshal(response.Body, &profile); err != nil {
			return biz.SourceIcon{}, fmt.Errorf("invalid publisher profile")
		}
		add(response.URL, profile.AvatarURL, 0)
	} else {
		homepage := ""
		response, err := f.client.Get(ctx, source.FeedURL, nil)
		if err == nil {
			if source.Kind == biz.SourceKindRSS {
				if feed, err := gofeed.NewParser().Parse(bytes.NewReader(response.Body)); err == nil {
					if feed.Image != nil {
						add(response.URL, feed.Image.URL, 512)
					}
					if link, err := url.Parse(feed.Link); err == nil && feed.Link != "" {
						base, _ := url.Parse(response.URL)
						homepage = base.ResolveReference(link).String()
					}
				}
			} else {
				homepage = response.URL
				f.pageCandidates(response.Body, response.URL, add)
			}
		}
		// Feed services do not represent the publisher. Only an explicit feed homepage can redirect discovery to it.
		if homepage == "" {
			u, err := url.Parse(source.FeedURL)
			if err == nil && !hostedFeed(u.Hostname()) {
				homepage = u.Scheme + "://" + u.Host
			}
		}
		if homepage != "" {
			beforePage := len(candidates)
			page, err := f.client.Get(ctx, homepage, nil)
			if err == nil {
				homepage = page.URL
				f.pageCandidates(page.Body, page.URL, add)
			}
			if u, err := url.Parse(homepage); err == nil {
				root := u.Scheme + "://" + u.Host
				if len(candidates) == beforePage && strings.TrimRight(homepage, "/") != root {
					if page, err := f.client.Get(ctx, root, nil); err == nil {
						f.pageCandidates(page.Body, page.URL, add)
					}
				}
				add(homepage, root+"/favicon.ico", 0)
			}
		}
	}
	slices.SortStableFunc(candidates, func(a, b iconCandidate) int {
		if a.Priority == b.Priority {
			return 0
		}
		if betterIcon(a.Priority, b.Priority) {
			return -1
		}
		return 1
	})
	var best biz.SourceIcon
	bestQuality := -1
	for index, candidate := range candidates {
		if index >= 20 || ctx.Err() != nil {
			break
		}
		icon, err := f.download(ctx, candidate.URL)
		if err != nil {
			continue
		}
		quality, err := iconQuality(icon.Content, icon.ContentType)
		if err != nil {
			continue
		}
		if betterIcon(quality, bestQuality) {
			best, bestQuality = icon, quality
		}
	}
	if bestQuality < 0 {
		return biz.SourceIcon{}, fmt.Errorf("no safe publisher icon found")
	}
	return best, nil
}

// Prefer vectors, then the smallest raster meeting the display target, then the largest fallback.
func betterIcon(size, current int) bool {
	if current < 0 {
		return true
	}
	if size == 1000000 || current == 1000000 {
		return size > current
	}
	if size >= 64 {
		return current < 64 || size < current
	}
	return current < 64 && size > current
}

func hostedFeed(host string) bool {
	for _, platform := range []string{"feedburner.com", "feedproxy.google.com", "rsshub.app", "rsshub.rssforever.com"} {
		if host == platform || strings.HasSuffix(host, "."+platform) {
			return true
		}
	}
	return false
}

func (f *sourceIconFetcher) pageCandidates(body []byte, base string, add func(string, string, int)) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[strings.ToLower(a.Key)] = a.Val
			}
			rel := strings.Fields(strings.ToLower(attrs["rel"]))
			if slices.Contains(rel, "icon") || slices.Contains(rel, "apple-touch-icon") || slices.Contains(rel, "apple-touch-icon-precomposed") {
				priority := 0
				if strings.EqualFold(attrs["type"], "image/svg+xml") || strings.HasSuffix(strings.ToLower(attrs["href"]), ".svg") {
					priority = 1000000
				}
				for size := range strings.FieldsSeq(attrs["sizes"]) {
					w, h, ok := strings.Cut(size, "x")
					if ok {
						width, _ := strconv.Atoi(w)
						height, _ := strconv.Atoi(h)
						priority = max(priority, min(width, height))
					}
				}
				add(base, attrs["href"], priority)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
}

func (f *sourceIconFetcher) download(ctx context.Context, raw string) (biz.SourceIcon, error) {
	response, err := f.client.Get(ctx, raw, nil)
	if err != nil {
		return biz.SourceIcon{}, err
	}
	content := response.Body
	if len(content) == 0 || len(content) > 2<<20 {
		return biz.SourceIcon{}, fmt.Errorf("invalid icon size")
	}
	mime := ""
	if bytes.HasPrefix(content, []byte{0, 0, 1, 0}) {
		return decodeIconContainer(content)
	}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(content)); err == nil {
		if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 4<<20 {
			return biz.SourceIcon{}, fmt.Errorf("invalid icon dimensions")
		}
		if _, _, err := image.Decode(bytes.NewReader(content)); err != nil {
			return biz.SourceIcon{}, fmt.Errorf("invalid icon image")
		}
		mime = map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	} else if safeSVG(content) == nil {
		mime = "image/svg+xml"
	}
	if mime == "" {
		return biz.SourceIcon{}, fmt.Errorf("unsupported or unsafe icon")
	}
	return biz.SourceIcon{Content: bytes.Clone(content), ContentType: mime}, nil
}

func iconQuality(content []byte, mime string) (int, error) {
	if mime == "image/x-icon" {
		return icoDimension(content), nil
	}
	if mime == "image/svg+xml" {
		return 1000000, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return 0, err
	}
	return min(cfg.Width, cfg.Height), nil
}

var svgPaint = regexp.MustCompile(`^(none|currentColor|transparent|#[0-9a-fA-F]{3,8}|[a-zA-Z]+|rgba?\([0-9., %]+\)|url\(#[a-zA-Z0-9_-]+\))$`)

// Only static geometry is accepted: no CSS, executable nodes, animation, entities or external references.
func safeSVG(content []byte) error {
	elements := strings.Fields("svg g path rect circle ellipse line polyline polygon defs linearGradient radialGradient stop clipPath mask title desc use")
	attributes := strings.Fields("xmlns version width height viewBox preserveAspectRatio id d x y x1 y1 x2 y2 cx cy r rx ry points transform fill fill-rule fill-opacity stroke stroke-width stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset stroke-opacity opacity clip-path clip-rule mask gradientUnits gradientTransform offset stop-color stop-opacity spreadMethod fx fy href")
	decoder := xml.NewDecoder(bytes.NewReader(content))
	depth, nodes := 0, 0
	root := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid SVG")
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			nodes++
			if depth > 64 || nodes > 5000 || !slices.Contains(elements, t.Name.Local) || (t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg") {
				return fmt.Errorf("unsafe SVG element")
			}
			if !root {
				if t.Name.Local != "svg" {
					return fmt.Errorf("invalid SVG root")
				}
				root = true
			} else if depth == 1 {
				return fmt.Errorf("multiple SVG roots")
			}
			for _, a := range t.Attr {
				if !slices.Contains(attributes, a.Name.Local) {
					return fmt.Errorf("unsafe SVG attribute")
				}
				if a.Name.Space != "" && a.Name.Space != "xmlns" && a.Name.Space != "http://www.w3.org/1999/xlink" {
					return fmt.Errorf("unsafe SVG namespace")
				}
				if a.Name.Local == "xmlns" || a.Name.Space == "xmlns" {
					if a.Value != "http://www.w3.org/2000/svg" && a.Value != "http://www.w3.org/1999/xlink" {
						return fmt.Errorf("unsafe SVG namespace")
					}
					continue
				}
				if a.Name.Local == "href" {
					if !strings.HasPrefix(a.Value, "#") || strings.ContainsAny(a.Value, " \t\r\n") {
						return fmt.Errorf("external SVG reference")
					}
					continue
				}
				if slices.Contains([]string{"fill", "stroke", "stop-color", "clip-path", "mask"}, a.Name.Local) && !svgPaint.MatchString(a.Value) {
					return fmt.Errorf("unsafe SVG paint")
				}
				lower := strings.ToLower(a.Value)
				if strings.Contains(lower, "javascript:") || strings.Contains(lower, "data:") || strings.Contains(lower, "http:") || strings.Contains(lower, "https:") {
					return fmt.Errorf("external SVG value")
				}
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			return fmt.Errorf("SVG directives forbidden")
		case xml.ProcInst:
			if t.Target != "xml" {
				return fmt.Errorf("SVG processing instruction forbidden")
			}
		}
	}
	if !root || depth != 0 {
		return fmt.Errorf("invalid SVG")
	}
	return nil
}

func icoDimension(content []byte) int {
	if len(content) < 22 {
		return 0
	}
	w, h := int(content[6]), int(content[7])
	if w == 0 {
		w = 256
	}
	if h == 0 {
		h = 256
	}
	return min(w, h)
}

// Keep one validated original ICO image; uncompressed 32-bit DIB is a bounded fallback for older site favicons.
func decodeIconContainer(content []byte) (biz.SourceIcon, error) {
	if len(content) < 6 {
		return biz.SourceIcon{}, fmt.Errorf("invalid ICO")
	}
	count := int(binary.LittleEndian.Uint16(content[4:6]))
	if count == 0 || count > 256 || len(content) < 6+count*16 {
		return biz.SourceIcon{}, fmt.Errorf("invalid ICO")
	}
	best := -1
	var result biz.SourceIcon
	for i := range count {
		entry := content[6+i*16 : 6+(i+1)*16]
		size, offset := uint64(binary.LittleEndian.Uint32(entry[8:12])), uint64(binary.LittleEndian.Uint32(entry[12:16]))
		if size == 0 || offset < uint64(6+count*16) || offset+size > uint64(len(content)) {
			continue
		}
		pixels := content[offset : offset+size]
		w, h := int(entry[0]), int(entry[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		if !betterIcon(min(w, h), best) {
			continue
		}
		if cfg, format, err := image.DecodeConfig(bytes.NewReader(pixels)); err == nil && format == "png" {
			if cfg.Width != w || cfg.Height != h {
				continue
			}
			if _, _, err := image.Decode(bytes.NewReader(pixels)); err != nil {
				continue
			}
			result = biz.SourceIcon{Content: bytes.Clone(pixels), ContentType: "image/png"}
			best = min(w, h)
			continue
		}
		if len(pixels) < 40 || binary.LittleEndian.Uint32(pixels[:4]) != 40 || int(binary.LittleEndian.Uint32(pixels[4:8])) != w || int(binary.LittleEndian.Uint32(pixels[8:12])) != h*2 || binary.LittleEndian.Uint16(pixels[12:14]) != 1 || binary.LittleEndian.Uint16(pixels[14:16]) != 32 || binary.LittleEndian.Uint32(pixels[16:20]) != 0 || len(pixels) < 40+w*h*4 {
			continue
		}
		single := make([]byte, 22+len(pixels))
		copy(single[:6], []byte{0, 0, 1, 0, 1, 0})
		copy(single[6:22], entry)
		binary.LittleEndian.PutUint32(single[18:22], 22)
		copy(single[22:], pixels)
		result = biz.SourceIcon{Content: single, ContentType: "image/x-icon"}
		best = min(w, h)
	}
	if best < 0 {
		return biz.SourceIcon{}, fmt.Errorf("ICO has no supported image")
	}
	return result, nil
}
