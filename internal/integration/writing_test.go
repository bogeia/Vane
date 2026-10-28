package integration

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
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

func TestCollectedItemIsWrittenAndAppearsInAllItemsAPI(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>Original AI research announcement</title><link>https://example.test/items/1</link><guid>fixture-item-1</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody))
	}))
	defer feedServer.Close()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer fake-test-key" {
			http.Error(w, "unexpected fake model request", http.StatusBadRequest)
			return
		}
		if kind := writeFakeModelResponse(w, r, `{}`); kind == "score" || kind == "write" {
			t.Errorf("default service received %s", kind)
		}
	}))
	defer llmServer.Close()

	var scoreCalls, writeCalls atomic.Int32
	scoreServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if kind := writeFakeModelResponse(w, r, `{}`); kind != "score" {
			t.Errorf("score service received %s", kind)
		}
		scoreCalls.Add(1)
	}))
	defer scoreServer.Close()
	writeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if kind := writeFakeModelResponse(w, r, `{"title":"中文 AI 研究发布","summary":"这项研究带来了重要进展。","reason":"值得关注其后续影响。","subjects":["openai","not-listed","google","openai"],"fact":{"title":"OpenAI发布研究","subject":"OpenAI","action":"发布","object":"研究","occurred_at":"2026-02-30"},"roundup":false,"category":"论文","tags":["RAG","OpenAI"]}`); kind != "write" {
			t.Errorf("write service received %s", kind)
		}
		writeCalls.Add(1)
	}))
	defer writeServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = llmServer.URL
	cfg.LLM.APIKey = "fake-test-key"
	cfg.LLM.Model = "fake-model"
	cfg.LLM.Steps.Score = config.LLMStep{BaseURL: scoreServer.URL}
	cfg.LLM.Steps.Write = config.LLMStep{BaseURL: writeServer.URL}
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture Feed", Tier: "official", FeedURL: feedServer.URL,
		Enabled: true, IntervalMinutes: 60,
	}}
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("write collected item: %v", err)
	}

	if scoreCalls.Load() != 2 || writeCalls.Load() != 1 {
		t.Fatalf("separate services received score=%d write=%d calls", scoreCalls.Load(), writeCalls.Load())
	}

	var subjectsJSON, factJSON, topicsJSON []byte
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), `SELECT to_json(subjects),fact,to_json(topics) FROM items WHERE url='https://example.test/items/1'`).Scan(&subjectsJSON, &factJSON, &topicsJSON); err != nil {
		t.Fatal(err)
	}
	var topics []string
	if json.Unmarshal(topicsJSON, &topics) != nil || strings.Join(topics, ",") != "rag,paper" {
		t.Fatalf("stored topics=%s", topicsJSON)
	}
	var subjects []string
	var fact struct {
		Title, Subject, Action, Object string
		OccurredAt                     *string `json:"occurredAt"`
	}
	if json.Unmarshal(subjectsJSON, &subjects) != nil || strings.Join(subjects, ",") != "openai,google" {
		t.Fatalf("stored subjects=%s", subjectsJSON)
	}
	if json.Unmarshal(factJSON, &fact) != nil || fact.Title != "OpenAI发布研究" || fact.Subject != "OpenAI" || fact.Action != "发布" || fact.Object != "研究" || fact.OccurredAt != nil {
		t.Fatalf("stored fact=%s", factJSON)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil)
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/items?view=all status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	var result struct {
		Items []struct {
			ID            string    `json:"id"`
			Title         string    `json:"title"`
			OriginalTitle string    `json:"original_title"`
			Summary       string    `json:"summary"`
			Reason        string    `json:"reason"`
			URL           string    `json:"url"`
			ImageURL      *string   `json:"image_url"`
			PublishedAt   time.Time `json:"published_at"`
			Score         *int      `json:"score"`
			Selected      *bool     `json:"selected"`
			Category      string    `json:"category"`
			Tags          []string  `json:"tags"`
			StoryID       *string   `json:"story_id"`
			StoryOthers   *int      `json:"story_others"`
			Source        struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Tier string `json:"tier"`
			} `json:"source"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode all-items response: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("all-items count = %d, want one written item", len(result.Items))
	}
	item := result.Items[0]
	if item.ID == "" || item.Title != "中文 AI 研究发布" || item.OriginalTitle != "Original AI research announcement" || item.Summary != "这项研究带来了重要进展。" || item.Reason != "值得关注其后续影响。" {
		t.Fatalf("all-items writing fields = %+v, want Chinese writing and original title", item)
	}
	if item.URL != "https://example.test/items/1" || item.Category != "论文" || len(item.Tags) != 2 || item.Tags[0] != "RAG" || item.Tags[1] != "OpenAI" {
		t.Fatalf("all-items metadata = %+v, want source link, category and tags", item)
	}
	if item.Source.ID != "fixture" || item.Source.Name != "Fixture Feed" || item.Source.Tier != "official" {
		t.Fatalf("all-items source = %+v, want configured fixture source", item.Source)
	}
	if item.ImageURL == nil || item.Score == nil || item.Selected == nil || item.StoryID == nil || item.StoryOthers == nil {
		t.Fatalf("all-items response omitted a public field: %+v", item)
	}
	if !item.PublishedAt.Equal(time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("published_at = %s, want fixture publication time", item.PublishedAt)
	}
}

func TestBlockedItemIsHiddenAndDoesNotReachScoringOrWriting(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>Unrelated announcement</title><link>https://example.test/items/unrelated</link><guid>fixture-unrelated</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody))
	}))
	defer feedServer.Close()

	var prefilters, scores, writings atomic.Int32
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
		var prompt string
		for _, message := range request.Messages {
			if message.Role == "system" {
				prompt = message.Content
				break
			}
		}
		var content string
		switch {
		case strings.Contains(prompt, "相关性预筛"):
			prefilters.Add(1)
			content = `{"label":"BLOCK","reason":"与 AI 无关"}`
		case strings.Contains(prompt, "事件注意力评分器"):
			scores.Add(1)
			content = `{"attention_score":99}`
		default:
			writings.Add(1)
			content = `{"title":"不应生成","summary":"不应写作","subjects":[],"fact":null,"roundup":false,"category":"行业","tags":[]}`
		}
		writeFakeCompletion(w, content)
	}))
	defer llmServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = llmServer.URL
	cfg.LLM.APIKey = "fake-test-key"
	cfg.LLM.Model = "fake-model"
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture Feed", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("process blocked fixture: %v", err)
	}
	if prefilters.Load() != 1 || scores.Load() != 0 || writings.Load() != 0 {
		t.Fatalf("model calls = prefilter:%d score:%d write:%d, want only one prefilter", prefilters.Load(), scores.Load(), writings.Load())
	}

	for _, view := range []string{"all", "selected"} {
		response := httptest.NewRecorder()
		application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/items?view="+view, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET items view=%s returned %d: %s", view, response.Code, response.Body.String())
		}
		var page struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatalf("decode items view=%s: %v", view, err)
		}
		if len(page.Items) != 0 {
			t.Errorf("blocked item appears in view=%s: %s", view, page.Items[0])
		}
	}
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feed/all.xml", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /feed/all.xml returned %d: %s", response.Code, response.Body.String())
	}
	var rss struct {
		Channel struct {
			Items []struct {
				Title string `xml:"title"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(response.Body.Bytes(), &rss); err != nil {
		t.Fatalf("decode RSS XML: %v", err)
	}
	if len(rss.Channel.Items) != 0 {
		t.Fatalf("RSS contains blocked item: %+v", rss.Channel.Items)
	}
}

func TestSelectedCategoryAndLiteralSearchFilters(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>AI 100%%_ selected release</title><link>https://example.test/items/selected</link><guid>fixture-selected</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
<item><title>AI 100Xa ordinary release</title><link>https://example.test/items/ordinary</link><guid>fixture-ordinary</guid><pubDate>Wed, 21 Oct 2026 11:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody, fixtureBody))
	}))
	defer feedServer.Close()
	llmServer := httptest.NewServer(http.HandlerFunc(writeFilterFakeModelResponse))
	defer llmServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = llmServer.URL
	cfg.LLM.APIKey = "fake-test-key"
	cfg.LLM.Model = "fake-model"
	cfg.Worker.Count = 1
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("process fixture feed: %v", err)
	}

	getItems := func(target string) []struct {
		OriginalTitle string `json:"original_title"`
		Title         string `json:"title"`
		Summary       string `json:"summary"`
		Score         int    `json:"score"`
		Selected      bool   `json:"selected"`
		Category      string `json:"category"`
	} {
		t.Helper()
		response := httptest.NewRecorder()
		application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s returned %d: %s", target, response.Code, response.Body.String())
		}
		var page struct {
			Items []struct {
				OriginalTitle string `json:"original_title"`
				Title         string `json:"title"`
				Summary       string `json:"summary"`
				Score         int    `json:"score"`
				Selected      bool   `json:"selected"`
				Category      string `json:"category"`
			} `json:"items"`
		}
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatalf("decode %s: %v", target, err)
		}
		return page.Items
	}

	selected := getItems("/api/items?view=selected")
	if len(selected) != 1 || !selected[0].Selected || selected[0].Score != 80 || selected[0].Category != "论文" {
		t.Fatalf("selected items = %+v, want only the high-scoring research item", selected)
	}
	papers := getItems("/api/items?view=all&category=论文")
	if len(papers) != 1 || papers[0].OriginalTitle != "AI 100%_ selected release" {
		t.Fatalf("paper category results = %+v", papers)
	}
	ordinary := getItems("/api/items?view=all&category=行业")
	if len(ordinary) != 1 || ordinary[0].OriginalTitle != "AI 100Xa ordinary release" {
		t.Fatalf("industry category results = %+v", ordinary)
	}
	literalSearch := getItems("/api/items?view=all&q=100%25_")
	if len(literalSearch) != 1 || literalSearch[0].OriginalTitle != "AI 100%_ selected release" {
		t.Fatalf("literal percent/underscore search results = %+v", literalSearch)
	}

	for _, target := range []string{
		"/api/items?view=all&category=未知分类",
		"/api/items?view=other",
	} {
		response := httptest.NewRecorder()
		application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/problem+json") {
			t.Errorf("GET %s returned %d %q, want problem+json 400", target, response.Code, response.Header().Get("Content-Type"))
		}
	}
}

func TestRelatedSourcesBecomeOneStoryAndKeepOfficialRepresentative(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)

	type feedFixture struct {
		id, name, tier, title, guid, published string
	}
	feeds := []feedFixture{
		{id: "b-media", name: "Media One", tier: "media", title: "媒体报道 OpenAI Atlas 新模型", guid: "media-atlas-one", published: "Wed, 21 Oct 2026 10:00:00 GMT"},
		{id: "c-media", name: "Media Two", tier: "media", title: "媒体关注 OpenAI Atlas 模型上线", guid: "media-atlas-two", published: "Wed, 21 Oct 2026 10:00:00 GMT"},
		{id: "a-official", name: "Official", tier: "official", title: "官方发布 OpenAI Atlas 模型", guid: "official-atlas", published: "Wed, 21 Oct 2026 11:00:00 GMT"},
	}
	feedServers := make([]*httptest.Server, 0, len(feeds))
	for index := range feeds {
		fixture := &feeds[index]
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
			_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>%s</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>%s</title><link>https://example.test/items/%s</link><guid>%s</guid><pubDate>%s</pubDate><description>%s</description></item>
</channel></rss>`, fixture.name, fixture.title, fixture.guid, fixture.guid, fixture.published, fixtureBody))
		}))
		feedServers = append(feedServers, server)
		defer server.Close()
	}

	var prefilters, scores, writings, groupings atomic.Int32
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
			switch message.Role {
			case "system":
				prompt = message.Content
			case "user":
				material = message.Content
			}
		}
		switch {
		case strings.Contains(prompt, "相关性预筛"):
			prefilters.Add(1)
			writeFakeCompletion(w, `{"label":"PASS","reason":"AI 相关"}`)
		case strings.Contains(prompt, "事件注意力评分器"):
			scores.Add(1)
			var item struct {
				Title string `json:"title"`
			}
			_ = json.Unmarshal([]byte(material), &item)
			if strings.Contains(item.Title, "官方") {
				writeFakeCompletion(w, `{"attention_score":60}`)
			} else {
				writeFakeCompletion(w, `{"attention_score":100}`)
			}
		case strings.Contains(prompt, "事件归组"):
			groupings.Add(1)
			relations, err := fakeStoryRelations(material, "SAME_OCCURRENCE")
			if err != nil {
				http.Error(w, "invalid grouping material", http.StatusBadRequest)
				return
			}
			writeFakeCompletion(w, relations)
		default:
			writings.Add(1)
			var item struct {
				Title string `json:"title"`
			}
			if err := json.Unmarshal([]byte(material), &item); err != nil {
				http.Error(w, "invalid writing material", http.StatusBadRequest)
				return
			}
			if strings.Contains(item.Title, "官方") {
				writeFakeCompletion(w, `{"title":"官方：OpenAI 发布 Atlas","summary":"OpenAI 发布 Atlas 新模型，性能和行业进展受到关注。","reason":"官方原文确认发布。","subjects":[],"fact":null,"roundup":false,"category":"模型","tags":["OpenAI"]}`)
			} else {
				writeFakeCompletion(w, `{"title":"媒体稿：OpenAI Atlas 模型","summary":"OpenAI 发布 Atlas 新模型，性能和行业进展受到关注。","reason":"媒体报道同一发布。","subjects":[],"fact":null,"roundup":false,"category":"模型","tags":["OpenAI"]}`)
			}
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
	cfg.Sources = nil
	for index, fixture := range feeds {
		cfg.Sources = append(cfg.Sources, config.Source{
			Kind: biz.SourceKindRSS, ID: fixture.id, Name: fixture.name, Tier: fixture.tier, FeedURL: feedServers[index].URL,
			Enabled: true, IntervalMinutes: 60,
		})
	}
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect source fixtures: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("process source fixtures: %v", err)
	}
	if prefilters.Load() != 3 || scores.Load() != 6 || writings.Load() != 3 || groupings.Load() != 2 {
		t.Fatalf("model calls = prefilter:%d score:%d write:%d group:%d, want one complete pass for all three sources", prefilters.Load(), scores.Load(), writings.Load(), groupings.Load())
	}

	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/items?view=all returned %d: %s", response.Code, response.Body.String())
	}
	var page struct {
		Items []struct {
			OriginalTitle string `json:"original_title"`
			Score         int    `json:"score"`
			StoryID       string `json:"story_id"`
			Source        struct {
				ID   string `json:"id"`
				Tier string `json:"tier"`
			} `json:"source"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode all-items response: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("all-items count = %d, want all three source articles", len(page.Items))
	}
	storyID := page.Items[0].StoryID
	if storyID == "" {
		t.Fatalf("grouping left the first item without a story: %+v", page.Items[0])
	}
	var officialFound bool
	var officialTier string
	var officialScore int
	for _, item := range page.Items {
		if item.StoryID != storyID {
			t.Errorf("item %q has story %q, want shared story %q", item.OriginalTitle, item.StoryID, storyID)
		}
		if item.Source.ID == "a-official" {
			officialFound = true
			officialTier = item.Source.Tier
			officialScore = item.Score
		}
	}
	if !officialFound || officialTier != "official" || officialScore >= 100 {
		t.Fatalf("official article tier/score = %s/%d, want the lower-scoring official source in the shared story", officialTier, officialScore)
	}
	storyResponse := httptest.NewRecorder()
	storyPath := "/api/stories/" + url.PathEscape(storyID)
	application.Router.ServeHTTP(storyResponse, httptest.NewRequest(http.MethodGet, storyPath, nil))
	if storyResponse.Code != http.StatusOK {
		t.Fatalf("GET %s returned %d, want 200: %s", storyPath, storyResponse.Code, storyResponse.Body.String())
	}
	var story struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Items []struct {
			PublishedAt time.Time `json:"published_at"`
			Source      struct {
				ID string `json:"id"`
			} `json:"source"`
		} `json:"items"`
	}
	if err := json.NewDecoder(storyResponse.Body).Decode(&story); err != nil {
		t.Fatalf("decode story response: %v", err)
	}
	if story.ID != storyID || story.Title != "官方：OpenAI 发布 Atlas" || len(story.Items) != 3 {
		t.Fatalf("story details = %+v, want official representative title and all three articles", story)
	}
	wantTimeline := []string{"b-media", "c-media", "a-official"}
	for index, item := range story.Items {
		if item.Source.ID != wantTimeline[index] {
			t.Errorf("story timeline item %d source = %q, want %q", index, item.Source.ID, wantTimeline[index])
		}
	}
	if !story.Items[0].PublishedAt.Equal(story.Items[1].PublishedAt) || story.Items[1].PublishedAt.After(story.Items[2].PublishedAt) {
		t.Errorf("story timeline is not ordered by publication time with stable ties: %+v", story.Items)
	}

	for _, path := range []string{"/api/stories/9223372036854775807"} {
		unknownResponse := httptest.NewRecorder()
		application.Router.ServeHTTP(unknownResponse, httptest.NewRequest(http.MethodGet, path, nil))
		if unknownResponse.Code != http.StatusNotFound {
			t.Errorf("GET %s returned %d, want 404", path, unknownResponse.Code)
		}
	}
	var unpublishedID int64
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), `INSERT INTO stories(title,created_at) VALUES('unpublished fixture', $1) RETURNING id`, now()).Scan(&unpublishedID); err != nil {
		t.Fatalf("seed unpublished story: %v", err)
	}
	unpublishedResponse := httptest.NewRecorder()
	unpublishedPath := "/api/stories/" + strconv.FormatInt(unpublishedID, 10)
	application.Router.ServeHTTP(unpublishedResponse, httptest.NewRequest(http.MethodGet, unpublishedPath, nil))
	if unpublishedResponse.Code != http.StatusNotFound {
		t.Errorf("GET unpublished story returned %d, want 404", unpublishedResponse.Code)
	}
	invalidResponse := httptest.NewRecorder()
	application.Router.ServeHTTP(invalidResponse, httptest.NewRequest(http.MethodGet, "/api/stories/not-an-id", nil))
	if invalidResponse.Code != http.StatusBadRequest || !strings.HasPrefix(invalidResponse.Header().Get("Content-Type"), "application/problem+json") {
		t.Errorf("GET invalid story id returned %d %q, want 400 problem+json", invalidResponse.Code, invalidResponse.Header().Get("Content-Type"))
	}
}

func TestUnrelatedAndRoundupItemsStartSeparateStories(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>OpenAI Atlas baseline</title><link>https://example.test/items/base</link><guid>story-base</guid><pubDate>Wed, 21 Oct 2026 08:00:00 GMT</pubDate><description>%s</description></item>
<item><title>OpenAI Atlas unrelated update</title><link>https://example.test/items/unrelated</link><guid>story-unrelated</guid><pubDate>Wed, 21 Oct 2026 09:00:00 GMT</pubDate><description>%s</description></item>
<item><title>OpenAI Atlas roundup</title><link>https://example.test/items/roundup</link><guid>story-roundup</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody, fixtureBody, fixtureBody))
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
			writeFakeCompletion(w, `{"label":"PASS","reason":"相关"}`)
		case strings.Contains(prompt, "事件注意力评分器"):
			writeFakeCompletion(w, `{"attention_score":80}`)
		case strings.Contains(prompt, "事件归组"):
			var input struct {
				Title      string `json:"title"`
				Candidates []struct {
					StoryID int64 `json:"story_id"`
				} `json:"candidates"`
			}
			if err := json.Unmarshal([]byte(material), &input); err != nil {
				http.Error(w, "invalid grouping material", http.StatusBadRequest)
				return
			}
			if len(input.Candidates) == 0 {
				writeFakeCompletion(w, `{"relations":[]}`)
				return
			}
			relation := "UNRELATED"
			if strings.Contains(input.Title, "roundup") {
				relation = "SAME_OCCURRENCE"
			}
			var relations []string
			for _, candidate := range input.Candidates {
				relations = append(relations, fmt.Sprintf(`{"story_id":%d,"relation":%q,"confidence":0.95}`, candidate.StoryID, relation))
			}
			writeFakeCompletion(w, `{"relations":[`+strings.Join(relations, ",")+`]}`)
		default:
			var item struct {
				Title string `json:"title"`
			}
			if err := json.Unmarshal([]byte(material), &item); err != nil {
				http.Error(w, "invalid writing material", http.StatusBadRequest)
				return
			}
			writeFakeCompletion(w, fmt.Sprintf(`{"title":%q,"summary":"OpenAI Atlas event update with enough factual context to group related reports.","reason":"fixture","subjects":[],"fact":null,"roundup":%t,"category":"模型","tags":["OpenAI"]}`, item.Title, strings.Contains(item.Title, "roundup")))
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
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "story-fixture", Name: "Story fixture", Tier: "media", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("process fixture: %v", err)
	}
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/items?view=all returned %d: %s", response.Code, response.Body.String())
	}
	var page struct {
		Items []struct {
			OriginalTitle string `json:"original_title"`
			StoryID       string `json:"story_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode all-items response: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("all-items count = %d, want 3", len(page.Items))
	}
	storyIDs := map[string]string{}
	for _, item := range page.Items {
		storyIDs[item.OriginalTitle] = item.StoryID
	}
	if storyIDs["OpenAI Atlas baseline"] == "" || storyIDs["OpenAI Atlas unrelated update"] == "" || storyIDs["OpenAI Atlas roundup"] == "" {
		t.Fatalf("items missing story ids: %+v", storyIDs)
	}
	if storyIDs["OpenAI Atlas baseline"] == storyIDs["OpenAI Atlas unrelated update"] || storyIDs["OpenAI Atlas baseline"] == storyIDs["OpenAI Atlas roundup"] || storyIDs["OpenAI Atlas unrelated update"] == storyIDs["OpenAI Atlas roundup"] {
		t.Fatalf("UNRELATED/ROUNDUP material merged into an existing story: %+v", storyIDs)
	}
}

func TestAnalyzedItemGroupsWithoutRepeatingCompletedModelSteps(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	var groupCalls, otherCalls atomic.Int32
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
		var material string
		for _, message := range request.Messages {
			if message.Role == "user" {
				material = message.Content
			}
		}
		for _, message := range request.Messages {
			if message.Role == "system" && strings.Contains(message.Content, "事件归组") {
				groupCalls.Add(1)
				relations, err := fakeStoryRelations(material, "SAME_OCCURRENCE")
				if err != nil {
					http.Error(w, "invalid grouping material", http.StatusBadRequest)
					return
				}
				writeFakeCompletion(w, relations)
				return
			}
		}
		otherCalls.Add(1)
		http.Error(w, "analyzed item repeated a completed model step", http.StatusInternalServerError)
	}))
	defer llmServer.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Collect.Enabled = false
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = llmServer.URL
	cfg.LLM.APIKey = "fake-test-key"
	cfg.LLM.Model = "fake-model"
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if _, err := testDB(t, application.Data).ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('analyzed','Analyzed','official','https://example.test/feed',false,60)`); err != nil {
		t.Fatalf("seed analyzed source: %v", err)
	}
	var analyzedID int64
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), `INSERT INTO items(source_id,url,original_title,body,published_at,stage,attempts,next_attempt_at,prefilter,score1,score2,title,summary,reason,category,tags,selected) VALUES('analyzed','https://example.test/analyzed','OpenAI Atlas analyzed item',$1,$2,'analyzed',0,$2,'PASS',60,70,'OpenAI Atlas completed writing','Completed summary','Completed reason','模型',ARRAY['OpenAI'],false) RETURNING id`, fixtureBody, now().Add(-time.Hour)).Scan(&analyzedID); err != nil {
		t.Fatalf("seed analyzed item: %v", err)
	}
	seed := groupItem(t, application.Data, now().Add(-time.Hour), 999)
	if err := data.NewWorkRepo(application.Data).Group(t.Context(), seed, now(), biz.PlanGrouping(seed, now(), func(context.Context, []biz.StoryCandidate) (int64, error) { return 0, nil })); err != nil {
		t.Fatal(err)
	}
	before := httptest.NewRecorder()
	application.Router.ServeHTTP(before, httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil))
	var beforePage struct {
		Items []json.RawMessage `json:"items"`
	}
	if before.Code != http.StatusOK || json.Unmarshal(before.Body.Bytes(), &beforePage) != nil || len(beforePage.Items) != 1 {
		t.Fatalf("analyzed item should remain unpublished until grouped; response %d %s", before.Code, before.Body.String())
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("group analyzed item: %v", err)
	}
	if groupCalls.Load() != 1 || otherCalls.Load() != 0 {
		t.Fatalf("model calls group:%d other:%d, want only one grouping call", groupCalls.Load(), otherCalls.Load())
	}
	after := httptest.NewRecorder()
	application.Router.ServeHTTP(after, httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil))
	var afterPage struct {
		Items []struct {
			ID      string `json:"id"`
			StoryID string `json:"story_id"`
		} `json:"items"`
	}
	if after.Code != http.StatusOK || json.Unmarshal(after.Body.Bytes(), &afterPage) != nil || len(afterPage.Items) != 2 {
		t.Fatalf("grouped analyzed item not visible: %d %s", after.Code, after.Body.String())
	}
	found := false
	for _, item := range afterPage.Items {
		if item.ID == strconv.FormatInt(analyzedID, 10) && item.StoryID != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("analyzed item was not published with a story")
	}
}

func TestGroupingFailureKeepsCompletedStepsAndHidesUnpublishedItem(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>OpenAI Atlas first report</title><link>https://example.test/items/first-story-failure</link><guid>story-failure-first</guid><pubDate>Wed, 21 Oct 2026 09:00:00 GMT</pubDate><description>%s</description></item>
<item><title>OpenAI Atlas second report</title><link>https://example.test/items/second-story-failure</link><guid>story-failure-second</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody, fixtureBody))
	}))
	defer feedServer.Close()
	var prefilters, scores, writings, groupings atomic.Int32
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
			prefilters.Add(1)
			writeFakeCompletion(w, `{"label":"PASS","reason":"相关"}`)
		case strings.Contains(prompt, "事件注意力评分器"):
			scores.Add(1)
			writeFakeCompletion(w, `{"attention_score":80}`)
		case strings.Contains(prompt, "事件归组"):
			groupings.Add(1)
			var input struct {
				Candidates []struct {
					StoryID int64 `json:"story_id"`
				} `json:"candidates"`
			}
			if err := json.Unmarshal([]byte(material), &input); err != nil {
				http.Error(w, "invalid grouping material", http.StatusBadRequest)
				return
			}
			if len(input.Candidates) == 0 {
				writeFakeCompletion(w, `{"relations":[]}`)
				return
			}
			writeFakeCompletion(w, `{"relations":[{"story_id":9223372036854775807,"relation":"SAME_OCCURRENCE","confidence":0.95}]}`)
		default:
			writings.Add(1)
			var item struct {
				Title string `json:"title"`
			}
			if err := json.Unmarshal([]byte(material), &item); err != nil {
				http.Error(w, "invalid writing material", http.StatusBadRequest)
				return
			}
			writeFakeCompletion(w, fmt.Sprintf(`{"title":%q,"summary":"OpenAI Atlas report with enough detail to distinguish the factual update and process it.","reason":"fixture","subjects":[],"fact":null,"roundup":false,"category":"模型","tags":["OpenAI"]}`, item.Title))
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
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "story-failure", Name: "Story failure fixture", Tier: "media", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err == nil {
		t.Fatal("processor succeeded despite an invalid relation from the grouping model")
	}
	if prefilters.Load() != 2 || scores.Load() != 4 || writings.Load() != 2 || groupings.Load() != 1 {
		t.Fatalf("first processing model calls prefilter:%d score:%d write:%d group:%d, want completed first item and failed second grouping", prefilters.Load(), scores.Load(), writings.Load(), groupings.Load())
	}
	all := httptest.NewRecorder()
	application.Router.ServeHTTP(all, httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil))
	var page struct {
		Items []struct {
			OriginalTitle string `json:"original_title"`
		} `json:"items"`
	}
	allBody := all.Body.Bytes()
	if all.Code != http.StatusOK || json.Unmarshal(allBody, &page) != nil || len(page.Items) != 1 || page.Items[0].OriginalTitle != "OpenAI Atlas second report" {
		t.Fatalf("unpublished grouping failure leaked into all-items response: %d %s", all.Code, allBody)
	}
	now = now.Add(2 * time.Minute)
	if err := application.Processor.RunOnce(t.Context()); err == nil {
		t.Fatal("processor succeeded on the retried invalid relation")
	}
	if prefilters.Load() != 2 || scores.Load() != 4 || writings.Load() != 2 || groupings.Load() != 2 {
		t.Fatalf("retry repeated completed steps: prefilter:%d score:%d write:%d group:%d", prefilters.Load(), scores.Load(), writings.Load(), groupings.Load())
	}
}

func TestProcessorRunsBothScoresAndWritingConcurrently(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>AI event for parallel model calls</title><link>https://example.test/items/parallel</link><guid>fixture-parallel</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody))
	}))
	defer feedServer.Close()

	started := make(chan string, 3)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var concurrent, maxConcurrent, requests atomic.Int32
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
		prompt := ""
		for _, message := range request.Messages {
			if message.Role == "system" {
				prompt = message.Content
				break
			}
		}
		switch {
		case strings.Contains(prompt, "相关性预筛"):
			requests.Add(1)
			writeFakeCompletion(w, `{"label":"PASS","reason":"AI 相关"}`)
		case strings.Contains(prompt, "事件注意力评分器"):
			requests.Add(1)
			gateConcurrentModelCall(t, "score", started, release, &concurrent, &maxConcurrent)
			writeFakeCompletion(w, `{"attention_score":80}`)
		case strings.Contains(prompt, "事件归组"):
			requests.Add(1)
			writeFakeCompletion(w, `{"relations":[]}`)
		default:
			requests.Add(1)
			gateConcurrentModelCall(t, "write", started, release, &concurrent, &maxConcurrent)
			writeFakeCompletion(w, `{"title":"并发写作标题","summary":"并发写作摘要","subjects":[],"fact":null,"roundup":false,"category":"论文","tags":[]}`)
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
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}

	processResult := make(chan error, 1)
	go func() { processResult <- application.Processor.RunOnce(t.Context()) }()
	seen := map[string]int{}
	for range 3 {
		select {
		case kind := <-started:
			seen[kind]++
		case <-time.After(5 * time.Second):
			releaseOnce.Do(func() { close(release) })
			t.Fatalf("only %d of the three parallel model calls started: %v", seen["score"]+seen["write"], seen)
		}
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-processResult; err != nil {
		t.Fatalf("process fixture item: %v", err)
	}
	if seen["score"] != 2 || seen["write"] != 1 {
		t.Fatalf("concurrent model calls = %v, want two scores and one writing call", seen)
	}
	if got := maxConcurrent.Load(); got < 3 {
		t.Fatalf("maximum concurrent model requests = %d, want at least 3", got)
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("total model requests = %d, want prefilter and three parallel calls", got)
	}
}

func gateConcurrentModelCall(t *testing.T, kind string, started chan<- string, release <-chan struct{}, concurrent, maximum *atomic.Int32) {
	t.Helper()
	active := concurrent.Add(1)
	for old := maximum.Load(); active > old && !maximum.CompareAndSwap(old, active); old = maximum.Load() {
	}
	started <- kind
	<-release
	concurrent.Add(-1)
}

func TestProcessorWritesConcurrentItemsOnceAndSkipsCompletedItems(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>First AI research release</title><link>https://example.test/items/first</link><guid>fixture-first</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
<item><title>Second AI research release</title><link>https://example.test/items/second</link><guid>fixture-second</guid><pubDate>Wed, 21 Oct 2026 11:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody, fixtureBody))
	}))
	defer feedServer.Close()

	var requests atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) <= 2 {
			started <- struct{}{}
			<-release
		}
		writeFakeModelResponseSameStory(w, r, `{"title":"并发生成的中文标题","summary":"生成的中文摘要","subjects":[],"fact":null,"roundup":false,"category":"论文","tags":["RAG"]}`)
	}))
	defer llmServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = llmServer.URL
	cfg.LLM.APIKey = "fake-test-key"
	cfg.LLM.Model = "fake-model"
	cfg.Worker.Count = 2
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture Feed", Tier: "official", FeedURL: feedServer.URL,
		Enabled: true, IntervalMinutes: 60,
	}}
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	testDB(t, application.Data).SetMaxOpenConns(3)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}

	processorResult := make(chan error, 1)
	go func() { processorResult <- application.Processor.RunOnce(t.Context()) }()
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			releaseOnce.Do(func() { close(release) })
			<-processorResult
			t.Fatal("two workers did not claim separate items concurrently")
		}
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-processorResult; err != nil {
		t.Fatalf("process collected items: %v", err)
	}
	if got := requests.Load(); got != 9 {
		t.Fatalf("model requests for two items = %d, want four analysis calls per item and one grouping call", got)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil)
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/items?view=all status = %d, want 200", response.Code)
	}
	var page struct {
		Items []struct {
			Title   string `json:"title"`
			StoryID string `json:"story_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode all-items response: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].Title != "并发生成的中文标题" || page.Items[1].Title != "并发生成的中文标题" {
		t.Fatalf("all-items titles after processing = %+v, want both Chinese writings", page.Items)
	}
	if page.Items[0].StoryID == "" || page.Items[0].StoryID != page.Items[1].StoryID {
		t.Fatalf("concurrent related items got distinct or missing stories: %+v", page.Items)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("rerun processor after completion: %v", err)
	}
	if got := requests.Load(); got != 9 {
		t.Fatalf("model requests after rerunning completed items = %d, want 9", got)
	}
}

func TestProcessorRetriesWritingFailureAndReportsFailedItem(t *testing.T) {
	testProcessorRetriesBadWriting(t, "{not valid JSON", "")
}

func TestProcessorRetriesWritingRefusalAndReportsFailedItem(t *testing.T) {
	testProcessorRetriesBadWriting(t, `{"title":"抱歉，我无法处理链接内容","summary":"无法访问链接","subjects":[],"fact":null,"roundup":false}`, "writing refused")
}

func testProcessorRetriesBadWriting(t *testing.T, output, wantError string) {
	t.Helper()
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>Failed AI research release</title><link>https://example.test/items/failed</link><guid>fixture-failed</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody))
	}))
	defer feedServer.Close()

	var requests, prefilters, scores, writings atomic.Int32
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch writeFakeModelResponse(w, r, output) {
		case "prefilter":
			prefilters.Add(1)
		case "score":
			scores.Add(1)
		case "write":
			writings.Add(1)
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
	cfg.Sources = []config.Source{{
		Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture Feed", Tier: "official", FeedURL: feedServer.URL,
		Enabled: true, IntervalMinutes: 60,
	}}
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

	backoff := []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute}
	for attempt := range 5 {
		if err := application.Processor.RunOnce(t.Context()); err == nil {
			t.Fatalf("processor attempt %d succeeded with invalid model JSON", attempt+1)
		}
		wantRequests := int32(4 + attempt)
		if got := requests.Load(); got != wantRequests {
			t.Fatalf("model requests after attempt %d = %d", attempt+1, got)
		}
		if prefilters.Load() != 1 || scores.Load() != 2 || writings.Load() != int32(attempt+1) {
			t.Fatalf("model calls after attempt %d = prefilter:%d score:%d write:%d, want successful steps retained and only writing retried", attempt+1, prefilters.Load(), scores.Load(), writings.Load())
		}
		if attempt == 4 {
			break
		}
		now = now.Add(backoff[attempt] - time.Minute)
		if err := application.Processor.RunOnce(t.Context()); err != nil {
			t.Fatalf("processor before retry deadline: %v", err)
		}
		if got := requests.Load(); got != wantRequests {
			t.Fatalf("model requests before %s backoff elapsed = %d, want %d", backoff[attempt], got, wantRequests)
		}
		now = now.Add(time.Minute)
	}
	if got := requests.Load(); got != 8 {
		t.Fatalf("model requests after failure limit = %d, want 8", got)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("processor after item failed: %v", err)
	}
	if got := requests.Load(); got != 8 {
		t.Fatalf("model requests after failed state = %d, want 8", got)
	}

	status, err := biz.NewOperationsUseCase(data.NewOperationsRepo(application.Data)).Status(t.Context(), 7)
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	statusView := status
	if len(statusView.FailedItems) != 1 || statusView.FailedItems[0].OriginalTitle != "Failed AI research release" || statusView.FailedItems[0].Attempts != 5 || statusView.FailedItems[0].LastError == "" {
		t.Fatalf("failed items status = %+v, want failed source item after five attempts", statusView.FailedItems)
	}

	if wantError != "" && statusView.FailedItems[0].LastError != wantError {
		t.Fatalf("status failure = %q, want %q", statusView.FailedItems[0].LastError, wantError)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil)
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/items?view=all status = %d, want 200", response.Code)
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode all-items response: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("all-items includes failed content: %s", page.Items[0])
	}
}

func TestProcessorAttemptTimeoutRecordsBackoffAndEventuallyFailsItem(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>Timeout AI research release</title><link>https://example.test/items/timeout</link><guid>fixture-timeout</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody))
	}))
	defer feedServer.Close()

	var requests, prefilters, scores, writings atomic.Int32
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
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
		prompt := ""
		for _, message := range request.Messages {
			if message.Role == "system" {
				prompt = message.Content
				break
			}
		}
		switch {
		case strings.Contains(prompt, "相关性预筛"):
			prefilters.Add(1)
			writeFakeCompletion(w, `{"label":"PASS","reason":"AI 相关"}`)
		case strings.Contains(prompt, "事件注意力评分器"):
			if scores.Add(1) == 1 {
				writeFakeCompletion(w, `{"attention_score":80}`)
				return
			}
			<-r.Context().Done()
		case strings.Contains(prompt, "AI 资料中文编辑"):
			writings.Add(1)
			writeFakeCompletion(w, `{"title":"超时重试中文标题","summary":"成功写作只调用一次。","reason":"保留已完成步骤。","subjects":[],"fact":null,"roundup":false,"category":"论文","tags":["RAG"]}`)
		default:
			http.Error(w, "unexpected fake model task", http.StatusBadRequest)
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
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	clock := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}
	options := biz.NewProcessorOptions(cfg)
	options.Enabled = true
	options.Worker.Count = 1
	options.Worker.AttemptTimeout = time.Second
	application.Processor = biz.NewProcessor(data.NewWorkRepo(application.Data), llm.New(cfg.LLM), options, now, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := application.Processor.RunOnce(t.Context()); err == nil {
		t.Fatal("first processing attempt succeeded despite a scoring request timing out")
	}
	if prefilters.Load() != 1 || scores.Load() != 2 || writings.Load() != 1 {
		t.Fatalf("model calls after first timeout = prefilter:%d score:%d write:%d", prefilters.Load(), scores.Load(), writings.Load())
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("run before recorded retry deadline: %v", err)
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("model requests before retry deadline = %d, want 4", got)
	}

	for attempt, delay := range []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute} {
		clock = clock.Add(delay)
		if err := application.Processor.RunOnce(t.Context()); err == nil {
			t.Fatalf("processing attempt %d succeeded despite the score timeout", attempt+2)
		}
		wantRequests := int32(5 + attempt)
		if got := requests.Load(); got != wantRequests {
			t.Fatalf("model requests after timeout attempt %d = %d, want %d", attempt+2, got, wantRequests)
		}
		if prefilters.Load() != 1 || scores.Load() != int32(attempt+3) || writings.Load() != 1 {
			t.Fatalf("model calls after timeout attempt %d = prefilter:%d score:%d write:%d, want only unfinished score retried", attempt+2, prefilters.Load(), scores.Load(), writings.Load())
		}
	}

	status, err := biz.NewOperationsUseCase(data.NewOperationsRepo(application.Data)).Status(t.Context(), 7)
	if err != nil {
		t.Fatalf("read status after five timed-out attempts: %v", err)
	}
	if len(status.FailedItems) != 1 || status.FailedItems[0].OriginalTitle != "Timeout AI research release" || status.FailedItems[0].Attempts != 5 || status.FailedItems[0].LastError == "" {
		t.Fatalf("failed items after timed-out attempts = %+v, want the item failed after five attempts", status.FailedItems)
	}
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/items?view=all returned %d: %s", response.Code, response.Body.String())
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode all-items response: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("all-items contains timed-out item: %s", page.Items[0])
	}
}

func TestAllItemsCursorPagesTiedPublicationTimesWithoutGaps(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>Article one</title><link>https://example.test/items/one</link><guid>fixture-one</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
<item><title>Article two</title><link>https://example.test/items/two</link><guid>fixture-two</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
<item><title>Article three</title><link>https://example.test/items/three</link><guid>fixture-three</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody, fixtureBody, fixtureBody))
	}))
	defer feedServer.Close()
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFakeModelResponse(w, r, `{"title":"分页中文标题","summary":"分页中文摘要","subjects":[],"fact":null,"roundup":false,"category":"论文","tags":["RAG"]}`)
	}))
	defer llmServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = llmServer.URL
	cfg.LLM.APIKey = "fake-test-key"
	cfg.LLM.Model = "fake-model"
	cfg.Worker.Count = 2
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("write fixture items: %v", err)
	}

	ids := make(map[string]bool)
	cursor := ""
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		path := "/api/items?view=all&limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		response := httptest.NewRecorder()
		application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s returned %d: %s", path, response.Code, response.Body.String())
		}
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			NextCursor string `json:"next_cursor"`
		}
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatalf("decode page %d: %v", pageNumber+1, err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("page %d contains %d items, want 1", pageNumber+1, len(page.Items))
		}
		if ids[page.Items[0].ID] {
			t.Fatalf("item %q repeated across cursor pages", page.Items[0].ID)
		}
		ids[page.Items[0].ID] = true
		if pageNumber < 2 && page.NextCursor == "" {
			t.Fatalf("page %d omitted its next cursor", pageNumber+1)
		}
		if pageNumber == 2 && page.NextCursor != "" {
			t.Fatalf("last page unexpectedly has cursor %q", page.NextCursor)
		}
		cursor = page.NextCursor
	}
	if len(ids) != 3 {
		t.Fatalf("unique cursor results = %d, want all 3 tied-time items", len(ids))
	}

	for _, target := range []string{
		"/api/items?view=all&limit=0",
		"/api/items?view=all&limit=101",
		"/api/items?view=all&cursor=not-a-cursor",
	} {
		response := httptest.NewRecorder()
		application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/problem+json") {
			t.Errorf("GET %s returned %d with content type %q, want problem+json 400", target, response.Code, response.Header().Get("Content-Type"))
		}
	}
}

func TestAllFeedIsValidRSSWithEscapedChineseWriting(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>Original title</title><link>https://example.test/items/one</link><guid>fixture-one</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody))
	}))
	defer feedServer.Close()
	title := "中文 & AI <新> 发布"
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFakeModelResponse(w, r, `{"title":"`+title+`","summary":"中文摘要","subjects":[],"fact":null,"roundup":false,"category":"论文","tags":["RAG"]}`)
	}))
	defer llmServer.Close()

	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = llmServer.URL
	cfg.LLM.APIKey = "fake-test-key"
	cfg.LLM.Model = "fake-model"
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	now := func() time.Time { return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC) }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("write fixture item: %v", err)
	}

	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/feed/all.xml", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /feed/all.xml returned %d: %s", response.Code, response.Body.String())
	}
	var rss struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel struct {
			Language string `xml:"language"`
			Items    []struct {
				Title   string `xml:"title"`
				Link    string `xml:"link"`
				PubDate string `xml:"pubDate"`
				GUID    struct {
					Value       string `xml:",chardata"`
					IsPermaLink string `xml:"isPermaLink,attr"`
				} `xml:"guid"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(response.Body.Bytes(), &rss); err != nil {
		t.Fatalf("decode RSS XML: %v", err)
	}
	if rss.XMLName.Local != "rss" || rss.Version != "2.0" || len(rss.Channel.Items) != 1 {
		t.Fatalf("RSS document root/version/items = %s/%q/%d, want rss/2.0/1", rss.XMLName.Local, rss.Version, len(rss.Channel.Items))
	}
	if rss.Channel.Items[0].Title != title || rss.Channel.Items[0].Link != "https://example.test/items/one" {
		t.Fatalf("RSS item = %+v, want escaped Chinese title and source link", rss.Channel.Items[0])
	}
	if rss.Channel.Language != "zh-CN" {
		t.Errorf("RSS language = %q, want zh-CN", rss.Channel.Language)
	}
	allItemsResponse := requestFoldItems(t, application.Router, "/api/items?view=all")
	var allItems struct {
		Items []service.Item `json:"items"`
	}
	if err := json.Unmarshal(allItemsResponse, &allItems); err != nil {
		t.Fatalf("decode public all-items response: %v", err)
	}
	var itemID string
	for _, item := range allItems.Items {
		if item.URL == "https://example.test/items/one" {
			itemID = item.ID
			break
		}
	}
	if itemID == "" {
		t.Fatalf("public all-items response omitted fixture item: %s", allItemsResponse)
	}
	wantGUID := strings.TrimRight(cfg.Site.PublicURL, "/") + "/items/" + itemID
	if got := rss.Channel.Items[0].GUID; got.Value != wantGUID || got.IsPermaLink != "false" {
		t.Errorf("RSS GUID = %+v, want value %q with isPermaLink=false", got, wantGUID)
	}
	gotPubDate, err := time.Parse(time.RFC1123Z, rss.Channel.Items[0].PubDate)
	if err != nil {
		t.Fatalf("parse RSS pubDate %q: %v", rss.Channel.Items[0].PubDate, err)
	}
	if want := time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC); !gotPubDate.Equal(want) {
		t.Errorf("RSS pubDate = %s, want instant %s", gotPubDate, want)
	}
}

func TestRunnerStartsWriterAndCancellationReleasesClaimedItem(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	feedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Fixture</title><link>https://example.test/</link><description>fixture feed</description>
<item><title>Cancelable AI research</title><link>https://example.test/items/cancelable</link><guid>fixture-cancelable</guid><pubDate>Wed, 21 Oct 2026 10:00:00 GMT</pubDate><description>%s</description></item>
</channel></rss>`, fixtureBody))
	}))
	defer feedServer.Close()

	var prefilters, scores, writings, groupings atomic.Int32
	blockedScore := make(chan struct{}, 1)
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
		prompt := ""
		for _, message := range request.Messages {
			if message.Role == "system" {
				prompt = message.Content
				break
			}
		}
		switch {
		case strings.Contains(prompt, "相关性预筛"):
			prefilters.Add(1)
			writeFakeCompletion(w, `{"label":"PASS","reason":"AI 相关"}`)
		case strings.Contains(prompt, "事件注意力评分器"):
			switch scores.Add(1) {
			case 1:
				writeFakeCompletion(w, `{"attention_score":80}`)
			case 2:
				writeFakeCompletion(w, "{invalid score JSON")
			case 3:
				blockedScore <- struct{}{}
				<-r.Context().Done()
			default:
				writeFakeCompletion(w, `{"attention_score":80}`)
			}
		case strings.Contains(prompt, "事件归组"):
			groupings.Add(1)
			writeFakeCompletion(w, `{"relations":[]}`)
		default:
			writings.Add(1)
			writeFakeCompletion(w, `{"title":"取消后重新处理成功","summary":"锁已释放并完成写作。","subjects":[],"fact":null,"roundup":false,"category":"论文","tags":["RAG"]}`)
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
	cfg.Server.Address = "127.0.0.1:0"
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: feedServer.URL, Enabled: true, IntervalMinutes: 60}}
	clock := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, logger, now, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)

	if err := application.Collector.RunOnce(t.Context()); err != nil {
		t.Fatalf("collect fixture feed: %v", err)
	}
	if err := application.Processor.RunOnce(t.Context()); err == nil {
		t.Fatal("first processing attempt succeeded despite invalid JSON from one scoring step")
	}
	if prefilters.Load() != 1 || scores.Load() != 2 || writings.Load() != 1 {
		t.Fatalf("calls after partial failure = prefilter:%d score:%d write:%d", prefilters.Load(), scores.Load(), writings.Load())
	}
	clock = clock.Add(2 * time.Minute)
	runContext, cancelRun := context.WithCancel(t.Context())
	runResult := make(chan struct{})
	go func() { application.Jobs.Run(runContext); close(runResult) }()
	select {
	case <-blockedScore:
	case <-time.After(5 * time.Second):
		cancelRun()
		<-runResult
		t.Fatal("Runner.Run did not start the retry for the failed score step")
	}
	cancelRun()
	<-runResult

	if err := application.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("process item after canceled worker released its claim: %v", err)
	}
	if got := prefilters.Load(); got != 1 {
		t.Fatalf("prefilter requests after cancellation recovery = %d, want retained successful prefilter", got)
	}
	if got := scores.Load(); got != 4 {
		t.Fatalf("score requests after cancellation recovery = %d, want two initial, canceled retry and final retry", got)
	}
	if got := writings.Load(); got != 1 {
		t.Fatalf("writing requests after cancellation recovery = %d, want one", got)
	}
	if got := groupings.Load(); got != 0 {
		t.Fatalf("grouping requests after cancellation recovery = %d, want zero", got)
	}
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "取消后重新处理成功") {
		t.Fatalf("completed item after cancellation recovery returned %d: %s", response.Code, response.Body.String())
	}
}

func writeFakeModelResponse(w http.ResponseWriter, r *http.Request, writing string) string {
	return writeFakeModelResponseMode(w, r, writing, false)
}

func writeFakeModelResponseSameStory(w http.ResponseWriter, r *http.Request, writing string) string {
	return writeFakeModelResponseMode(w, r, writing, true)
}

func writeFakeModelResponseMode(w http.ResponseWriter, r *http.Request, writing string, groupSameStory bool) string {
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid fake model request", http.StatusBadRequest)
		return "invalid"
	}
	var prompt string
	for _, message := range request.Messages {
		if message.Role == "system" {
			prompt = message.Content
			break
		}
	}
	content := writing
	kind := "write"
	switch {
	case strings.Contains(prompt, "相关性预筛"):
		kind = "prefilter"
		content = `{"label":"PASS","reason":"测试继续处理"}`
	case strings.Contains(prompt, "事件注意力评分器"):
		kind = "score"
		content = `{"attention_score":80}`
	case strings.Contains(prompt, "事件归组"):
		kind = "group"
		relation := "UNRELATED"
		if groupSameStory {
			relation = "SAME_OCCURRENCE"
		}
		var material string
		for _, message := range request.Messages {
			if message.Role == "user" {
				material = message.Content
				break
			}
		}
		var err error
		content, err = fakeStoryRelations(material, relation)
		if err != nil {
			http.Error(w, "invalid grouping material", http.StatusBadRequest)
			return "invalid"
		}
	}
	writeFakeCompletion(w, content)
	return kind
}

func writeFilterFakeModelResponse(w http.ResponseWriter, r *http.Request) {
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
		switch message.Role {
		case "system":
			prompt = message.Content
		case "user":
			material = message.Content
		}
	}
	var item struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(material), &item); err != nil {
		http.Error(w, "invalid fake model material", http.StatusBadRequest)
		return
	}
	switch {
	case strings.Contains(prompt, "相关性预筛"):
		writeFakeCompletion(w, `{"label":"PASS","reason":"AI 相关"}`)
	case strings.Contains(prompt, "事件注意力评分器"):
		if strings.Contains(item.Title, "selected") {
			writeFakeCompletion(w, `{"attention_score":80}`)
		} else {
			writeFakeCompletion(w, `{"attention_score":50}`)
		}
	case strings.Contains(prompt, "事件归组"):
		relations, err := fakeStoryRelations(material, "UNRELATED")
		if err != nil {
			http.Error(w, "invalid grouping material", http.StatusBadRequest)
			return
		}
		writeFakeCompletion(w, relations)
	case strings.Contains(item.Title, "selected"):
		writeFakeCompletion(w, `{"title":"精选 AI 100%_","summary":"搜索 100%_ 摘要","reason":"精选内容","subjects":[],"fact":null,"roundup":false,"category":"论文","tags":["RAG"]}`)
	default:
		writeFakeCompletion(w, `{"title":"普通 AI 100Xa","summary":"搜索 100Xa 摘要","reason":"普通内容","subjects":[],"fact":null,"roundup":false,"category":"行业","tags":[]}`)
	}
}

func fakeStoryRelations(material, relation string) (string, error) {
	var input struct {
		Candidates []struct {
			StoryID int64 `json:"story_id"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(material), &input); err != nil {
		return "", err
	}
	relations := make([]map[string]any, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		relations = append(relations, map[string]any{
			"story_id": candidate.StoryID, "relation": relation, "confidence": 0.95,
		})
	}
	content, err := json.Marshal(map[string]any{"relations": relations})
	return string(content), err
}

func writeFakeCompletion(w http.ResponseWriter, content string) {
	response, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	if err != nil {
		http.Error(w, "encode fake completion", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(response)
}
