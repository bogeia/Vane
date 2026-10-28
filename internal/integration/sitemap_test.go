package integration

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

type sitemapDocument struct {
	XMLName xml.Name
	URLs    []struct {
		Location     string `xml:"loc"`
		LastModified string `xml:"lastmod"`
	} `xml:"url"`
}

func TestSitemapIncludesPublicPagesAndUsesProcessingWindowAndLastModified(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Site.PublicURL = "https://public.example/base&path/"
	app := assembleTestApp(t, cfg)
	ctx := t.Context()
	if _, err := testDB(t, app.Data).ExecContext(ctx, `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('sitemap','Sitemap','official','https://example.test/map',false,60)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	story := insertFoldTestStory(t, ctx, testDB(t, app.Data), "Sitemap story", now)
	insert := func(title, stage string, selected bool, published, processed time.Time) int64 {
		id := insertFoldTestItem(t, ctx, testDB(t, app.Data), story, stage, "sitemap", title, title, "summary", "reason", "https://example.test/"+title, "模型", []string{}, published, selected)
		if _, err := testDB(t, app.Data).ExecContext(ctx, `UPDATE items SET processed_at=$2 WHERE id=$1`, id, processed); err != nil {
			t.Fatal(err)
		}
		return id
	}
	included := insert("old-publication-new-processing", "done", true, now.AddDate(-2, 0, 0), now.Add(-time.Hour))
	excluded := []int64{
		insert("unselected", "done", false, now, now),
		insert("too-old-processing", "done", true, now, now.Add(-365*24*time.Hour-time.Hour)),
		insert("not-public", "analyzed", true, now, now),
		insert("blocked", "blocked", true, now, now),
		insert("future-processing", "done", true, now, now.Add(time.Hour)),
	}
	if _, err := testDB(t, app.Data).ExecContext(ctx, `INSERT INTO reports(date,content,created_at) SELECT DATE '2025-01-01'+i,'{}'::jsonb,TIMESTAMPTZ '2026-10-22 09:15:00+08:00' FROM generate_series(0,69) i`); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	app.Router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, "/sitemap.xml", nil))
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/xml") || response.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("sitemap response=%d %v %s", response.Code, response.Header(), response.Body.String())
	}
	var document sitemapDocument
	if err := xml.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.XMLName.Local != "urlset" || document.XMLName.Space != "http://www.sitemaps.org/schemas/sitemap/0.9" || !strings.Contains(response.Body.String(), "&amp;") {
		t.Fatal("missing namespace or XML escaping")
	}
	base := strings.TrimRight(cfg.Site.PublicURL, "/")
	locations := map[string]string{}
	for _, entry := range document.URLs {
		if !strings.HasPrefix(entry.Location, base+"/") || strings.Contains(entry.Location, "/stories/") {
			t.Fatalf("invalid URL %s", entry.Location)
		}
		if _, exists := locations[entry.Location]; exists {
			t.Fatalf("duplicate URL %s", entry.Location)
		}
		locations[entry.Location] = entry.LastModified
	}
	for _, path := range []string{"/", "/all", "/hot", "/reports", "/topics", "/about", "/agent", "/changelog"} {
		if lastmod, ok := locations[base+path]; !ok || lastmod != "" {
			t.Fatalf("missing fixed page %s or unexpected lastmod", path)
		}
	}
	for _, topic := range taxonomy.Topics {
		if lastmod, ok := locations[base+"/topics/"+topic.Slug]; !ok || lastmod != "" {
			t.Fatalf("missing topic %s or unexpected lastmod", topic.Slug)
		}
	}
	if _, exists := locations[base+"/more"]; exists {
		t.Fatal("sitemap lists the phone-only /more page")
	}
	if len(locations) != 8+len(taxonomy.Topics)+70+1 {
		t.Fatalf("sitemap URLs=%d", len(locations))
	}
	processed, err := time.Parse(time.RFC3339, locations[base+"/items/"+strconv.FormatInt(included, 10)])
	if err != nil || !processed.Equal(now.Add(-time.Hour)) {
		t.Fatalf("invalid item lastmod %s", processed)
	}
	for _, id := range excluded {
		if _, exists := locations[fmt.Sprintf("%s/items/%d", base, id)]; exists {
			t.Fatalf("excluded item %d present", id)
		}
	}
	for _, date := range []string{"2025-01-01", "2025-03-11"} {
		lastmod, err := time.Parse(time.RFC3339, locations[base+"/reports/"+date])
		want, _ := time.Parse(time.RFC3339, "2026-10-22T09:15:00+08:00")
		if err != nil || !lastmod.Equal(want) {
			t.Fatalf("report %s lastmod=%s", date, lastmod)
		}
	}
	head := httptest.NewRecorder()
	app.Router.ServeHTTP(head, httptest.NewRequestWithContext(ctx, http.MethodHead, "/sitemap.xml", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Type") != response.Header().Get("Content-Type") || head.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("HEAD sitemap=%d %v %s", head.Code, head.Header(), head.Body.String())
	}
}

func TestSitemapCapsURLsAndKeepsLatestProcessedItemsEvenWhenReportsFillLimit(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Site.PublicURL = "https://sitemap.example"
	app := assembleTestApp(t, cfg)
	ctx := t.Context()
	if _, err := testDB(t, app.Data).ExecContext(ctx, `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('cap','Cap','official','https://example.test/cap',false,60)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	story := insertFoldTestStory(t, ctx, testDB(t, app.Data), "Cap", now)
	if _, err := testDB(t, app.Data).ExecContext(ctx, `INSERT INTO items(source_id,url,original_title,title,published_at,processed_at,stage,selected,story_id) SELECT 'cap','https://example.test/cap/'||i,'Cap '||i,'Cap '||i,$1::timestamptz+i*interval '1 second',$1::timestamptz-i*interval '1 second','done',true,$2 FROM generate_series(1,50005) i`, now, story); err != nil {
		t.Fatal(err)
	}
	get := func() sitemapDocument {
		response := httptest.NewRecorder()
		app.Router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, "/sitemap.xml", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("sitemap returned %d", response.Code)
		}
		var doc sitemapDocument
		if err := xml.Unmarshal(response.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.URLs) != 50000 {
			t.Fatalf("URL count=%d want50000", len(doc.URLs))
		}
		return doc
	}
	doc := get()
	itemBudget := 50000 - 8 - len(taxonomy.Topics)
	seen := map[string]bool{}
	for _, entry := range doc.URLs {
		seen[entry.Location] = true
	}
	// IDs are inserted in reverse processing-time order; publication order is opposite.
	for _, id := range []int{1, itemBudget} {
		if !seen[fmt.Sprintf("%s/items/%d", cfg.Site.PublicURL, id)] {
			t.Fatalf("latest processed item %d missing", id)
		}
	}
	if seen[fmt.Sprintf("%s/items/%d", cfg.Site.PublicURL, itemBudget+1)] {
		t.Fatal("oldest excess item was retained")
	}
	// Reports alone exceed the available budget: avoid a negative/zero SQL limit
	// exposing all items, and keep the total bound even in this case.
	if _, err := testDB(t, app.Data).ExecContext(ctx, `INSERT INTO reports(date,content) SELECT DATE '2000-01-01'+i,'{}'::jsonb FROM generate_series(0,50004) i`); err != nil {
		t.Fatal(err)
	}
	doc = get()
	reports := 0
	for _, entry := range doc.URLs {
		if strings.Contains(entry.Location, "/items/") {
			t.Fatal("items leaked after reports filled the budget")
		}
		if strings.Contains(entry.Location, "/reports/") {
			reports++
		}
	}
	if reports != itemBudget {
		t.Fatalf("report count=%d want%d", reports, itemBudget)
	}
}
