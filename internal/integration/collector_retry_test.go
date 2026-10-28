package integration

import (
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

func TestCollectorPersistsRetryAfterBeyondSourceInterval(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	now := time.Now().UTC().Truncate(time.Second)
	clock := now
	until := now.Add(time.Hour)
	var requests atomic.Int32
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", until.Format(http.TimeFormat))
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer feedServer.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 2}}
	cfg.Fetch.RequestTimeout = time.Second
	app, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return clock }, data.NewFeedFetcher(fetch.New(true, cfg.Fetch), nil))
	if err != nil {
		t.Fatalf("assemble collector: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := app.Collector.RunOnce(t.Context()); err == nil {
		t.Fatal("missing source failure")
	}
	var next time.Time
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT next_collect_at FROM sources WHERE id = 'fixture'").Scan(&next); err != nil {
		t.Fatal("read source schedule")
	}
	if !next.Equal(until) {
		t.Fatalf("next_collect_at=%v want %v", next, until)
	}
	clock = now.Add(2 * time.Minute)
	if err := app.Collector.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests before server deadline=%d", requests.Load())
	}
	due, err := data.NewCollectionRepo(app.Data).DueSources(t.Context(), until)
	if err != nil || len(due) != 1 || due[0].ID != "fixture" {
		t.Fatalf("source not due at server deadline: sources=%+v err=%v", due, err)
	}
}
