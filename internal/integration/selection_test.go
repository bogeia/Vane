package integration

import (
	"encoding/json"
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
	"github.com/bogeia/vane/internal/infra/llm"
	"github.com/bogeia/vane/internal/service"
)

func TestProcessorUsesConfiguredThresholdWithoutReselectingPublishedItems(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, `<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description><item><title>AI research announcement</title><link>https://example.test/threshold</link><guid>threshold</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item></channel></rss>`, fixtureBody)
	}))
	defer feedServer.Close()
	var scores atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) == 0 {
			http.Error(w, "invalid fake model request", http.StatusBadRequest)
			return
		}
		switch prompt := request.Messages[0].Content; {
		case strings.Contains(prompt, "相关性预筛"):
			writeFakeCompletion(w, `{"label":"PASS"}`)
		case strings.Contains(prompt, "事件注意力评分器"):
			score := 59
			if scores.Add(1) == 2 {
				score = 61
			}
			writeFakeCompletion(w, fmt.Sprintf(`{"attention_score":%d}`, score))
		case strings.Contains(prompt, "事件归组"):
			writeFakeCompletion(w, `{"relations":[]}`)
		default:
			writeFakeCompletion(w, `{"title":"门槛配置生效","summary":"两次评分达到配置门槛。","subjects":[],"fact":null,"roundup":false,"category":"论文","tags":[]}`)
		}
	}))
	defer modelServer.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled, cfg.LLM.Enabled = true, true
	cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model = modelServer.URL, "fake-test-key", "fake-model"
	cfg.Selection.Thresholds.Media = 60
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "media", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, cfg.Fetch), nil))
	if err != nil {
		t.Fatal("assemble application failed")
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertSelected := func() {
		t.Helper()
		body := requestFoldItems(t, application.Router, "/api/items?view=selected")
		var page struct {
			Items []service.Item `json:"items"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].Title != "门槛配置生效" {
			t.Fatalf("expected media item selected at threshold 60: %s", body)
		}
	}
	assertSelected()
	cfg.Selection.Thresholds.Media = 100
	options := biz.NewProcessorOptions(cfg)
	options.Enabled = true
	application.Processor = biz.NewProcessor(data.NewWorkRepo(application.Data), llm.New(cfg.LLM), options, now, logger)
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertSelected()
	if scores.Load() != 2 {
		t.Fatalf("score calls=%d, want two without reprocessing published items", scores.Load())
	}
}
