package integration

import (
	"database/sql"
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

func TestCollectorPersistenceFailureRecordsSourceErrorAndDefersRetry(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	var requests atomic.Int32
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/feed+json; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`{"version":"https://jsonfeed.org/version/1.1","title":"Fixture","items":[
{"id":"valid","url":"https://example.test/valid","title":"Valid item","content_text":%q,"date_published":"2026-10-22T12:00:00Z"},
{"id":"invalid","url":"https://example.test/invalid","title":"Invalid\u0000title","content_text":%q,"date_published":"2026-10-21T11:00:00Z"}
]}`, fixtureBody, fixtureBody))
	}))
	defer feedServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL,
		Enabled: true, IntervalMinutes: 60,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble collector app: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)

	if err := application.Collector.RunOnce(t.Context()); err == nil {
		t.Fatal("collector run succeeded despite a database persistence error")
	}
	var itemCount int
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&itemCount); err != nil {
		t.Fatal("read persisted item count")
	}
	if itemCount != 0 {
		t.Fatalf("persisted items after transaction failure = %d, want 0", itemCount)
	}

	var firstCollected bool
	var lastError string
	var lastCollectedAt *time.Time
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), `
		SELECT first_collected, last_error, last_collected_at
		FROM sources WHERE id = 'fixture'
	`).Scan(&firstCollected, &lastError, &lastCollectedAt); err != nil {
		t.Fatal("read source state after persistence failure")
	}
	if firstCollected {
		t.Fatal("source marked first_collected after a failed transaction")
	}
	if lastError == "" {
		t.Fatal("source did not record the persistence error")
	}
	if lastCollectedAt == nil || !lastCollectedAt.Equal(now) {
		t.Fatalf("last_collected_at = %v, want %s", lastCollectedAt, now)
	}

	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collector retry at unchanged clock: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("feed requests at unchanged clock = %d, want 1", got)
	}
}

func TestCollectorReportsPersistenceAndFailureRecordingErrors(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	database := make(chan *sql.DB, 1)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/feed+json; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`{"version":"https://jsonfeed.org/version/1.1","title":"Fixture","items":[
{"id":"fixture-item","url":"https://example.test/item","title":"Fixture item","content_text":%q,"date_published":"2026-10-21T10:00:00Z"}
]}`, fixtureBody))
		_ = (<-database).Close()
	}))
	defer feedServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL,
		Enabled: true, IntervalMinutes: 60,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, func() time.Time {
		return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	}, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble collector app: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	database <- testDB(t, application.Data)

	err = application.Collector.RunOnce(t.Context())
	if err == nil {
		t.Fatal("collector run succeeded after the database connection was closed")
	}
	message := err.Error()
	for _, expected := range []string{"save source fixture", "record source fixture failure", "database is closed"} {
		if !strings.Contains(message, expected) {
			t.Errorf("collector error %q does not include %q", message, expected)
		}
	}
}
