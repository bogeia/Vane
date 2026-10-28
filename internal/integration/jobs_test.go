package integration

import (
	"context"
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

func TestRunnerStartsCollectionUntilCanceled(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	var feedRequests atomic.Int32
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		feedRequests.Add(1)
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>Fixture</description><item><title>Job fixture</title><link>https://example.test/job-item</link><guid>job-item</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item></channel></rss>`, fixtureBody)
	}))
	defer feedServer.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "job-fixture", Name: "Job fixture", Tier: "official", FeedURL: feedServer.URL,
		Enabled: true, IntervalMinutes: 60,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble app: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan struct{})
	go func() { application.Jobs.Run(ctx); close(finished) }()
	httpServer := httptest.NewServer(application.Router)
	defer httpServer.Close()

	client := &http.Client{Timeout: time.Second}
	baseURL := httpServer.URL
	deadline := time.Now().Add(10 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		response, requestErr := client.Get(baseURL + "/healthz")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && feedRequests.Load() > 0 {
				ready = true
				break
			}
		}
		select {
		case <-t.Context().Done():
			t.Fatal("test canceled while waiting for Runner.Run")
		case <-time.After(25 * time.Millisecond):
		}
	}
	if !ready {
		cancel()
		t.Fatal("Runner.Run did not start the initial collection")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("Runner.Run did not stop after cancellation")
	}
}
