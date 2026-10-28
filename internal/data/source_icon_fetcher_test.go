package data

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/fetch"
)

func iconPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSourceIconDiscoveryChoosesSmallestAdequateOriginal(t *testing.T) {
	small, large, medium := iconPNG(t, 32, 32), iconPNG(t, 512, 256), iconPNG(t, 64, 64)
	vector := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 256"><path fill="#123456" d="M0 0h512v256H0z"/></svg>`)
	withSVG := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed":
			fmt.Fprintf(w, `<rss version="2.0"><channel><title>Publisher</title><link>http://%s/home</link><description>Publisher</description><image><url>/small.png</url></image></channel></rss>`, r.Host)
		case "/home":
			fmt.Fprint(w, `<link rel="icon" href="/broken.svg" type="image/svg+xml"><link rel="icon" href="/small.png" sizes="32x32"><link rel="apple-touch-icon" href="/large.png" sizes="256x256"><link rel="icon" href="/medium.png" sizes="64x64">`)
			if withSVG {
				fmt.Fprint(w, `<link rel="icon" href="/vector.svg" type="image/svg+xml">`)
			}
		case "/broken.svg":
			fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`)
		case "/small.png":
			w.Write(small)
		case "/medium.png":
			w.Write(medium)
		case "/large.png":
			w.Write(large)
		case "/vector.svg":
			w.Write(vector)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	f := &sourceIconFetcher{client: fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}})}
	source := biz.Source{Kind: biz.SourceKindRSS, FeedURL: server.URL + "/feed"}
	icon, err := f.DiscoverIcon(t.Context(), source)
	if err != nil || icon.ContentType != "image/png" || !bytes.Equal(icon.Content, medium) {
		t.Fatalf("did not retain best raster: type=%s err=%v", icon.ContentType, err)
	}
	withSVG = true
	icon, err = f.DiscoverIcon(t.Context(), source)
	if err != nil || icon.ContentType != "image/svg+xml" || !bytes.Equal(icon.Content, vector) {
		t.Fatalf("did not prefer safe SVG: %v", err)
	}
	source.IconURL = server.URL + "/broken.svg"
	if _, err := f.DiscoverIcon(t.Context(), source); err == nil {
		t.Fatal("manual override must not silently fall back")
	}
}

func TestSourceIconRejectsUnsafeSVG(t *testing.T) {
	for _, body := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><use href="https://evil.test/a.svg#x"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><path style="fill:red"/></svg>`,
		`<!DOCTYPE svg [<!ENTITY x "evil">]><svg xmlns="http://www.w3.org/2000/svg"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><image href="data:image/svg+xml,evil"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><path fill="url(https://evil.test/x)"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><animate attributeName="href"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg">` + strings.Repeat(`<g>`, 65) + strings.Repeat(`</g>`, 65) + `</svg>`,
	} {
		if err := safeSVG([]byte(body)); err == nil {
			t.Fatalf("accepted unsafe SVG: %.100s", body)
		}
	}
	if err := safeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256"><defs><linearGradient id="a"><stop stop-color="#fff"/></linearGradient></defs><path fill="url(#a)" d="M0 0h256v256z"/></svg>`)); err != nil {
		t.Fatal(err)
	}
}

func TestSourceIconRejectsOversizedRasterAndSSRF(t *testing.T) {
	// A valid PNG header with excessive dimensions is rejected before allocating decoded pixels.
	large := iconPNG(t, 4097, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(large) }))
	defer server.Close()
	cfg := config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}}
	f := &sourceIconFetcher{client: fetch.New(true, cfg)}
	if _, err := f.download(t.Context(), server.URL); err == nil {
		t.Fatal("accepted excessive dimensions")
	}
	f.client = fetch.New(false, cfg)
	if _, err := f.download(t.Context(), server.URL); err == nil {
		t.Fatal("production client allowed loopback")
	}
}

func TestSourceIconICOOriginalFallbackAndInvalidOffsets(t *testing.T) {
	pixels := make([]byte, 40+32*32*4)
	binary.LittleEndian.PutUint32(pixels[:4], 40)
	binary.LittleEndian.PutUint32(pixels[4:8], 32)
	binary.LittleEndian.PutUint32(pixels[8:12], 64)
	binary.LittleEndian.PutUint16(pixels[12:14], 1)
	binary.LittleEndian.PutUint16(pixels[14:16], 32)
	content := make([]byte, 22+len(pixels))
	copy(content, []byte{0, 0, 1, 0, 1, 0, 32, 32})
	binary.LittleEndian.PutUint32(content[14:18], uint32(len(pixels)))
	binary.LittleEndian.PutUint32(content[18:22], 22)
	copy(content[22:], pixels)
	icon, err := decodeIconContainer(content)
	if err != nil || icon.ContentType != "image/x-icon" || icoDimension(icon.Content) != 32 || !bytes.Equal(icon.Content[22:], pixels) {
		t.Fatalf("original ICO fallback lost: %v", err)
	}
	binary.LittleEndian.PutUint32(content[18:22], uint32(len(content)+1))
	if _, err := decodeIconContainer(content); err == nil {
		t.Fatal("accepted invalid ICO offset")
	}
}

func TestSourceIconMatchesPublisherNotHostingPlatform(t *testing.T) {
	for _, tc := range []struct {
		source biz.Source
		want   string
	}{
		{biz.Source{Kind: biz.SourceKindHFModels, FeedURL: "https://huggingface.co/deepseek-ai"}, "companies/deepseek.svg"},
		{biz.Source{Kind: biz.SourceKindHFModels, FeedURL: "https://huggingface.co/unknown"}, ""},
		{biz.Source{Kind: biz.SourceKindRSS, FeedURL: "https://blog.google/rss/"}, "sources/google-color.svg"},
		{biz.Source{Kind: biz.SourceKindRSS, FeedURL: "https://evilopenai.com/feed"}, ""},
	} {
		if got := publisherArtwork(tc.source); got != tc.want {
			t.Fatalf("publisher=%s got=%s want=%s", tc.source.FeedURL, got, tc.want)
		}
	}
}

func TestSourceIconSizeLimitAndContentSniffing(t *testing.T) {
	pngBody := iconPNG(t, 256, 256)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/oversize":
			w.Write(bytes.Repeat([]byte("x"), (2<<20)+1))
		default:
			w.Write(pngBody)
		}
	}))
	defer server.Close()
	f := &sourceIconFetcher{client: fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}})}
	icon, err := f.download(t.Context(), server.URL)
	if err != nil || icon.ContentType != "image/png" || !bytes.Equal(icon.Content, pngBody) {
		t.Fatal("trusted incorrect MIME instead of validated bytes")
	}
	if _, err := f.download(t.Context(), server.URL+"/oversize"); err == nil {
		t.Fatal("accepted image larger than 2 MiB")
	}
}

func TestSourceIconICOChoosesSmallestAdequatePNG(t *testing.T) {
	small, large := iconPNG(t, 64, 64), iconPNG(t, 256, 256)
	content := make([]byte, 38+len(small)+len(large))
	copy(content, []byte{0, 0, 1, 0, 2, 0})
	for i, body := range [][]byte{small, large} {
		entry := content[6+i*16 : 22+i*16]
		if i == 0 {
			entry[0], entry[1] = 64, 64
		}
		offset := 38
		if i == 1 {
			offset += len(small)
		}
		binary.LittleEndian.PutUint32(entry[8:12], uint32(len(body)))
		binary.LittleEndian.PutUint32(entry[12:16], uint32(offset))
		copy(content[offset:], body)
	}
	icon, err := decodeIconContainer(content)
	if err != nil || icon.ContentType != "image/png" || !bytes.Equal(icon.Content, small) {
		t.Fatalf("ICO selected wrong resolution: %v", err)
	}
}

func TestSourceIconReviewedRasterArtwork(t *testing.T) {
	for host, asset := range map[string]string{
		"bair.berkeley.edu": "bair.png", "theverge.com": "the-verge.png", "techcrunch.com": "techcrunch.png",
		"arstechnica.com": "ars-technica.png", "technologyreview.com": "mit-tech-review.ico", "the-decoder.com": "the-decoder.png",
		"simonwillison.net": "simon-willison.ico", "latent.space": "latent-space.png", "pytorch.org": "pytorch.webp",
		"amazon.science": "amazon-science.png", "news.mit.edu": "mit-news.ico", "qbitai.com": "qbitai.ico", "importai.substack.com": "import-ai.png",
	} {
		source := biz.Source{Kind: biz.SourceKindRSS, FeedURL: "https://" + host + "/feed"}
		if got := publisherArtwork(source); got != "sources/"+asset {
			t.Fatalf("%s: %s", host, got)
		}
		icon, err := (&sourceIconFetcher{}).DiscoverIcon(t.Context(), source)
		if err != nil || len(icon.Content) == 0 || icon.ContentType == "" {
			t.Fatalf("%s: %v", host, err)
		}
		if host == "latent.space" && len(icon.Content) > 10000 {
			t.Fatalf("large Latent Space artwork: %d", len(icon.Content))
		}
	}
}

func TestSourceIconFeedSelfLinkFallsBackToRootHTML(t *testing.T) {
	original := iconPNG(t, 64, 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed":
			fmt.Fprintf(w, `<rss version="2.0"><channel><title>T</title><link>http://%s/feed</link><description>T</description></channel></rss>`, r.Host)
		case "/":
			fmt.Fprint(w, `<link rel="icon" href="/brand.png">`)
		case "/brand.png":
			w.Write(original)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	f := &sourceIconFetcher{client: fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}})}
	icon, err := f.DiscoverIcon(t.Context(), biz.Source{Kind: biz.SourceKindRSS, FeedURL: server.URL + "/feed"})
	if err != nil || !bytes.Equal(icon.Content, original) {
		t.Fatalf("root discovery failed: %v", err)
	}
}

func TestSourceIconQualityTarget(t *testing.T) {
	for _, tc := range []struct {
		size, current int
		want          bool
	}{{64, 256, true}, {256, 64, false}, {32, 16, true}, {32, 64, false}, {64, 32, true}, {1000000, 64, true}, {64, 1000000, false}} {
		if got := betterIcon(tc.size, tc.current); got != tc.want {
			t.Fatalf("%+v got %v", tc, got)
		}
	}
}

func TestSourceIconCandidateLimitKeepsSmallestAdequate(t *testing.T) {
	small, large := iconPNG(t, 64, 64), iconPNG(t, 256, 256)
	downloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			for i := range 21 {
				fmt.Fprintf(w, `<link rel="icon" sizes="256x256" href="/large-%d.png">`, i)
			}
			fmt.Fprint(w, `<link rel="icon" sizes="64x64" href="/small.png">`)
		case "/small.png":
			downloads++
			w.Write(small)
		default:
			downloads++
			w.Write(large)
		}
	}))
	defer server.Close()
	f := &sourceIconFetcher{client: fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}})}
	icon, err := f.DiscoverIcon(t.Context(), biz.Source{Kind: biz.SourceKindWebList, FeedURL: server.URL})
	if err != nil || !bytes.Equal(icon.Content, small) || downloads > 20 {
		t.Fatalf("candidate limit: %d downloads, %v", downloads, err)
	}
}
