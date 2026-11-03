package llm_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/llm"
)

func TestModelAttemptLogsUsageAndRetries(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"label":"PASS"}`}}}, "usage": map[string]int{"prompt_tokens": 13, "completion_tokens": 0}})
	}))
	defer server.Close()
	client := llm.New(config.LLM{Enabled: true, BaseURL: server.URL, APIKey: "private-key", Model: "fake", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 2, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}})
	if _, err := client.Prefilter(t.Context(), biz.WorkItem{Body: "private-material"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("logs=%s", logs.String())
	}
	for i, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["step"] != "prefilter" || record["model"] != "fake" || record["duration"] == nil {
			t.Fatalf("record=%v", record)
		}
		if i == 0 && (record["result"] != "http_error" || record["input_tokens"] != "unknown") {
			t.Fatalf("record=%v", record)
		}
		if i == 1 && (record["result"] != "http_success" || record["input_tokens"] != float64(13) || record["output_tokens"] != float64(0)) {
			t.Fatalf("record=%v", record)
		}
	}
	for _, secret := range []string{server.URL, "private-key", "private-material"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("log leaked sensitive value")
		}
	}
}

func TestModelAttemptLogsMissingUsageAndInvalidResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body, result string
		wantError          bool
	}{
		{"missing usage", `{"choices":[{"message":{"content":"{\"label\":\"PASS\"}"}}]}`, "http_success", false},
		{"incompatible usage", `{"choices":[{"message":{"content":"{\"label\":\"PASS\"}"}}],"usage":"unsupported"}`, "http_success", false},
		{"invalid envelope", `{"choices":[]}`, "invalid_response", true},
		{"invalid model JSON", `{"choices":[{"message":{"content":"no JSON"}}]}`, "invalid_response", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			old := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(old) })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			client := llm.New(config.LLM{Enabled: true, BaseURL: server.URL, Model: "fake", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}})
			_, err := client.Prefilter(t.Context(), biz.WorkItem{})
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			var record map[string]any
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record["result"] != tc.result || record["input_tokens"] != "unknown" || record["output_tokens"] != "unknown" {
				t.Fatalf("record=%v", record)
			}
		})
	}
}
