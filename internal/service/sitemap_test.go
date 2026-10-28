package service

import (
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
)

type sitemapResourceRepository struct {
	biz.ItemsRepository
	reports, items []biz.SitemapResource
}

func (r sitemapResourceRepository) SitemapReports(context.Context, int) ([]biz.SitemapResource, error) {
	return r.reports, nil
}

func (r sitemapResourceRepository) SitemapItems(context.Context, biz.SitemapQuery) ([]biz.SitemapResource, error) {
	return r.items, nil
}

func TestSitemapServiceMapsResourceReferencesToURLs(t *testing.T) {
	stamp := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	repo := sitemapResourceRepository{reports: []biz.SitemapResource{{Kind: biz.SitemapReport, ID: "2026-10-23", LastModified: stamp}}, items: []biz.SitemapResource{{Kind: biz.SitemapItem, ID: "9007199254740993", LastModified: stamp}}}
	items := NewItemsService(biz.NewItemsUseCase(repo, time.Now), slog.New(slog.NewTextHandler(io.Discard, nil)))
	body, err := items.Sitemap(t.Context(), config.Site{PublicURL: "https://vane.test/"})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		URLs []sitemapURL `xml:"url"`
	}
	if err := xml.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.URLs) < 2 {
		t.Fatalf("document=%s", body)
	}
	resources := document.URLs[len(document.URLs)-2:]
	for index, want := range []string{"https://vane.test/reports/2026-10-23", "https://vane.test/items/9007199254740993"} {
		if resources[index].Location != want || resources[index].LastModified != "2026-10-23T08:00:00Z" {
			t.Fatalf("URL=%+v", resources[index])
		}
	}
	repo.items = []biz.SitemapResource{{Kind: "unsupported", ID: "1"}}
	items = NewItemsService(biz.NewItemsUseCase(repo, time.Now), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := items.Sitemap(t.Context(), config.Site{}); err == nil {
		t.Fatal("accepted unsupported resource kind")
	}
}

func TestSitemapListsFixedPagesWithoutLastModified(t *testing.T) {
	items := NewItemsService(biz.NewItemsUseCase(sitemapResourceRepository{}, time.Now), slog.New(slog.NewTextHandler(io.Discard, nil)))
	body, err := items.Sitemap(t.Context(), config.Site{PublicURL: "https://vane.test"})
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		URLs []sitemapURL `xml:"url"`
	}
	if err := xml.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	want := []string{"/", "/all", "/hot", "/reports", "/topics", "/about", "/agent", "/changelog"}
	if len(document.URLs) < len(want) {
		t.Fatalf("document=%s", body)
	}
	for index, path := range want {
		if got := document.URLs[index]; got.Location != "https://vane.test"+path || got.LastModified != "" {
			t.Fatalf("URL %d = %+v, want %s without lastmod", index, got, path)
		}
	}
	for _, entry := range document.URLs {
		if entry.Location == "https://vane.test/more" {
			t.Fatal("sitemap lists the phone-only /more page")
		}
	}
	locations := make([]string, len(document.URLs))
	for index, entry := range document.URLs {
		locations[index] = entry.Location
	}
	assert.Contains(t, locations, "https://vane.test/agent")
}
