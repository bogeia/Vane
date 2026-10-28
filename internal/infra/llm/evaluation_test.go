package llm_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/llm"
)

func TestClientPrefilterAndScoreUseExpectedOpenAIRequests(t *testing.T) {
	type capturedRequest struct {
		Path           string
		Authorization  string
		Model          string
		Temperature    *float64
		ResponseFormat string
		SystemPrompt   string
		UserMaterial   map[string]json.RawMessage
	}
	requests := make(chan capturedRequest, 5)
	var call atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model          string   `json:"model"`
			Temperature    *float64 `json:"temperature"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		capture := capturedRequest{Path: r.URL.Path, Authorization: r.Header.Get("Authorization")}
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			capture.Model = payload.Model
			capture.Temperature = payload.Temperature
			capture.ResponseFormat = payload.ResponseFormat.Type
			for _, message := range payload.Messages {
				switch message.Role {
				case "system":
					capture.SystemPrompt = message.Content
				case "user":
					_ = json.Unmarshal([]byte(message.Content), &capture.UserMaterial)
				}
			}
		}
		requests <- capture

		var content string
		switch call.Add(1) {
		case 1:
			content = `{"label":"PASS","reason":"相关"}`
		case 2:
			content = `{"label":"UNKNOWN","reason":"不确定"}`
		case 3:
			content = `{"label":"BLOCK","reason":"无关"}`
		case 4:
			content = `{"label":"INVALID","reason":"不支持"}`
		default:
			content = `{"attention_score":85}`
		}
		writeCompletion(w, content)
	}))
	defer server.Close()

	client := llm.New(config.LLM{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 4, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}, Enabled: true, BaseURL: server.URL, APIKey: "test-key", Model: "fake"})
	publishedAt := time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC)
	item := biz.WorkItem{
		ID: 9, OriginalTitle: "原始标题", Body: strings.Repeat("正", 6001),
		PublishedAt: publishedAt, Attempts: 1, Tier: "media",
	}
	for _, want := range []string{"PASS", "UNKNOWN", "BLOCK"} {
		got, err := client.Prefilter(t.Context(), item)
		if err != nil || got != want {
			t.Fatalf("Prefilter() = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := client.Prefilter(t.Context(), item); err == nil {
		t.Fatal("Prefilter accepted an unsupported outcome")
	}
	score, err := client.Score(t.Context(), item)
	if err != nil {
		t.Fatalf("Score(): %v", err)
	}
	if score != 85 {
		t.Fatalf("Score() = %d, want 85", score)
	}

	for index := range 5 {
		got := <-requests
		if got.Path != "/chat/completions" || got.Authorization != "Bearer test-key" || got.Model != "fake" || got.ResponseFormat != "json_object" {
			t.Errorf("request %d protocol = %+v, want OpenAI-compatible JSON request", index+1, got)
		}
		if index < 4 && !strings.Contains(got.SystemPrompt, "相关性预筛") {
			t.Errorf("prefilter system prompt omitted its purpose: %q", got.SystemPrompt)
		}
		if index == 4 {
			if got.Temperature == nil || *got.Temperature != 0.7 {
				t.Errorf("score temperature = %v, want 0.7", got.Temperature)
			}
			if !strings.Contains(got.SystemPrompt, "事件注意力评分器") {
				t.Errorf("score system prompt omitted its purpose: %q", got.SystemPrompt)
			}
			var title string
			var gotDate time.Time
			var body string
			_ = json.Unmarshal(got.UserMaterial["title"], &title)
			_ = json.Unmarshal(got.UserMaterial["published_at"], &gotDate)
			_ = json.Unmarshal(got.UserMaterial["body"], &body)
			if len(got.UserMaterial) != 3 || title != "原始标题" || !gotDate.Equal(publishedAt) || len([]rune(body)) != 6000 {
				t.Errorf("score material keys/title/date/body length = %v/%q/%s/%d; want only title,published_at,body and 6000 runes", materialKeys(got.UserMaterial), title, gotDate, len([]rune(body)))
			}
			if _, ok := got.UserMaterial["tier"]; ok {
				t.Error("score material included source tier")
			}
		}
	}
}

func materialKeys(material map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(material))
	for key := range material {
		keys = append(keys, key)
	}
	return keys
}

func TestClientReportsWhichStepRejectedModelJSON(t *testing.T) {
	const rawOutput = `MODEL_PRIVATE_OUTPUT`
	tests := []struct {
		name string
		step string
		call func(*llm.Client) error
	}{
		{name: "prefilter", step: "prefilter", call: func(client *llm.Client) error {
			_, err := client.Prefilter(t.Context(), biz.WorkItem{})
			return err
		}},
		{name: "score", step: "score", call: func(client *llm.Client) error {
			_, err := client.Score(t.Context(), biz.WorkItem{})
			return err
		}},
		{name: "writing", step: "writing", call: func(client *llm.Client) error {
			_, err := client.Write(t.Context(), biz.WorkItem{})
			return err
		}},
		{name: "grouping", step: "grouping", call: func(client *llm.Client) error {
			_, err := client.Group(t.Context(), biz.WorkItem{Writing: &biz.Writing{}}, nil)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeCompletion(w, rawOutput)
			}))
			defer server.Close()
			client := llm.New(config.LLM{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 1}, Enabled: true, BaseURL: server.URL, APIKey: "test-key", Model: "fake-model"})

			err := test.call(client)
			if err == nil || !strings.Contains(err.Error(), test.step) {
				t.Fatalf("error = %v, want %q step-specific parse error", err, test.step)
			}
			if test.name != "writing" && strings.Contains(err.Error(), "writing") {
				t.Errorf("error = %q, want no writing label for %s", err, test.step)
			}
			if strings.Contains(err.Error(), rawOutput) || strings.Contains(err.Error(), "test-key") {
				t.Errorf("error exposed model output or API key: %q", err)
			}
		})
	}
}
