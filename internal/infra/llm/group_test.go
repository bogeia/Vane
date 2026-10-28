package llm_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/llm"
)

func TestClientGroupsAgainstOnlyProvidedStoryCandidates(t *testing.T) {
	wantedContent := `{"relations":[{"story_id":17,"relation":"SAME_OCCURRENCE","confidence":0.8},{"story_id":22,"relation":"UNRELATED","confidence":0.9}]}`
	var gotPrompt, gotMaterial string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		var request struct {
			Model          string `json:"model"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request JSON", http.StatusBadRequest)
			return
		}
		if request.Model != "fake-model" || request.ResponseFormat.Type != "json_object" {
			http.Error(w, "unexpected model request format", http.StatusBadRequest)
			return
		}
		for _, message := range request.Messages {
			switch message.Role {
			case "system":
				gotPrompt = message.Content
			case "user":
				gotMaterial = message.Content
			}
		}
		writeCompletion(w, wantedContent)
	}))
	defer server.Close()

	client := llm.New(config.LLM{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 4, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}, Enabled: true, BaseURL: server.URL, APIKey: "test-key", Model: "fake-model"})
	item := biz.WorkItem{
		OriginalTitle: "原标题",
		PublishedAt:   time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC),
		Writing:       &biz.Writing{Title: "中文事件标题", Summary: "中文事件摘要"},
	}
	candidates := []biz.StoryCandidate{
		{StoryMember: biz.StoryMember{StoryID: 17, Title: "候选事件一", Summary: "候选摘要一"}, Similarity: 0.8},
		{StoryMember: biz.StoryMember{StoryID: 22, Title: "候选事件二", Summary: "候选摘要二"}, Similarity: 0.4},
	}

	got, err := client.Group(t.Context(), item, candidates)
	if err != nil {
		t.Fatalf("Group(): %v", err)
	}
	if len(got) != 2 || got[0].StoryID != 17 || got[0].Relation != "SAME_OCCURRENCE" || got[0].Confidence != 0.8 || got[1].StoryID != 22 || got[1].Relation != "UNRELATED" {
		t.Fatalf("Group() = %+v, want parsed candidate relations", got)
	}
	if !strings.Contains(gotPrompt, "事件归组") {
		t.Errorf("grouping system prompt omitted its purpose: %q", gotPrompt)
	}
	var material struct {
		Title      string `json:"title"`
		Summary    string `json:"summary"`
		Candidates []struct {
			StoryID int64  `json:"story_id"`
			Title   string `json:"title"`
			Summary string `json:"summary"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(gotMaterial), &material); err != nil {
		t.Fatalf("decode grouping material %q: %v", gotMaterial, err)
	}
	if material.Title != "中文事件标题" || material.Summary != "中文事件摘要" || len(material.Candidates) != 2 || material.Candidates[0].StoryID != 17 || material.Candidates[0].Title != "候选事件一" || material.Candidates[1].StoryID != 22 {
		t.Errorf("grouping material = %+v, want writing and only the supplied candidates", material)
	}
}

const wantedRelationsForGroupTest = `{"relations":[{"story_id":17,"relation":"SAME_STORY","confidence":0.9},{"story_id":22,"relation":"UNRELATED","confidence":0.8}]}`

func TestClientGroupAcceptsNoRelationsAndRejectsInvalidRelations(t *testing.T) {
	testCases := []struct {
		name        string
		content     string
		candidates  []biz.StoryCandidate
		wantSuccess bool
		wantCount   int
	}{
		{name: "empty candidates allow empty relations", content: `{"relations":[]}`, candidates: []biz.StoryCandidate{}, wantSuccess: true},
		{name: "one candidate cannot be omitted", content: `{"relations":[]}`},
		{name: "two candidates cannot have one omitted", content: `{"relations":[{"story_id":17,"relation":"UNRELATED","confidence":0.9}]}`, candidates: []biz.StoryCandidate{
			{StoryMember: biz.StoryMember{StoryID: 17, Title: "候选事件一"}, Similarity: 0.5},
			{StoryMember: biz.StoryMember{StoryID: 22, Title: "候选事件二"}, Similarity: 0.4},
		}},
		{name: "all candidates have relations", content: wantedRelationsForGroupTest, candidates: []biz.StoryCandidate{
			{StoryMember: biz.StoryMember{StoryID: 17, Title: "候选事件一"}, Similarity: 0.5},
			{StoryMember: biz.StoryMember{StoryID: 22, Title: "候选事件二"}, Similarity: 0.4},
		}, wantSuccess: true, wantCount: 2},
		{name: "unknown story id", content: `{"relations":[{"story_id":99,"relation":"SAME_STORY","confidence":0.9}]}`},
		{name: "unsupported relation", content: `{"relations":[{"story_id":17,"relation":"MAYBE","confidence":0.9}]}`},
		{name: "confidence above one", content: `{"relations":[{"story_id":17,"relation":"SAME_STORY","confidence":1.1}]}`},
		{name: "negative confidence", content: `{"relations":[{"story_id":17,"relation":"SAME_STORY","confidence":-0.1}]}`},
		{name: "duplicate candidate relation", content: `{"relations":[{"story_id":17,"relation":"SAME_STORY","confidence":0.9},{"story_id":17,"relation":"UNRELATED","confidence":0.8}]}`},
	}
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeCompletion(w, test.content)
			}))
			defer server.Close()
			client := llm.New(config.LLM{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 4, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}, Enabled: true, BaseURL: server.URL, APIKey: "test-key", Model: "fake-model"})
			candidates := test.candidates
			if candidates == nil {
				candidates = []biz.StoryCandidate{{StoryMember: biz.StoryMember{StoryID: 17, Title: "候选事件"}, Similarity: 0.5}}
			}
			got, err := client.Group(t.Context(), biz.WorkItem{Writing: &biz.Writing{Title: "新资料", Summary: "摘要"}}, candidates)
			if test.wantSuccess {
				if err != nil || len(got) != test.wantCount {
					t.Fatalf("Group() = %+v, %v; want %d valid candidate relations", got, err, test.wantCount)
				}
				return
			}
			if err == nil {
				t.Fatalf("Group() accepted invalid relation response: %+v", got)
			}
		})
	}
}
