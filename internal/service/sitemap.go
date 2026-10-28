package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/biz/taxonomy"
	"github.com/bogeia/vane/internal/config"
)

const sitemapLimit = 50000

// Fixed pages carry no lastmod. /more is phone-only navigation and stays out.
var sitemapFixedPaths = []string{"/", "/all", "/hot", "/reports", "/topics", "/about", "/agent", "/changelog"}

type sitemapURL struct {
	Location     string `xml:"loc"`
	LastModified string `xml:"lastmod,omitempty"`
}

func (i *ItemsService) Sitemap(ctx context.Context, site config.Site) ([]byte, error) {
	document := struct {
		XMLName   xml.Name     `xml:"urlset"`
		Namespace string       `xml:"xmlns,attr"`
		URLs      []sitemapURL `xml:"url"`
	}{Namespace: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	base := strings.TrimRight(site.PublicURL, "/")
	for _, path := range sitemapFixedPaths {
		document.URLs = append(document.URLs, sitemapURL{Location: base + path})
	}
	for _, topic := range taxonomy.Topics {
		document.URLs = append(document.URLs, sitemapURL{Location: base + "/topics/" + topic.Slug})
	}
	pages, err := i.items.SitemapPages(ctx, time.Now(), max(0, sitemapLimit-len(document.URLs)))
	if err != nil {
		return nil, err
	}
	for _, page := range pages {
		if len(document.URLs) >= sitemapLimit {
			break
		}
		var path string
		switch page.Kind {
		case biz.SitemapReport:
			path = "/reports/" + page.ID
		case biz.SitemapItem:
			path = "/items/" + page.ID
		default:
			return nil, fmt.Errorf("unsupported sitemap resource kind %q", page.Kind)
		}
		document.URLs = append(document.URLs, sitemapURL{Location: base + path, LastModified: page.LastModified.Format(time.RFC3339)})
	}
	body, err := xml.Marshal(document)
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}
