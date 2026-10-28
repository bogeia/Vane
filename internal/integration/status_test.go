package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/infra/fetch"
)

func TestStatusReportsFeedErrorsAndCollectionMetadata(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, `<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>Fixture</description><item><title>Successful item</title><link>https://example.test/status-item</link><guid>status-item</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item></channel></rss>`, strings.Repeat("Long local feed summary content. ", 20))
	}))
	defer feedServer.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{
		{Kind: biz.SourceKindRSS, ID: "healthy-fixture", Name: "Healthy fixture", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60},
		{Kind: biz.SourceKindRSS, ID: "private-fixture", Name: "Private fixture", Tier: "official", FeedURL: "http://10.20.30.40/feed", Enabled: true, IntervalMinutes: 60},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble collector app: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err == nil {
		t.Fatal("collector accepted a private source address")
	}

	operations, cleanupStatus, err := newTestStatus(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open status operations: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanupStatus)
	status, err := operations.Status(t.Context(), 7)
	if err != nil {
		t.Fatal("read status")
	}
	foundPrivate := false
	foundHealthy := false
	for _, source := range status.Sources {
		switch source.ID {
		case "private-fixture":
			foundPrivate = true
			if source.ItemCount != 0 {
				t.Errorf("private source item count = %d, want 0", source.ItemCount)
			}
			if source.LastError == "" {
				t.Error("status omitted the private-source fetch error")
			}
			if source.LastCollectedAt == nil || !source.LastCollectedAt.Equal(now) {
				t.Errorf("private source last collected at = %v, want %s", source.LastCollectedAt, now)
			}
		case "healthy-fixture":
			foundHealthy = true
			if source.ItemCount != 1 {
				t.Errorf("healthy source item count = %d, want 1", source.ItemCount)
			}
			if source.LastError != "" {
				t.Errorf("healthy source last error = %q, want empty", source.LastError)
			}
			if source.LastCollectedAt == nil || !source.LastCollectedAt.Equal(now) {
				t.Errorf("healthy source last collected at = %v, want %s", source.LastCollectedAt, now)
			}
		}
	}
	if !foundPrivate || !foundHealthy {
		t.Fatalf("status omitted configured sources: private=%v healthy=%v", foundPrivate, foundHealthy)
	}
}

func TestStatusIdentifiesScoreJSONFailureAfterRetries(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, `<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>Fixture</description><item><title>Score failure item</title><link>https://example.test/score-failure</link><guid>score-failure</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item></channel></rss>`, strings.Repeat("Long local feed summary content. ", 20))
	}))
	defer feedServer.Close()
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid fake model request", http.StatusBadRequest)
			return
		}
		prompt := request.Messages[0].Content
		switch {
		case strings.Contains(prompt, "相关性预筛"):
			writeFakeCompletion(w, `{"label":"PASS"}`)
		case strings.Contains(prompt, "事件注意力评分器"):
			writeFakeCompletion(w, `{"attention_score":"bad","private":"MODEL_PRIVATE_OUTPUT"}`)
		default:
			writeFakeCompletion(w, `{"title":"测试标题","summary":"测试摘要","subjects":[],"fact":null,"roundup":false,"category":"模型"}`)
		}
	}))
	defer llmServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = llmServer.URL
	cfg.LLM.APIKey = "fake-test-key"
	cfg.LLM.Model = "fake-model"
	cfg.Worker.Count = 1
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture Feed", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}
	for attempt, delay := range []time.Duration{0, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute} {
		now = now.Add(delay)
		if err := application.Processor.RunOnce(t.Context()); err == nil {
			t.Fatalf("processor attempt %d succeeded with invalid score JSON", attempt+1)
		}
	}
	status, err := biz.NewOperationsUseCase(data.NewOperationsRepo(application.Data)).Status(t.Context(), 7)
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	view := status
	if len(view.FailedItems) != 1 || view.FailedItems[0].OriginalTitle != "Score failure item" || view.FailedItems[0].Attempts != 5 {
		t.Fatalf("failed items = %+v, want the score item failed after five attempts", view.FailedItems)
	}
	lastError := view.FailedItems[0].LastError
	if !strings.Contains(lastError, "score") || strings.Contains(lastError, "writing") || strings.Contains(lastError, "MODEL_PRIVATE_OUTPUT") || strings.Contains(lastError, "fake-test-key") {
		t.Errorf("failed item error = %q, want score-specific and sanitized", lastError)
	}
}
