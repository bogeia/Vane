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

func TestCollectorCompletesShortContentAndFallsBackOnArticleError(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	articleParagraph := strings.Repeat("Substantial article detail supporting the original reporting. ", 10)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed":
			w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
			_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Fixture</title><link>%s</link><description>fixture</description>
<item><title>Article success</title><link>%s/article-success</link><guid>article-success</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>Short RSS summary.</description></item>
<item><title>Article failure</title><link>%s/article-failure</link><guid>article-failure</guid><pubDate>Wed, 21 Oct 2026 11:00:00 GMT</pubDate><description>Short RSS fallback summary.</description></item>
</channel></rss>`, server.URL, server.URL, server.URL)
		case "/article-success":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, `<!doctype html><html><head><title>Expanded story</title><meta name="description" content="Detailed article"></head><body><article><h1>Expanded story</h1><p>%s</p><p>%s</p><p>%s</p><p>%s</p></article></body></html>`, articleParagraph, articleParagraph, articleParagraph, articleParagraph)
		case "/article-failure":
			http.Error(w, "article unavailable", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: server.URL + "/feed",
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

	bodies := make(map[string]string)
	rows, err := testDB(t, application.Data).QueryContext(t.Context(), "SELECT url, body FROM items")
	if err != nil {
		t.Fatal("read collected item bodies")
	}
	defer rows.Close()
	for rows.Next() {
		var url, body string
		if err := rows.Scan(&url, &body); err != nil {
			t.Fatal("scan collected item body")
		}
		bodies[url] = body
	}
	if err := rows.Err(); err != nil {
		t.Fatal("iterate collected item bodies")
	}

	completed := bodies[server.URL+"/article-success"]
	if len(completed) < 400 || !strings.Contains(completed, "Substantial article detail") {
		t.Errorf("short feed summary was not completed from the article (body length %d)", len(completed))
	}
	if fallback := bodies[server.URL+"/article-failure"]; fallback != "Short RSS fallback summary." {
		t.Errorf("article failure body = %q, want original RSS summary", fallback)
	}
}

func TestCollectorDisabledDoesNotRequestFeedOrArticle(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	var feedRequests atomic.Int32
	var articleRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed":
			feedRequests.Add(1)
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = io.WriteString(w, "<rss version=\"2.0\"><channel><title>Fixture</title></channel></rss>")
		case "/article":
			articleRequests.Add(1)
			_, _ = io.WriteString(w, "<html><body>article</body></html>")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = false
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: server.URL + "/feed",
		Enabled: true, IntervalMinutes: 1,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, func() time.Time {
		return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	}, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble collector app: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("run disabled collector: %v", err)
	}
	if got := feedRequests.Load(); got != 0 {
		t.Errorf("RSS requests with collection disabled = %d, want 0", got)
	}
	if got := articleRequests.Load(); got != 0 {
		t.Errorf("article requests with collection disabled = %d, want 0", got)
	}
}
