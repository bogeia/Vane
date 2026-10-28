package integration

import (
	"encoding/json"
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
)

func TestProcessorRetriesDegenerateWritingWithoutRepeatingCompletedSteps(t *testing.T) {
	testProcessorRecoversWriting(t, `{"title":"GPT-5 发布","summary":"GPT-5 发布","subjects":[],"fact":null,"roundup":false,"category":"模型"}`, "writing degenerate")
}

func TestProcessorRetriesMissingStructuredFieldsWithoutRepeatingCompletedSteps(t *testing.T) {
	for _, output := range []string{
		`{"title":"GPT-5 发布","summary":"新模型提升了编码能力。","roundup":false,"fact":null}`,
		`{"title":"GPT-5 发布","summary":"新模型提升了编码能力。","roundup":false,"subjects":[]}`,
	} {
		t.Run(output, func(t *testing.T) { testProcessorRecoversWriting(t, output, "invalid writing JSON") })
	}
}

func testProcessorRecoversWriting(t *testing.T, firstOutput, wantError string) {
	t.Helper()
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	var prefilters, scores, writings atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		output := `{"title":"GPT-5 发布","summary":"新模型提升了编码能力。","subjects":[],"fact":null,"roundup":false,"category":"模型"}`
		if writings.Load() == 0 {
			output = firstOutput
		}
		switch writeFakeModelResponse(w, r, output) {
		case "prefilter":
			prefilters.Add(1)
		case "score":
			scores.Add(1)
		case "write":
			writings.Add(1)
		}
	}))
	defer server.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.LLM.Enabled = true
	cfg.LLM.BaseURL = server.URL
	cfg.LLM.APIKey = "test-key"
	cfg.LLM.Model = "fake"
	cfg.Worker.Count = 1
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "quality", Name: "Quality fixture", Tier: "official", FeedURL: "https://example.test/feed", Enabled: false, IntervalMinutes: 60}}
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	app, cleanup, err := newTestApp(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now })
	if err != nil {
		t.Fatalf("assemble app: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,body,published_at,next_attempt_at) VALUES('quality','https://example.test/quality','GPT-5 release',$1,$2,$2)`, fixtureBody, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := app.Processor.RunOnce(t.Context()); err == nil || !strings.Contains(err.Error(), wantError) {
		t.Fatalf("first attempt error=%v, want %s", err, wantError)
	}
	response := httptest.NewRecorder()
	app.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil))
	var page struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Items) != 0 {
		t.Fatalf("degenerate writing became public: %d %s", response.Code, response.Body.String())
	}
	now = now.Add(cfg.Worker.Retry.InitialInterval)
	if err := app.Processor.RunOnce(t.Context()); err != nil {
		t.Fatalf("retry normal writing: %v", err)
	}
	if prefilters.Load() != 1 || scores.Load() != 2 || writings.Load() != 2 {
		t.Fatalf("calls prefilter=%d score=%d writing=%d, want 1/2/2", prefilters.Load(), scores.Load(), writings.Load())
	}
	response = httptest.NewRecorder()
	app.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/items?view=all", nil))
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].Title != "GPT-5 发布" {
		t.Fatalf("recovered writing not published: %d %s", response.Code, response.Body.String())
	}
}
