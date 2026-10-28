package integration

import (
	"encoding/json"
	"encoding/xml"
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

func TestRoundupRemainsInAllItemsButNotSelectedOrDailyOutputs(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, `<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>Fixture</description><item><title>AI roundup</title><link>https://example.test/roundup</link><guid>roundup-item</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item></channel></rss>`, strings.Repeat("Long local feed summary content. ", 20))
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
			writeFakeCompletion(w, `{"attention_score":90}`)
		case strings.Contains(prompt, "事件归组"):
			writeFakeCompletion(w, `{"relations":[]}`)
		default:
			writeFakeCompletion(w, `{"title":"AI 综合动态","summary":"汇总多件独立事件。","subjects":["openai"],"fact":{"title":"本不应保存的事实","subject":"OpenAI","action":"发布","object":"产品","occurred_at":"2026-10-22"},"roundup":true,"category":"行业","tags":[]}`)
		}
	}))
	defer llmServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = llmServer.URL
	cfg.LLM.APIKey = "fake-test-key"
	cfg.LLM.Model = "fake-model"
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture Feed", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	now := time.Date(2026, time.October, 22, 7, 55, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("process roundup item: %v", err)
	}
	var factIsNull bool
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), `SELECT fact IS NULL FROM items WHERE url='https://example.test/roundup'`).Scan(&factIsNull); err != nil {
		t.Fatal(err)
	}
	if !factIsNull {
		t.Fatal("roundup retained fact")
	}

	all := requestFoldItems(t, application.Router, "/api/items?view=all")
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(all, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("all-items count = %d, want roundup retained: %s", len(page.Items), all)
	}
	itemID := page.Items[0].ID
	for _, path := range []string{"/api/items?view=selected", "/feed.xml"} {
		if path == "/feed.xml" {
			response := httptest.NewRecorder()
			application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			var rss struct {
				Channel struct {
					Items []struct {
						Title string `xml:"title"`
					} `xml:"item"`
				} `xml:"channel"`
			}
			if response.Code != http.StatusOK || xml.Unmarshal(response.Body.Bytes(), &rss) != nil || len(rss.Channel.Items) != 0 {
				t.Errorf("GET %s status/items = %d/%+v, want empty feed", path, response.Code, rss.Channel.Items)
			}
			continue
		}
		selected := requestFoldItems(t, application.Router, path)
		var selectedPage struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(selected, &selectedPage); err != nil || len(selectedPage.Items) != 0 {
			t.Errorf("GET %s returned %s, want empty list", path, selected)
		}
	}
	detail := httptest.NewRecorder()
	application.Router.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/items/"+itemID, nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "AI 综合动态") {
		t.Errorf("GET item detail status/body = %d/%s, want roundup detail", detail.Code, detail.Body.String())
	}
	if _, err := application.Reports.Generate(t.Context(), "2026-10-22"); err != nil {
		t.Fatalf("generate report: %v", err)
	}
	report := httptest.NewRecorder()
	application.Router.ServeHTTP(report, httptest.NewRequest(http.MethodGet, "/api/reports/2026-10-22", nil))
	if report.Code != http.StatusOK || strings.Contains(report.Body.String(), "AI 综合动态") {
		t.Errorf("GET report status/body = %d/%s, want report without roundup", report.Code, report.Body.String())
	}
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feed/all.xml", nil))
	var rss struct {
		Channel struct {
			Items []struct {
				Title string `xml:"title"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if response.Code != http.StatusOK || xml.Unmarshal(response.Body.Bytes(), &rss) != nil || len(rss.Channel.Items) != 1 {
		t.Fatalf("GET /feed/all.xml status/items = %d/%+v, want roundup retained", response.Code, rss.Channel.Items)
	}
}

func TestRoundupDoesNotAttractLaterOrdinaryItemIntoItsStory(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = io.WriteString(w, fmt.Sprintf(`<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>Fixture</description><item><title>OpenAI Atlas roundup</title><link>https://example.test/roundup-first</link><guid>roundup-first</guid><pubDate>Wed, 21 Oct 2026 08:00:00 GMT</pubDate><description>%s</description></item><item><title>OpenAI Atlas ordinary update</title><link>https://example.test/ordinary-after</link><guid>ordinary-after</guid><pubDate>Wed, 21 Oct 2026 09:00:00 GMT</pubDate><description>%s</description></item></channel></rss>`, strings.Repeat("OpenAI Atlas details and research update. ", 12), strings.Repeat("OpenAI Atlas details and research update. ", 12)))
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
		var prompt, material string
		for _, message := range request.Messages {
			if message.Role == "system" {
				prompt = message.Content
			} else if message.Role == "user" {
				material = message.Content
			}
		}
		switch {
		case strings.Contains(prompt, "相关性预筛"):
			writeFakeCompletion(w, `{"label":"PASS"}`)
		case strings.Contains(prompt, "事件注意力评分器"):
			writeFakeCompletion(w, `{"attention_score":90}`)
		case strings.Contains(prompt, "事件归组"):
			relations, err := fakeStoryRelations(material, "SAME_OCCURRENCE")
			if err != nil {
				http.Error(w, "invalid grouping material", http.StatusBadRequest)
				return
			}
			writeFakeCompletion(w, relations)
		default:
			var item struct {
				Title string `json:"title"`
			}
			if err := json.Unmarshal([]byte(material), &item); err != nil {
				http.Error(w, "invalid writing material", http.StatusBadRequest)
				return
			}
			isRoundup := strings.Contains(item.Title, "roundup")
			content, err := json.Marshal(map[string]any{"title": item.Title, "summary": "OpenAI Atlas details and research update.", "subjects": []string{}, "fact": nil, "roundup": isRoundup, "category": "行业"})
			if err != nil {
				http.Error(w, "encode fake writing", http.StatusInternalServerError)
				return
			}
			writeFakeCompletion(w, string(content))
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
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("process fixture items: %v", err)
	}
	all := requestFoldItems(t, application.Router, "/api/items?view=all")
	var page struct {
		Items []struct {
			OriginalTitle string `json:"original_title"`
			StoryID       string `json:"story_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(all, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].StoryID == "" || page.Items[1].StoryID == "" || page.Items[0].StoryID == page.Items[1].StoryID {
		t.Fatalf("roundup and later ordinary story IDs = %+v, want two independent stories", page.Items)
	}
}
