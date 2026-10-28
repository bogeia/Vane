package llm_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/llm"
)

func TestClientRequiresStructuredWritingFields(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		subjects, fact                any
		omitSubjects, omitFact, valid bool
	}{
		{name: "missing subjects", fact: nil, omitSubjects: true},
		{name: "missing fact", subjects: []string{}, omitFact: true},
		{name: "null subjects", subjects: nil},
		{name: "string subjects", subjects: "openai"},
		{name: "string fact", subjects: []string{}, fact: "release"},
		{name: "array fact", subjects: []string{}, fact: []string{}},
		{name: "number fact", subjects: []string{}, fact: 42},
		{name: "boolean fact", subjects: []string{}, fact: true},
		{name: "null fact", subjects: []string{}, valid: true},
		{name: "object fact", subjects: []string{}, fact: map[string]any{"category": "论文", "title": "OpenAI发布模型", "subject": "OpenAI", "action": "发布", "object": "模型", "occurred_at": nil}, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := map[string]any{"category": "论文", "title": "GPT-5 发布", "summary": "新模型提升了编码能力。", "roundup": false}
			if !tc.omitSubjects {
				output["subjects"] = tc.subjects
			}
			if !tc.omitFact {
				output["fact"] = tc.fact
			}
			_, err := writeStructuredResponse(t, output)
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, "invalid writing JSON")
			}
		})
	}
}

func writeStructuredResponse(t *testing.T, output map[string]any) (biz.Writing, error) {
	t.Helper()
	content, err := json.Marshal(output)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeCompletion(w, string(content)) }))
	defer server.Close()
	client := llm.New(config.LLM{Enabled: true, BaseURL: server.URL, APIKey: "test-key", Model: "fake", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}})
	return client.Write(t.Context(), biz.WorkItem{})
}

func TestClientNormalizesStructuredWriting(t *testing.T) {
	validDate := "2026-10-22"
	for _, tc := range []struct {
		name, date string
		roundup    bool
		wantDate   *string
	}{
		{name: "valid explicit date", date: "2026-10-22", wantDate: &validDate},
		{name: "invalid calendar date", date: "2026-02-30"},
		{name: "not a date", date: "yesterday"},
		{name: "wrong date format", date: "2026-1-02"},
		{name: "roundup discards fact", date: "2026-10-22", roundup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := map[string]any{"category": "论文", "title": "GPT-5 发布", "summary": "新模型提升了编码能力。", "roundup": tc.roundup, "subjects": []string{"openai", "unknown", "google", "openai", "deepmind", "anthropic", "google"}, "fact": map[string]any{"category": "论文", "title": " " + longWritingText(31) + " ", "subject": longWritingText(51), "action": longWritingText(51), "object": longWritingText(51), "occurred_at": tc.date}}
			got, err := writeStructuredResponse(t, output)
			require.NoError(t, err)
			if diff := cmp.Diff([]string{"openai", "google", "anthropic"}, got.Subjects); diff != "" {
				t.Errorf("subjects mismatch (-want +got):\n%s", diff)
			}
			if tc.roundup {
				assert.Nil(t, got.Fact, "roundup retained fact")
				return
			}
			wantFact := &biz.Fact{Title: longWritingText(30), Subject: longWritingText(50), Action: longWritingText(50), Object: longWritingText(50), OccurredAt: tc.wantDate}
			if diff := cmp.Diff(wantFact, got.Fact); diff != "" {
				t.Errorf("fact mismatch (-want +got):\n%s", diff)
			}
		})
	}
	for _, title := range []string{"", " \n\t "} {
		t.Run("empty fact title/"+title, func(t *testing.T) {
			_, err := writeStructuredResponse(t, map[string]any{"category": "论文", "title": "GPT-5 发布", "summary": "新模型提升了编码能力。", "roundup": false, "subjects": []string{}, "fact": map[string]any{"category": "论文", "title": title}})
			assert.EqualError(t, err, "invalid writing JSON")
		})
	}
	t.Run("fact excluded from writing refusal and degeneration checks", func(t *testing.T) {
		_, err := writeStructuredResponse(t, map[string]any{"category": "论文", "title": "GPT-5 发布", "summary": "新模型提升了编码能力。", "roundup": false, "subjects": []string{}, "fact": map[string]any{"category": "论文", "title": "模型拒答研究", "subject": "作为一个AI", "action": "只输出 JSON", "object": "抱歉"}})
		assert.NoError(t, err, "fact triggered writing quality rule")
	})
}
