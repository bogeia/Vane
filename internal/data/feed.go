package data

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"codeberg.org/readeck/go-readability/v2"
	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/infra/fetch"
)

type FeedFetcher struct {
	client   *fetch.Client
	webLists map[string]WebListSelectors
}

func NewFeedFetcher(client *fetch.Client, webLists map[string]WebListSelectors) *FeedFetcher {
	return &FeedFetcher{client: client, webLists: webLists}
}

func (f *FeedFetcher) Fetch(ctx context.Context, source biz.CollectSource) (biz.Feed, error) {
	if source.Kind == biz.SourceKindHFModels {
		return f.hfModels(ctx, source)
	}
	if source.Kind != biz.SourceKindRSS && source.Kind != biz.SourceKindWebList {
		return biz.Feed{}, fmt.Errorf("unsupported source kind")
	}
	headers := make(http.Header)
	if source.Kind == biz.SourceKindRSS && source.ETag != "" {
		headers.Set("If-None-Match", source.ETag)
	}
	if source.Kind == biz.SourceKindRSS && source.LastModified != "" {
		headers.Set("If-Modified-Since", source.LastModified)
	}
	response, err := f.client.Get(ctx, source.FeedURL, headers)
	if err != nil {
		return biz.Feed{}, err
	}
	result := biz.Feed{URL: response.URL, ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified"), NotModified: response.StatusCode == http.StatusNotModified}
	if result.NotModified {
		return result, nil
	}
	if source.Kind == biz.SourceKindWebList {
		// Fetch lists without validators so articles skipped on failure can retry next time.
		result.ETag, result.LastModified = "", ""
		result.Items, err = parseWebList(response.Body, response.URL, f.webLists[source.ID])
		if err != nil {
			return biz.Feed{}, err
		}
		return result, nil
	}
	parsed, err := gofeed.NewParser().Parse(bytes.NewReader(response.Body))
	if err != nil {
		return biz.Feed{}, fmt.Errorf("%w: %w", biz.ErrFeedParse, err)
	}
	for _, entry := range parsed.Items {
		body := entry.Content
		if strings.TrimSpace(body) == "" {
			body = entry.Description
		}
		image := ""
		if entry.Image != nil {
			image = entry.Image.URL
		}
		result.Items = append(result.Items, biz.FeedItem{URL: entry.Link, Title: entry.Title, Body: plainText(body), ImageURL: image, Published: entry.PublishedParsed, Updated: entry.UpdatedParsed})
	}
	return result, nil
}

func (f *FeedFetcher) Article(ctx context.Context, source biz.CollectSource, rawURL string) (string, error) {
	if source.Kind == biz.SourceKindHFModels {
		cardURL, err := url.JoinPath(rawURL, "raw/main/README.md")
		if err != nil {
			return "", err
		}
		response, err := f.client.Get(ctx, cardURL, nil)
		if err != nil {
			return "", err
		}
		return hfModelCard(string(response.Body))
	}
	response, err := f.client.Get(ctx, rawURL, nil)
	if err != nil {
		return "", err
	}
	pageURL, err := url.Parse(response.URL)
	if err != nil {
		return "", err
	}
	article, err := readability.FromReader(bytes.NewReader(response.Body), pageURL)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	if err := article.RenderText(&text); err != nil {
		return "", err
	}
	return strings.TrimSpace(text.String()), nil
}

func plainText(source string) string {
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return ""
	}
	var text strings.Builder
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && (node.Data == "script" || node.Data == "style") {
			return
		}
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
			text.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	return strings.Join(strings.Fields(text.String()), " ")
}
