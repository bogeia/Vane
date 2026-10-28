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

func TestWrittenCompanyTopicsUseSubjectsOrUniqueTitleEvidence(t *testing.T) {
	for _, tc := range []struct{ name, original, title, subjects, want string }{
		{"multiple subjects", "New research announcement", "OpenAI 发布新研究", `["openai","microsoft"]`, "openai"},
		{"single subject fallback", "New research announcement", "新的推理研究发布", `["anthropic"]`, "anthropic"},
		{"original title evidence", "Claude research announcement", "新的推理研究发布", `["anthropic","openai"]`, "anthropic"},
		{"no subject with unique title company", "Anthropic research announcement", "Claude 发布新研究", `[]`, "anthropic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			databaseURL := testDatabaseURL(t)
			resetTestSchema(t, databaseURL)
			feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/rss+xml")
				fmt.Fprintf(w, `<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture</description><item><title>%s</title><link>https://example.test/company-item</link><guid>company-item</guid><description>%s</description></item></channel></rss>`, tc.original, fixtureBody)
			}))
			defer feed.Close()
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeFakeModelResponse(w, r, `{"title":"`+tc.title+`","summary":"研究改进了推理能力并公布实验结果。","subjects":`+tc.subjects+`,"fact":null,"roundup":false,"category":"论文","tags":[]}`)
			}))
			defer model.Close()
			cfg := loadTestConfig(t, databaseURL)
			cfg.Collect.Enabled, cfg.LLM.Enabled = true, true
			cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model = model.URL, "fake-test-key", "fake-model"
			cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feed.URL, Enabled: true, IntervalMinutes: 60}}
			now := func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }
			app, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), now, data.NewFeedFetcher(fetch.New(true, cfg.Fetch), nil))
			if err != nil {
				t.Fatal(strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
			}
			t.Cleanup(cleanup)
			if err := app.Collector.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := app.Processor.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, company := range []string{"openai", "anthropic", "microsoft"} {
				var page itemPageResponse
				if err := json.Unmarshal(requestFoldItems(t, app.Router, "/api/items?view=all&topic="+company), &page); err != nil {
					t.Fatal(err)
				}
				want := 0
				if company == tc.want {
					want = 1
				}
				if len(page.Items) != want {
					t.Fatalf("company %s returned %d items want %d", company, len(page.Items), want)
				}
			}
		})
	}
}
