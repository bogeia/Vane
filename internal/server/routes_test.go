package server

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/biz/taxonomy"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/service"
	agentservice "github.com/bogeia/vane/internal/service/agent"
)

func TestNewRouterHTTPBehavior(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := testRouter(logger, "")
	router.GET("/panic", func(*echo.Context) error { panic("private panic detail") })
	router.GET("/error", func(*echo.Context) error { return errors.New("private error detail") })
	router.GET("/committed", func(c *echo.Context) error {
		if err := c.String(http.StatusOK, "already written"); err != nil {
			return err
		}
		return errors.New("error after response")
	})
	router.GET("/method-check", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })

	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/docs", http.StatusOK},
		{"GET", "/openapi.json", http.StatusOK},
		{"GET", "/openapi.yaml", http.StatusOK},
		{"GET", "/healthz", http.StatusOK},
		{"GET", "/users", http.StatusNotFound},
		{"GET", "/missing", http.StatusNotFound},
		{"PATCH", "/method-check", http.StatusMethodNotAllowed},
		{"HEAD", "/missing", http.StatusNotFound},
		{"GET", "/panic", http.StatusInternalServerError},
		{"GET", "/error", http.StatusInternalServerError},
		{"GET", "/committed", http.StatusOK},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			logStart := logs.Len()
			request := httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status %d, want %d: %s", response.Code, test.status, response.Body)
			}
			if test.status >= 400 {
				if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/problem+json") {
					t.Errorf("unexpected error content type: %s", contentType)
				}
				if test.method != http.MethodHead {
					var problem huma.ErrorModel
					if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil || problem.Status != test.status || problem.Title == "" {
						t.Errorf("invalid problem response: %s", response.Body)
					}
				}
				if strings.Contains(response.Body.String(), "private") {
					t.Error("internal error details leaked to client")
				}
			}
			if test.method == http.MethodHead && response.Body.Len() != 0 {
				t.Error("HEAD response contains a body")
			}
			if test.status == http.StatusMethodNotAllowed && response.Header().Get("Allow") == "" {
				t.Error("405 response lost Allow header")
			}
			if test.path == "/committed" && response.Body.String() != "already written" {
				t.Errorf("error handler rewrote committed response: %s", response.Body)
			}
			accessLogs := 0
			for line := range strings.SplitSeq(strings.TrimSpace(logs.String()[logStart:]), "\n") {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Errorf("invalid log: %v", err)
					continue
				}
				if record["msg"] != "http request" {
					continue
				}
				accessLogs++
				if record["method"] != test.method || record["path"] != test.path || record["status"] != float64(test.status) || record["latency"] == nil {
					t.Errorf("invalid access log: %s", line)
				}
				if test.status == http.StatusInternalServerError && (record["level"] != "ERROR" || record["error"] == nil) {
					t.Error("server error missing error log detail")
				}
				if test.path == "/panic" {
					detail, _ := record["error"].(string)
					if !strings.Contains(detail, "private panic detail") {
						t.Error("recovery lost panic detail in server log")
					}
				}
			}
			if accessLogs != 1 {
				t.Errorf("expected one access log, got %d", accessLogs)
			}
		})
	}
	if bytes.Contains(logs.Bytes(), []byte(`"msg":"request args"`)) {
		t.Fatalf("request input must not be logged: %s", &logs)
	}
}

func TestHealthFailureDoesNotLeakDetails(t *testing.T) {
	const privateDetail = "private database connection detail"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	operations := service.NewOperationsService(biz.NewOperationsUseCase(fakePing{err: errors.New(privateDetail)}), logger)
	items := service.NewItemsService(biz.NewItemsUseCase(fakeItems{}, time.Now), logger)
	stories := service.NewStoriesService(biz.NewStoriesUseCase(fakeStories{}), logger)
	hot := service.NewHotService(biz.NewHotUseCase(fakeHotRepository{}, time.Now), logger)
	reports := service.NewReportsService(biz.NewReportsUseCase(fakeReportsRepository{}, time.Now, nil), logger)
	cfg := config.Config{Site: config.Site{Name: "Vane", PublicURL: "https://vane.test"}}
	agent, mcpServer := testAgentServices(logger, cfg.Site, fakeItems{})
	router := NewRouter(logger, operations, cfg, items, stories, hot, reports, service.NewSourcesService(biz.NewSourcesUseCase(nil, nil, time.Now), nil), agent, mcpServer)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /healthz returned %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if strings.Contains(response.Body.String(), privateDetail) {
		t.Fatalf("health response leaked private error detail: %s", response.Body)
	}
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("unexpected health error content type: %s", response.Header().Get("Content-Type"))
	}
}

func TestMetaReturnsSiteNameAndCategories(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	operations := service.NewOperationsService(biz.NewOperationsUseCase(fakePing{}), logger)
	items := service.NewItemsService(biz.NewItemsUseCase(fakeItems{}, time.Now), logger)
	stories := service.NewStoriesService(biz.NewStoriesUseCase(fakeStories{}), logger)
	hot := service.NewHotService(biz.NewHotUseCase(fakeHotRepository{}, time.Now), logger)
	reports := service.NewReportsService(biz.NewReportsUseCase(fakeReportsRepository{}, time.Now, nil), logger)
	cfg := config.Config{Site: config.Site{Name: "Vane", PublicURL: "https://vane.test"}}
	agent, mcpServer := testAgentServices(logger, cfg.Site, fakeItems{})
	router := NewRouter(logger, operations, cfg, items, stories, hot, reports, service.NewSourcesService(biz.NewSourcesUseCase(nil, nil, time.Now), nil), agent, mcpServer)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/meta", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/meta returned %d, want 200: %s", response.Code, response.Body)
	}
	var meta struct {
		Name       string   `json:"name"`
		Categories []string `json:"categories"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode /api/meta: %v", err)
	}
	if meta.Name != "Vane" || !slices.Equal(meta.Categories, biz.Categories) {
		t.Fatalf("/api/meta = %+v, want site name and configured categories", meta)
	}
}

func TestOpenAPIContainsExplicitOperationIDs(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), "")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.json", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json returned %d", response.Code)
	}
	var document struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode OpenAPI document: %v", err)
	}
	for _, operation := range []struct{ path, id string }{
		{"/api/meta", "site-meta"},
		{"/api/items", "list-items"},
		{"/api/items/{id}", "get-item"},
		{"/api/stories/{id}", "get-story"},
		{"/api/hot", "hot-ranking"},
		{"/api/reports", "list-reports"},
		{"/api/reports/{date}", "get-report"},
		{"/healthz", "health"},
	} {
		get, ok := document.Paths[operation.path]["get"]
		if !ok || get.OperationID != operation.id {
			t.Errorf("GET %s operationId = %q, present=%v; want %q", operation.path, get.OperationID, ok, operation.id)
		}
	}
}

func TestFeedRoutesReturnDistinctRSSAndCacheSuccessfulResponses(t *testing.T) {
	items := []biz.Item{
		{ID: "41", StoryID: "7", Title: "精选 & 中文", URL: "https://news.test/official", Summary: "官方摘要", PublishedAt: time.Date(2026, time.October, 21, 9, 0, 0, 0, time.UTC), Selected: false},
		{ID: "42", StoryID: "7", Title: "媒体报道", URL: "https://news.test/media", Summary: "媒体摘要", PublishedAt: time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC), Selected: true},
	}
	reports := feedReportsRepository{
		summaries: []biz.ReportSummary{{Date: "2026-10-21", Headline: "精选 & 中文"}},
		reports:   map[string]biz.Report{"2026-10-21": {Date: "2026-10-21", Headline: &items[0], Highlights: []biz.Item{items[1]}, Sections: []biz.ReportSection{}, Briefs: []biz.Item{}}},
	}
	router := feedTestRouter(feedItemsRepository{all: items, selected: items[:1]}, reports)

	for _, test := range []struct {
		path, wantTitle, wantGUID string
		items                     int
	}{
		{"/feed.xml", "精选 & 中文", "https://vane.test/stories/7", 1},
		{"/feed/all.xml", "精选 & 中文", "https://vane.test/items/41", 2},
		{"/feed/daily.xml", "Vane 日报 · 2026-10-21", "https://vane.test/reports/2026-10-21", 1},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s returned %d: %s", test.path, response.Code, response.Body)
			}
			if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/rss+xml") {
				t.Fatalf("GET %s Content-Type = %q", test.path, got)
			}
			if got := response.Header().Get("Cache-Control"); got != "public, max-age=60" {
				t.Fatalf("GET %s Cache-Control = %q", test.path, got)
			}
			var document struct {
				Version string `xml:"version,attr"`
				Channel struct {
					Items []struct {
						Title string `xml:"title"`
						GUID  struct {
							Value string `xml:",chardata"`
						} `xml:"guid"`
					} `xml:"item"`
				} `xml:"channel"`
			}
			if err := xml.Unmarshal(response.Body.Bytes(), &document); err != nil {
				t.Fatalf("decode RSS XML: %v", err)
			}
			if document.Version != "2.0" || len(document.Channel.Items) != test.items {
				t.Fatalf("RSS version/count = %q/%d, want 2.0/%d", document.Version, len(document.Channel.Items), test.items)
			}
			if document.Channel.Items[0].Title != test.wantTitle || document.Channel.Items[0].GUID.Value != test.wantGUID {
				t.Fatalf("RSS first item = %+v, want title=%q GUID=%q", document.Channel.Items[0], test.wantTitle, test.wantGUID)
			}
		})
	}
}

func TestFeedRoutesHideBackendErrorsAndDoNotCacheFailures(t *testing.T) {
	const privateError = "private repository detail"
	itemErrorRouter := feedTestRouter(feedItemsRepository{err: errors.New(privateError)}, feedReportsRepository{})
	for _, path := range []string{"/feed.xml", "/feed/all.xml"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		itemErrorRouter.ServeHTTP(response, request)
		if response.Code != http.StatusInternalServerError {
			t.Errorf("GET %s status = %d, want 500", path, response.Code)
		}
		if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/problem+json") {
			t.Errorf("GET %s error content type = %q", path, response.Header().Get("Content-Type"))
		}
		if strings.Contains(response.Body.String(), privateError) || response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("GET %s leaked backend details or cacheable error: %s", path, response.Body)
		}
	}

	reportErrorRouter := feedTestRouter(feedItemsRepository{}, feedReportsRepository{listErr: errors.New(privateError)})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/feed/daily.xml", nil)
	response := httptest.NewRecorder()
	reportErrorRouter.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), privateError) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET /feed/daily.xml exposed backend failure: status=%d body=%s", response.Code, response.Body)
	}
}

func TestAgentRoutesUseMarkdownCachePolicyAndHeadSemantics(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	for _, path := range []string{"/api/agent", "/api/agent/latest"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+" "+path, func(t *testing.T) {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), method, path, nil))
				require.Equal(t, http.StatusOK, response.Code)
				assert.Equal(t, "text/markdown; charset=utf-8", response.Header().Get("Content-Type"))
				assert.Equal(t, "public, max-age=60", response.Header().Get("Cache-Control"))
				if method == http.MethodHead {
					assert.Empty(t, response.Body.String())
				}
			})
		}
	}
}

func TestAgentParameterAndUnknownRoutesReturnUncachedMarkdown(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	for _, tc := range []struct {
		path   string
		status int
		phrase string
	}{
		{"/api/agent/latest?window=wrong", http.StatusBadRequest, "参数有误"},
		{"/api/agent/unknown", http.StatusNotFound, "没有这个地址"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))
			require.Equal(t, tc.status, response.Code)
			assert.Equal(t, "text/markdown; charset=utf-8", response.Header().Get("Content-Type"))
			assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			assert.Contains(t, response.Body.String(), tc.phrase)
		})
	}
}

func TestAgentQueryParserUsesFirstValueAndIgnoresUnknownParameters(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	for _, tc := range []struct {
		path   string
		status int
		cache  string
		phrase string
	}{
		{"/api/agent/latest?window=24h&window=wrong&ignored=x", http.StatusOK, "public, max-age=60", "过去 24 小时"},
		{"/api/agent/latest?limit=nope", http.StatusBadRequest, "no-store", "limit 只能是 1–30"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))
			require.Equal(t, tc.status, response.Code)
			assert.Equal(t, tc.cache, response.Header().Get("Cache-Control"))
			assert.Contains(t, response.Body.String(), tc.phrase)
		})
	}
}

func TestAgentHotAndStoryRoutesReturnMarkdown(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	for _, tc := range []struct {
		path   string
		status int
		phrase string
	}{
		{"/api/agent/hot", http.StatusOK, "# Vane 当前热点 Top 0"},
		{"/api/agent/stories/42", http.StatusNotFound, "# Vane 事件"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))
			require.Equal(t, tc.status, response.Code)
			assert.Equal(t, "text/markdown; charset=utf-8", response.Header().Get("Content-Type"))
			assert.Contains(t, response.Body.String(), tc.phrase)
		})
	}
}

func TestAgentSearchRouteUsesMarkdownCacheAndHeadSemantics(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), method, "/api/agent/search?q=OpenAI", nil))
			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "text/markdown; charset=utf-8", response.Header().Get("Content-Type"))
			assert.Equal(t, "public, max-age=60", response.Header().Get("Cache-Control"))
			if method == http.MethodHead {
				assert.Empty(t, response.Body.String())
			} else {
				assert.Contains(t, response.Body.String(), "# Vane 搜索「OpenAI」 · 最近 7 天")
				assert.Contains(t, response.Body.String(), "最近 7 天的精选和全部动态里都没有相关报道。")
			}
		})
	}
}

func TestAgentSearchRouteRejectsMissingAndForbiddenParameters(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	for _, tc := range []struct{ path, phrase string }{
		{"/api/agent/search", "q 必须是 1–200 个字符"},
		{"/api/agent/search?q=OpenAI&topic=agent", "搜索不接受 topic 或 mode 参数"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))
			require.Equal(t, http.StatusBadRequest, response.Code)
			assert.Equal(t, "text/markdown; charset=utf-8", response.Header().Get("Content-Type"))
			assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			assert.Contains(t, response.Body.String(), tc.phrase)
		})
	}
}

func TestAgentDailyRoutesServeLatestAndDateWithCacheAndHead(t *testing.T) {
	date := "2026-10-26"
	report := biz.Report{Date: date, Headline: &biz.Item{ID: "1", Title: "日报头条", Source: biz.ItemSource{Name: "信源"}}}
	router := testRouterWithDailyReport(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), report)
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/agent/daily"},
		{http.MethodHead, "/api/agent/daily"},
		{http.MethodGet, "/api/agent/daily/2026-10-26"},
		{http.MethodHead, "/api/agent/daily/2026-10-26"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil))
			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "text/markdown; charset=utf-8", response.Header().Get("Content-Type"))
			assert.Equal(t, "public, max-age=60", response.Header().Get("Cache-Control"))
			if tc.method == http.MethodHead {
				assert.Empty(t, response.Body.String())
			} else {
				assert.Contains(t, response.Body.String(), "# Vane 日报 · 2026-10-26（星期一）")
				assert.Contains(t, response.Body.String(), "日报头条")
			}
		})
	}
}

type agentDailyReportRepoFake struct{ report biz.Report }

func (agentDailyReportRepoFake) MarkReportPushed(context.Context, string, time.Time) error {
	return nil
}

func (agentDailyReportRepoFake) GenerateReport(context.Context, biz.ReportRequest, func([]biz.ReportCandidate, []biz.Report) (biz.Report, error)) (biz.Report, error) {
	return biz.Report{}, nil
}

func (f agentDailyReportRepoFake) Report(context.Context, string) (biz.Report, error) {
	return f.report, nil
}

func (agentDailyReportRepoFake) ListReports(context.Context, int) ([]biz.ReportSummary, error) {
	return []biz.ReportSummary{{Date: "2026-10-26"}}, nil
}

func testRouterWithDailyReport(logger *slog.Logger, webDir string, report biz.Report) *echo.Echo {
	operations := service.NewOperationsService(biz.NewOperationsUseCase(fakePing{}), logger)
	itemUseCase := biz.NewItemsUseCase(fakeItems{}, time.Now)
	items := service.NewItemsService(itemUseCase, logger)
	storyUseCase := biz.NewStoriesUseCase(fakeStories{})
	stories := service.NewStoriesService(storyUseCase, logger)
	hotUseCase := biz.NewHotUseCase(fakeHotRepository{}, time.Now)
	hot := service.NewHotService(hotUseCase, logger)
	configValue := config.Config{Site: config.Site{Name: "Vane", PublicURL: "https://vane.test"}, Web: config.Web{Dir: webDir}}
	reportsUseCase := biz.NewReportsUseCase(agentDailyReportRepoFake{report: report}, time.Now, nil)
	reports := service.NewReportsService(reportsUseCase, logger)
	agent := agentservice.NewAgentService(itemUseCase, hotUseCase, storyUseCase, reportsUseCase, configValue.Site, time.Now, logger)
	mcpServer := agentservice.NewMCPServer(agent)
	return NewRouter(logger, operations, configValue, items, stories, hot, reports, service.NewSourcesService(biz.NewSourcesUseCase(biz.NewSources(configValue), nil, time.Now), nil), agent, mcpServer)
}

func TestAgentSearchRouteUsesFirstRepeatedValuesAndRejectsEmptyForbiddenParameters(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/agent/search?q=OpenAI&q=ignored&window=24h&window=wrong&ignored=x", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "# Vane 搜索「OpenAI」 · 过去 24 小时")
	assert.NotContains(t, response.Body.String(), "ignored")
	for _, parameter := range []string{"topic=", "mode="} {
		t.Run(parameter, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/agent/search?q=OpenAI&"+parameter, nil))
			require.Equal(t, http.StatusBadRequest, response.Code)
			assert.Contains(t, response.Body.String(), "搜索不接受 topic 或 mode 参数")
		})
	}
}

func TestMCPRejectsBrowserCrossOriginOversizedAndNonPostRequests(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	for _, tc := range []struct {
		name   string
		method string
		body   string
		origin string
		fetch  string
		status int
	}{
		{"cross origin", http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, "https://attacker.example", "cross-site", http.StatusForbidden},
		{"oversized body", http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"vane_get_latest","arguments":{"padding":"` + strings.Repeat("x", (64<<10)+1) + `"}}}`, "", "", http.StatusRequestEntityTooLarge},
		{"get", http.MethodGet, "", "", "", http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), tc.method, "/api/mcp", strings.NewReader(tc.body))
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
				request.Header.Set("Sec-Fetch-Site", tc.fetch)
			}
			if tc.method == http.MethodPost {
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Accept", "application/json, text/event-stream")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, tc.status, response.Code, "%s response body=%q", tc.name, response.Body.String())
		})
	}
}

type blockingAgentItemsRepository struct {
	biz.ItemsRepository
	entered   chan struct{}
	cancelled chan struct{}
}

func (r blockingAgentItemsRepository) ListItems(ctx context.Context, _ *biz.Cursor, _ int, _ biz.ItemFilter) ([]biz.Item, error) {
	close(r.entered)
	<-ctx.Done()
	close(r.cancelled)
	return nil, ctx.Err()
}

func TestMCPRequestCancellationReachesItemsUseCase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	repository := blockingAgentItemsRepository{entered: make(chan struct{}), cancelled: make(chan struct{})}
	router := testRouterWithAgentItems(logger, t.TempDir(), repository)
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/api/mcp"}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	callDone := make(chan error, 1)
	go func() {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "vane_get_latest", Arguments: map[string]any{}})
		callDone <- err
	}()
	require.Eventually(t, func() bool {
		select {
		case <-repository.entered:
			return true
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond, "MCP call did not reach the repository")
	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-repository.cancelled:
			return true
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond, "request cancellation did not reach the repository")
	require.Eventually(t, func() bool {
		select {
		case <-callDone:
			return true
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond, "MCP call did not return after cancellation")
}

func TestNewRouterServesDeepLinksAndKeepsAPIErrors(t *testing.T) {
	webDir := t.TempDir()
	const page = "Vane frontend shell"
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	const asset = "Vane frontend asset"
	if err := os.Mkdir(filepath.Join(webDir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte(asset), 0o600); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := testRouter(logger, webDir)

	for _, test := range []struct {
		method, path string
		status       int
		body         string
	}{
		{http.MethodGet, "/items/1", http.StatusOK, page},
		{http.MethodGet, "/topics", http.StatusOK, page},
		{http.MethodGet, "/topics/agent", http.StatusOK, page},
		{http.MethodHead, "/topics/agent", http.StatusOK, ""},
		{http.MethodGet, "/favorites", http.StatusOK, page},
		{http.MethodHead, "/favorites", http.StatusOK, ""},
		{http.MethodGet, "/about", http.StatusOK, page},
		{http.MethodHead, "/about", http.StatusOK, ""},
		{http.MethodGet, "/changelog", http.StatusOK, page},
		{http.MethodHead, "/changelog", http.StatusOK, ""},
		{http.MethodGet, "/more", http.StatusOK, page},
		{http.MethodHead, "/more", http.StatusOK, ""},
		{http.MethodGet, "/assets/app.js", http.StatusOK, asset},
		{http.MethodHead, "/items/1", http.StatusOK, ""},
	} {
		request := httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != test.status || response.Body.String() != test.body {
			t.Fatalf("%s %s returned %d with body %q", test.method, test.path, response.Code, response.Body.String())
		}
	}

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/不存在", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("GET /api/不存在 returned %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	var problem huma.ErrorModel
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil || problem.Status != http.StatusNotFound {
		t.Fatalf("invalid API error response: %s", response.Body)
	}

	request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/assets/app.js", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/problem+json") || response.Body.String() == page {
		t.Fatalf("GET /api/assets/app.js returned %d with body %q", response.Code, response.Body.String())
	}
}

func TestNewRouterServesManifestAndIconsAsStaticFiles(t *testing.T) {
	webDir := t.TempDir()
	const manifest = `{"name":"Vane"}`
	icon := []byte("\x89PNG\r\n\x1a\nicon")
	for name, body := range map[string][]byte{"index.html": []byte("Vane frontend shell"), "manifest.json": []byte(manifest), "icon-192.png": icon} {
		if err := os.WriteFile(filepath.Join(webDir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), webDir)
	for _, test := range []struct {
		method, path, contentType, body string
	}{
		{http.MethodGet, "/manifest.json", "application/json", manifest},
		{http.MethodHead, "/manifest.json", "application/json", ""},
		{http.MethodGet, "/icon-192.png", "image/png", string(icon)},
		{http.MethodHead, "/icon-192.png", "image/png", ""},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil))
		if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), test.contentType) || response.Body.String() != test.body {
			t.Fatalf("%s %s returned %d %q with body %q", test.method, test.path, response.Code, response.Header().Get("Content-Type"), response.Body.String())
		}
	}
}

func TestNewRouterReturnsNotFoundWhenIndexIsMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := testRouter(logger, t.TempDir())
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/items/1", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("GET /items/1 returned %d, want 404", response.Code)
	}
}

type fakePing struct{ err error }

func (f fakePing) Ping(context.Context) error { return f.err }
func (fakePing) Status(context.Context, biz.StatusQuery) (biz.Status, error) {
	return biz.Status{Sources: []biz.SourceStatus{}, Stages: map[string]int64{}}, nil
}

type fakeItems struct{}

func (fakeItems) Detail(context.Context, int64) (biz.ItemDetail, error) {
	return biz.ItemDetail{}, biz.ErrItemNotFound
}

func (fakeItems) ListItems(context.Context, *biz.Cursor, int, biz.ItemFilter) ([]biz.Item, error) {
	return []biz.Item{}, nil
}

type fakeStories struct{}

func (fakeStories) Story(context.Context, int64) (biz.Story, error) {
	return biz.Story{}, biz.ErrStoryNotFound
}

type fakeHotRepository struct{}

func (fakeHotRepository) HotMentions(context.Context, time.Time, time.Time) ([]biz.HotMention, error) {
	return []biz.HotMention{}, nil
}

type fakeReportsRepository struct{}

func (fakeReportsRepository) GenerateReport(context.Context, biz.ReportRequest, func([]biz.ReportCandidate, []biz.Report) (biz.Report, error)) (biz.Report, error) {
	return biz.Report{}, nil
}

func (fakeReportsRepository) Report(context.Context, string) (biz.Report, error) {
	return biz.Report{}, biz.ErrReportNotFound
}

func (fakeReportsRepository) ListReports(context.Context, int) ([]biz.ReportSummary, error) {
	return []biz.ReportSummary{}, nil
}

type feedItemsRepository struct {
	all, selected []biz.Item
	err           error
}

func (feedItemsRepository) Detail(context.Context, int64) (biz.ItemDetail, error) {
	return biz.ItemDetail{}, biz.ErrItemNotFound
}

func (f feedItemsRepository) ListItems(_ context.Context, _ *biz.Cursor, _ int, filter biz.ItemFilter) ([]biz.Item, error) {
	if f.err != nil {
		return nil, f.err
	}
	if filter.View == "selected" {
		return f.selected, nil
	}
	return f.all, nil
}

type feedReportsRepository struct {
	summaries []biz.ReportSummary
	reports   map[string]biz.Report
	listErr   error
}

func (feedReportsRepository) GenerateReport(context.Context, biz.ReportRequest, func([]biz.ReportCandidate, []biz.Report) (biz.Report, error)) (biz.Report, error) {
	return biz.Report{}, nil
}

func (f feedReportsRepository) Report(_ context.Context, date string) (biz.Report, error) {
	report, ok := f.reports[date]
	if !ok {
		return biz.Report{}, biz.ErrReportNotFound
	}
	return report, nil
}

func (f feedReportsRepository) ListReports(context.Context, int) ([]biz.ReportSummary, error) {
	return f.summaries, f.listErr
}

func feedTestRouter(items feedItemsRepository, reports feedReportsRepository) *echo.Echo {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return feedTestRouterWithLogger(logger, items, reports)
}

func feedTestRouterWithLogger(logger *slog.Logger, items feedItemsRepository, reports feedReportsRepository) *echo.Echo {
	operations := service.NewOperationsService(biz.NewOperationsUseCase(fakePing{}), logger)
	serviceItems := service.NewItemsService(biz.NewItemsUseCase(items, time.Now), logger)
	stories := service.NewStoriesService(biz.NewStoriesUseCase(fakeStories{}), logger)
	hot := service.NewHotService(biz.NewHotUseCase(fakeHotRepository{}, time.Now), logger)
	serviceReports := service.NewReportsService(biz.NewReportsUseCase(reports, time.Now, nil), logger)
	cfg := config.Config{Site: config.Site{Name: "Vane", PublicURL: "https://vane.test"}}
	agent, mcpServer := testAgentServices(logger, cfg.Site, items)
	return NewRouter(logger, operations, cfg, serviceItems, stories, hot, serviceReports, service.NewSourcesService(biz.NewSourcesUseCase(biz.NewSources(cfg), nil, time.Now), nil), agent, mcpServer)
}

func testRouter(logger *slog.Logger, webDir string) *echo.Echo {
	return testRouterWithAgentItems(logger, webDir, fakeItems{})
}

func testRouterWithAgentItems(logger *slog.Logger, webDir string, agentItems biz.ItemsRepository) *echo.Echo {
	operations := service.NewOperationsService(biz.NewOperationsUseCase(fakePing{}), logger)
	items := service.NewItemsService(biz.NewItemsUseCase(fakeItems{}, time.Now), logger)
	stories := service.NewStoriesService(biz.NewStoriesUseCase(fakeStories{}), logger)
	hot := service.NewHotService(biz.NewHotUseCase(fakeHotRepository{}, time.Now), logger)
	reports := service.NewReportsService(biz.NewReportsUseCase(fakeReportsRepository{}, time.Now, nil), logger)
	cfg := config.Config{Site: config.Site{Name: "Vane", PublicURL: "https://vane.test"}, Web: config.Web{Dir: webDir}}
	agent, mcpServer := testAgentServices(logger, cfg.Site, agentItems)
	return NewRouter(logger, operations, cfg, items, stories, hot, reports, service.NewSourcesService(biz.NewSourcesUseCase(biz.NewSources(cfg), nil, time.Now), nil), agent, mcpServer)
}

func testAgentServices(logger *slog.Logger, site config.Site, repository biz.ItemsRepository) (*agentservice.AgentService, *mcp.Server) {
	agent := agentservice.NewAgentService(biz.NewItemsUseCase(repository, time.Now), biz.NewHotUseCase(fakeHotRepository{}, time.Now), biz.NewStoriesUseCase(fakeStories{}), biz.NewReportsUseCase(fakeReportsRepository{}, time.Now, nil), site, time.Now, logger)
	return agent, agentservice.NewMCPServer(agent)
}

func (fakeReportsRepository) MarkReportPushed(context.Context, string, time.Time) error { return nil }
func (feedReportsRepository) MarkReportPushed(context.Context, string, time.Time) error { return nil }

func (fakeItems) Topics(context.Context) ([]taxonomy.Topic, error) {
	return slices.Clone(taxonomy.Topics), nil
}

func (feedItemsRepository) Topics(context.Context) ([]taxonomy.Topic, error) {
	return slices.Clone(taxonomy.Topics), nil
}

func (fakeItems) SitemapReports(context.Context, int) ([]biz.SitemapResource, error) {
	return []biz.SitemapResource{}, nil
}

func (fakeItems) SitemapItems(context.Context, biz.SitemapQuery) ([]biz.SitemapResource, error) {
	return []biz.SitemapResource{}, nil
}

func (f feedItemsRepository) SitemapReports(context.Context, int) ([]biz.SitemapResource, error) {
	return []biz.SitemapResource{}, f.err
}

func (f feedItemsRepository) SitemapItems(context.Context, biz.SitemapQuery) ([]biz.SitemapResource, error) {
	return []biz.SitemapResource{}, f.err
}

func TestCategoryFeedRoutesAndFailureCache(t *testing.T) {
	router := feedTestRouter(feedItemsRepository{}, feedReportsRepository{})
	for _, slug := range []string{"model", "product", "industry", "paper", "tutorial", "opinion"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/feed/category/"+slug+".xml", nil))
		if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/rss+xml") || response.Header().Get("Cache-Control") != "public, max-age=60" {
			t.Errorf("%s status=%d headers=%v", slug, response.Code, response.Header())
		}
	}
	for _, slug := range []string{"unknown.xml", "agent.xml", "paper", "paper.json"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/feed/category/"+slug, nil))
		if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "<html") {
			t.Errorf("unknown category %s status=%d body=%s", slug, response.Code, response.Body)
		}
	}
	response := httptest.NewRecorder()
	feedTestRouter(feedItemsRepository{err: errors.New("private repository detail")}, feedReportsRepository{}).ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/feed/category/paper.xml", nil))
	if response.Code != http.StatusInternalServerError || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "private repository detail") {
		t.Fatalf("failure status=%d headers=%v body=%s", response.Code, response.Header(), response.Body)
	}
}

func TestInputValidationKeepsTransportStatusAndFailureCache(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/api/items?limit=not-an-integer", http.StatusUnprocessableEntity},
		{"/api/items?limit=0", http.StatusBadRequest},
		{"/api/items?cursor=invalid", http.StatusBadRequest},
		{"/api/items?category=invalid", http.StatusBadRequest},
		{"/api/items/invalid", http.StatusBadRequest},
		{"/api/stories/0", http.StatusBadRequest},
		{"/api/reports/invalid", http.StatusBadRequest},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))
			if response.Code != tc.status || response.Header().Get("Cache-Control") != noStoreCacheControl || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/problem+json") {
				t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body)
			}
		})
	}
}
