package integration

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/infra/fetch"
)

var fixtureBody = strings.Repeat("fixture body content for collection integration tests. ", 12)

func TestCollectorRunOnceStoresFeedItemAsNew(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>Can we change an AI model&amp;#8217;s architecture &amp; R&amp;D &amp;amp;#8217;</title><link>https://example.test/items/1</link><guid>fixture-item-1</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody))
	}))
	defer feedServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL,
		Enabled: true, IntervalMinutes: 60,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble collector app: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)

	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("run collector: %v", err)
	}
	var originalTitle string
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), "SELECT original_title FROM items").Scan(&originalTitle); err != nil {
		t.Fatal(err)
	}
	if originalTitle != "Can we change an AI model’s architecture & R&D &#8217;" {
		t.Fatalf("collected original title = %q", originalTitle)
	}
	var itemCount int
	var stage string
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), "SELECT count(*), min(stage) FROM items").Scan(&itemCount, &stage); err != nil {
		t.Fatal("read collected item state")
	}
	if itemCount != 1 || stage != "new" {
		t.Fatalf("collected items = %d at stage %q, want one item at stage new", itemCount, stage)
	}
}

func TestCollectorKeepsInitialEightAndOnlyRecentValidItems(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	currentTime := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	secondTime := currentTime.Add(time.Minute)
	var requests atomic.Int32
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		if requests.Add(1) == 1 {
			var feed strings.Builder
			feed.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture</description>`)
			for index := 1; index <= 10; index++ {
				published := currentTime.AddDate(0, 0, -31+index).Format(time.RFC1123)
				fmt.Fprintf(&feed, `<item><title>Initial %02d</title><link>https://example.test/initial-%02d</link><guid>initial-%02d</guid><pubDate>%s</pubDate><description>%s</description></item>`, index, index, index, published, fixtureBody)
			}
			feed.WriteString(`</channel></rss>`)
			_, _ = io.WriteString(w, feed.String())
			return
		}
		_, _ = io.WriteString(w, atomFeed(
			atomEntry("too-old", "https://example.test/too-old", secondTime.Add(-7*24*time.Hour-time.Second), time.Time{}),
			atomEntry("seven-day-boundary", "https://example.test/seven-day-boundary", secondTime.Add(-7*24*time.Hour), time.Time{}),
			atomEntry("recent", "https://example.test/recent", secondTime.Add(-24*time.Hour), time.Time{}),
			atomEntry("published-wins", "https://example.test/published-wins", secondTime.Add(-48*time.Hour), secondTime.Add(-24*time.Hour)),
			atomUpdatedEntry("updated-only", "https://example.test/updated-only", secondTime.Add(-time.Hour)),
			atomEntryWithoutDates("discovered", "https://example.test/discovered"),
			atomEntry("future", "https://example.test/future", secondTime.Add(48*time.Hour), time.Time{}),
			atomEntry("invalid-url", "javascript:alert(1)", secondTime.Add(-time.Hour), time.Time{}),
		))
	}))
	defer feedServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL,
		Enabled: true, IntervalMinutes: 1,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, func() time.Time { return currentTime }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble collector app: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("initial collector run: %v", err)
	}

	var initialCount int
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), `SELECT count(*) FROM items WHERE url LIKE 'https://example.test/initial-%'`).Scan(&initialCount); err != nil {
		t.Fatal("read initial item count")
	}
	if initialCount != 8 {
		t.Fatalf("initially collected items = %d, want the eight newest", initialCount)
	}
	var omittedOldest bool
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM items WHERE url='https://example.test/initial-01')`).Scan(&omittedOldest); err != nil {
		t.Fatal("check omitted initial item")
	}
	if omittedOldest {
		t.Fatal("initial collection retained an item outside the newest eight")
	}

	currentTime = currentTime.Add(time.Minute)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("subsequent collector run: %v", err)
	}

	rows, err := testDB(t, application.Data).QueryContext(t.Context(), `SELECT url,published_at,discovered_at FROM items ORDER BY url`)
	if err != nil {
		t.Fatal("read collected item dates")
	}
	defer rows.Close()
	items := make(map[string]struct{ published, discovered time.Time })
	for rows.Next() {
		var url string
		var item struct{ published, discovered time.Time }
		if err := rows.Scan(&url, &item.published, &item.discovered); err != nil {
			t.Fatal("scan collected item date")
		}
		items[url] = item
	}
	if err := rows.Err(); err != nil {
		t.Fatal("iterate collected item dates")
	}

	for _, url := range []string{
		"https://example.test/seven-day-boundary",
		"https://example.test/recent",
		"https://example.test/published-wins",
		"https://example.test/updated-only",
		"https://example.test/discovered",
		"https://example.test/future",
	} {
		if _, ok := items[url]; !ok {
			t.Errorf("recent valid item %q was not collected", url)
		}
	}
	for _, url := range []string{"https://example.test/too-old", "javascript:alert(1)"} {
		if _, ok := items[url]; ok {
			t.Errorf("out-of-window or invalid item %q was collected", url)
		}
	}
	if got, want := items["https://example.test/published-wins"].published, currentTime.Add(-48*time.Hour); !got.Equal(want) {
		t.Errorf("published timestamp = %s, want published date %s", got, want)
	}
	if got, want := items["https://example.test/updated-only"].published, currentTime.Add(-time.Hour); !got.Equal(want) {
		t.Errorf("updated fallback timestamp = %s, want %s", got, want)
	}
	if got, want := items["https://example.test/discovered"].published, currentTime; !got.Equal(want) {
		t.Errorf("missing-date timestamp = %s, want discovery time %s", got, want)
	}
	if got, want := items["https://example.test/future"].published, currentTime; !got.Equal(want) {
		t.Errorf("future timestamp = %s, want clamped time %s", got, want)
	}
}

func atomFeed(entries ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"><title>Fixture</title><id>https://example.test/</id><updated>2026-10-22T12:00:00Z</updated>` + strings.Join(entries, "") + `</feed>`
}

func atomEntry(id, url string, published, updated time.Time) string {
	var entry strings.Builder
	fmt.Fprintf(&entry, `<entry><title>%s</title><id>%s</id><link rel="alternate" href="%s"/><summary>%s</summary>`, id, id, url, fixtureBody)
	if !published.IsZero() {
		fmt.Fprintf(&entry, `<published>%s</published>`, published.Format(time.RFC3339))
	}
	if !updated.IsZero() {
		fmt.Fprintf(&entry, `<updated>%s</updated>`, updated.Format(time.RFC3339))
	}
	entry.WriteString(`</entry>`)
	return entry.String()
}

func atomUpdatedEntry(id, url string, updated time.Time) string {
	return atomEntry(id, url, time.Time{}, updated)
}

func atomEntryWithoutDates(id, url string) string {
	return atomEntry(id, url, time.Time{}, time.Time{})
}

func TestCollectorNormalizesDuplicateURLsAndUsesConditionalRequests(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	currentTime := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	var gotETag atomic.Bool
	var gotLastModified atomic.Bool
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("ETag", `"fixture-v1"`)
			w.Header().Set("Last-Modified", "Wed, 21 Oct 2026 10:00:00 GMT")
			w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
			_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>New AI research release</title><link>https://example.test/story?utm_source=first</link><guid>fixture-item-a</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
<item><title>New AI research release</title><link>https://example.test/story?utm_source=second</link><guid>fixture-item-b</guid><pubDate>Wed, 21 Oct 2026 11:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody, fixtureBody))
			return
		}
		gotETag.Store(r.Header.Get("If-None-Match") == `"fixture-v1"`)
		gotLastModified.Store(r.Header.Get("If-Modified-Since") == "Wed, 21 Oct 2026 10:00:00 GMT")
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusNotModified)
		_, _ = io.WriteString(w, "definitely not valid RSS")
	}))
	defer feedServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL,
		Enabled: true, IntervalMinutes: 60,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, func() time.Time { return currentTime }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble collector app: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)

	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("first collector run: %v", err)
	}
	var itemCount int
	var itemURL string
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), "SELECT count(*), min(url) FROM items").Scan(&itemCount, &itemURL); err != nil {
		t.Fatal("read collected item state")
	}
	if itemCount != 1 || itemURL != "https://example.test/story" {
		t.Fatalf("stored items = %d at URL %q, want one normalized story URL", itemCount, itemURL)
	}

	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("run before source is due: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("feed requests before interval elapsed = %d, want 1", got)
	}

	currentTime = currentTime.Add(time.Hour)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collector run for not-modified response: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("feed requests after interval elapsed = %d, want 2", got)
	}
	if !gotETag.Load() || !gotLastModified.Load() {
		t.Fatalf("conditional headers sent: If-None-Match=%v If-Modified-Since=%v", gotETag.Load(), gotLastModified.Load())
	}
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&itemCount); err != nil {
		t.Fatal("read item count after not-modified response")
	}
	if itemCount != 1 {
		t.Fatalf("item count after not-modified response = %d, want 1", itemCount)
	}
}

func TestChangingSourceIntervalUpdatesNextCollectionTime(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	currentTime := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>Fixture</description><item><title>Interval fixture</title><link>https://example.test/interval-item</link><guid>interval-item</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item></channel></rss>`, fixtureBody)
	}))
	defer feedServer.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL,
		Enabled: true, IntervalMinutes: 60,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, func() time.Time { return currentTime }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble collector app: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("initial collector run: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("initial requests = %d, want 1", got)
	}

	cfg.Sources[0].IntervalMinutes = 1
	if err := data.SyncSources(t.Context(), application.Data, cfg.Sources); err != nil {
		t.Fatalf("update source interval: %v", err)
	}
	currentTime = currentTime.Add(time.Minute)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collector run after interval change: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests after new interval elapsed = %d, want 2", got)
	}
}

func TestCollectorBlockWordsBeforeInitialLimit(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	now := time.Now().UTC().Truncate(time.Second)
	firstBlockedAt := now
	var blockedArticles atomic.Int32
	var failNext atomic.Bool
	var conditionalRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/feed" {
			if r.URL.Path == "/blocked" {
				blockedArticles.Add(1)
			}
			fmt.Fprint(w, "article")
			return
		}
		if failNext.Swap(false) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("If-None-Match") == `"block-v1"` || r.Header.Get("If-Modified-Since") == "Wed, 21 Oct 2026 10:00:00 GMT" {
			conditionalRequests.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"block-v1"`)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2026 10:00:00 GMT")
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<rss version="2.0"><channel><title>Fixture</title>`)
		fmt.Fprintf(w, `<item><title>CAR &amp;amp; battery</title><link>%s/blocked?utm_source=one</link></item>`, server.URL)
		fmt.Fprintf(w, `<item><title>car battery</title><link>%s/blocked?utm_source=two</link></item>`, server.URL)
		for i := range 8 {
			title := fmt.Sprintf("ordinary %d", i)
			if i == 0 {
				title = "car AI"
			}
			fmt.Fprintf(w, `<item><title>%s</title><link>%s/kept-%d</link><description>car</description></item>`, title, server.URL, i)
		}
		fmt.Fprint(w, `</channel></rss>`)
	}))
	defer server.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: server.URL + "/feed", Enabled: true, IntervalMinutes: 1, Block: [][]string{{"car", "!ai"}}}}
	app, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, cfg.Fetch), nil))
	if err != nil {
		t.Fatal("assemble collector")
	}
	defer cleanup()
	if err := app.Collector.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 8 || blockedArticles.Load() != 0 {
		t.Fatalf("items=%d blocked article requests=%d", count, blockedArticles.Load())
	}
	var successful bool
	var added int
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT success, new_items FROM fetch_records ORDER BY fetched_at DESC LIMIT 1").Scan(&successful, &added); err != nil {
		t.Fatal(err)
	}
	if !successful || added != 8 {
		t.Fatalf("fetch success=%v new=%d", successful, added)
	}

	operations := biz.NewOperationsUseCase(data.NewOperationsRepo(app.Data))
	checkBlocked := func() {
		t.Helper()
		status, err := operations.Status(t.Context(), 30)
		if err != nil {
			t.Fatal(err)
		}
		if len(status.RecentBlocked) != 1 || status.RecentBlocked[0].SourceID != "fixture" || status.RecentBlocked[0].OriginalTitle != "CAR & battery" || status.RecentBlocked[0].URL != server.URL+"/blocked" || !status.RecentBlocked[0].FirstBlockedAt.Equal(firstBlockedAt) {
			t.Fatalf("blocked contents = %+v", status.RecentBlocked)
		}
		if status.Sources[0].Health.NewBlocked != 1 {
			t.Fatalf("new blocked = %d", status.Sources[0].Health.NewBlocked)
		}
	}
	checkBlocked()
	now = now.Add(time.Minute)
	if err := app.Collector.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	checkBlocked()
	now = now.Add(time.Minute)
	cfg.Sources[0].Block = nil
	collector := biz.NewCollector(data.NewCollectionRepo(app.Data), data.NewFeedFetcher(fetch.New(true, cfg.Fetch), nil), biz.NewCollectorOptions(cfg), func() time.Time { return now })
	failNext.Store(true)
	if err := collector.RunOnce(t.Context()); err == nil {
		t.Fatal("expected failed revalidation")
	}
	source, err := data.NewCollectionRepo(app.Data).DueSources(t.Context(), now.Add(time.Minute))
	if err != nil || len(source) != 1 || source[0].ETag != `"block-v1"` || source[0].LastModified != "Wed, 21 Oct 2026 10:00:00 GMT" {
		t.Fatalf("failed revalidation damaged validators: %+v, %v", source, err)
	}
	now = now.Add(time.Minute)
	if err := collector.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 9 {
		t.Fatalf("relaxed block collected %d, want 9", count)
	}
	now = now.Add(time.Minute)
	if err := collector.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if conditionalRequests.Load() != 2 {
		t.Fatalf("ordinary later requests did not use validators: %d", conditionalRequests.Load())
	}
	now = now.Add(time.Minute)
	cfg.Sources[0].Block = [][]string{{"car"}}
	collector = biz.NewCollector(data.NewCollectionRepo(app.Data), data.NewFeedFetcher(fetch.New(true, cfg.Fetch), nil), biz.NewCollectorOptions(cfg), func() time.Time { return now })
	if err := collector.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 9 {
		t.Fatalf("tightened block changed existing items: %d", count)
	}
	checkBlocked()
}

func TestCollectorRecordsBlockedContentsAfterInitialEight(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	now := time.Now().UTC().Truncate(time.Second)
	firstBlockedAt := now
	var articleRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/feed" {
			articleRequests.Add(1)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<rss version="2.0"><channel><title>Fixture</title>`)
		for i := range 8 {
			fmt.Fprintf(w, `<item><title>ordinary %d</title><link>%s/kept-%d</link><description>%s</description></item>`, i, server.URL, i, fixtureBody)
		}
		fmt.Fprintf(w, `<item><title>CAR &amp;amp; battery</title><link>%s/blocked?utm_source=one</link></item>`, server.URL)
		fmt.Fprintf(w, `<item><title>duplicate car</title><link>%s/blocked?utm_source=two</link></item>`, server.URL)
		fmt.Fprintf(w, `<item><title>ordinary overflow</title><link>%s/overflow</link></item>`, server.URL)
		fmt.Fprint(w, `</channel></rss>`)
	}))
	defer server.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: server.URL + "/feed", Enabled: true, IntervalMinutes: 1, Block: [][]string{{"car"}}}}
	app, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, cfg.Fetch), nil))
	if err != nil {
		t.Fatal("assemble collector")
	}
	defer cleanup()
	if err := app.Collector.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := biz.NewOperationsUseCase(data.NewOperationsRepo(app.Data)).Status(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.RecentBlocked) != 1 || status.RecentBlocked[0].OriginalTitle != "CAR & battery" || status.RecentBlocked[0].URL != server.URL+"/blocked" || !status.RecentBlocked[0].FirstBlockedAt.Equal(firstBlockedAt) {
		t.Fatalf("tail blocked contents = %+v", status.RecentBlocked)
	}
	if status.Sources[0].Health.NewBlocked != 1 || articleRequests.Load() != 0 {
		t.Fatalf("blocked=%d article requests=%d", status.Sources[0].Health.NewBlocked, articleRequests.Load())
	}
	if status.Sources[0].ItemCount != 8 || status.Stages[biz.StageNew] != 8 {
		t.Fatalf("initial collection = %d, new = %d, want 8", status.Sources[0].ItemCount, status.Stages[biz.StageNew])
	}
}
