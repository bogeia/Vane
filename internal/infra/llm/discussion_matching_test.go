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

func matchingInput() biz.DiscussionMatchInput {
	return biz.DiscussionMatchInput{
		Thread:     biz.HNThread{ID: 100, Title: "OpenAI releases GPT-4.1", URL: "https://openai.com/index/gpt-4-1/", PublishedAt: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)},
		Candidates: []biz.DiscussionCandidate{{StoryID: 17, Items: []biz.DiscussionCandidateItem{{ID: 23, URL: "https://media.example/gpt-4-1", OriginalTitle: "OpenAI releases GPT-4.1", Summary: "OpenAI 发布 GPT-4.1。", Fact: biz.Fact{Subject: "OpenAI", Action: "发布", Object: "GPT-4.1"}}}}},
	}
}

func matchingResponse() string {
	return `{"decision":"same_occurrence","story_id":17,"evidence":[` +
		`{"role":"subject","hn_field":"title","hn_quote":"OpenAI","item_id":23,"event_field":"fact.subject","event_quote":"OpenAI"},` +
		`{"role":"action","hn_field":"title","hn_quote":"releases","item_id":23,"event_field":"fact.action","event_quote":"发布"},` +
		`{"role":"object","hn_field":"title","hn_quote":"GPT-4.1","item_id":23,"event_field":"fact.object","event_quote":"GPT-4.1"},` +
		`{"role":"anchor","hn_field":"title","hn_quote":"GPT-4.1","item_id":23,"event_field":"fact.object","event_quote":"GPT-4.1"}]}`
}

func TestDiscussionMatcherProtocolAndGroupModel(t *testing.T) {
	valid := matchingResponse()
	for _, tc := range []struct {
		name, response string
		wantError      bool
	}{
		{"cross-language occurrence", valid, false},
		{"unknown", `{"decision":"unknown","story_id":0,"evidence":[]}`, false},
		{"different", `{"decision":"different","story_id":0,"evidence":[]}`, false},
		{"missing decision", `{"story_id":0,"evidence":[]}`, true},
		{"null decision", `{"decision":null,"story_id":0,"evidence":[]}`, true},
		{"invalid enum", `{"decision":"SAME_STORY","story_id":0,"evidence":[]}`, true},
		{"missing id", `{"decision":"unknown","evidence":[]}`, true},
		{"missing evidence", `{"decision":"unknown","story_id":0}`, true},
		{"null evidence", `{"decision":"unknown","story_id":0,"evidence":null}`, true},
		{"unknown candidate", strings.Replace(valid, `"story_id":17`, `"story_id":99`, 1), true},
		{"wrong item", strings.ReplaceAll(valid, `"item_id":23`, `"item_id":99`), true},
		{"forged quote", strings.Replace(valid, `"hn_quote":"GPT-4.1"`, `"hn_quote":"GPT-4.5"`, 1), true},
		{"invalid field", strings.Replace(valid, `"hn_field":"title"`, `"hn_field":"comments"`, 1), true},
		{"publication date as occurrence", strings.Replace(valid, `"event_field":"fact.object"`, `"event_field":"published_at"`, 1), true},
		{"duplicate role", strings.Replace(valid, `"role":"anchor"`, `"role":"subject"`, 1), true},
		{"missing field", strings.Replace(valid, `"role":"subject",`, "", 1), true},
		{"null field", strings.Replace(valid, `"role":"subject"`, `"role":null`, 1), true},
		{"confidence forbidden", strings.Replace(valid, `"story_id":17`, `"story_id":17,"confidence":1`, 1), true},
		{"unknown cannot select", `{"decision":"unknown","story_id":17,"evidence":[]}`, true},
		{"different cannot cite", strings.Replace(valid, `"decision":"same_occurrence"`, `"decision":"different"`, 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request struct {
					Model    string `json:"model"`
					Reason   string `json:"reasoning_effort"`
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Model != "matching-model" || request.Reason != "low" || len(request.Messages) != 2 {
					t.Errorf("request=%+v", request)
				}
				if len(request.Messages) == 2 {
					for _, phrase := range []string{"不可信", "GPT-4.1 不等于 GPT-4.5", "重新提交", "全部 candidates", "没有评论"} {
						if !strings.Contains(request.Messages[0].Content, phrase) {
							t.Errorf("prompt missing %q", phrase)
						}
					}
					var material map[string]json.RawMessage
					if err := json.Unmarshal([]byte(request.Messages[1].Content), &material); err != nil {
						t.Error(err)
					}
					if strings.Contains(request.Messages[1].Content, `"comments"`) || !strings.Contains(request.Messages[1].Content, `"story_id":17`) || !strings.Contains(request.Messages[1].Content, `"item_id":23`) || !strings.Contains(request.Messages[1].Content, `"submitted_at":"2026-11-03T00:00:00Z"`) {
						t.Error("invalid bounded matching material")
					}
				}
				writeCompletion(w, tc.response)
			}))
			defer server.Close()
			client := llm.New(config.LLM{Enabled: true, BaseURL: "http://unused.invalid", Model: "base", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}, Steps: config.LLMSteps{Group: config.LLMStep{BaseURL: server.URL, Model: "matching-model", ReasoningEffort: "low"}}})
			match, err := client.MatchDiscussion(t.Context(), matchingInput())
			if (err != nil) != tc.wantError {
				t.Fatalf("match=%+v err=%v", match, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("calls=%d; want one request for all candidates", calls.Load())
			}
		})
	}
}

func TestDiscussionMatcherBoundsAndEmptyCandidatesAvoidNetwork(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*biz.DiscussionMatchInput)
		wantError bool
	}{
		{"empty", func(in *biz.DiscussionMatchInput) { in.Candidates = nil }, false},
		{"too many events", func(in *biz.DiscussionMatchInput) {
			in.Candidates = make([]biz.DiscussionCandidate, biz.DiscussionEventLimit+1)
		}, true},
		{"oversized material", func(in *biz.DiscussionMatchInput) { in.Candidates[0].Items[0].Summary = strings.Repeat("x", 256*1024) }, true},
		{"duplicate event", func(in *biz.DiscussionMatchInput) { in.Candidates = append(in.Candidates, in.Candidates[0]) }, true},
		{"duplicate item", func(in *biz.DiscussionMatchInput) {
			in.Candidates[0].Items = append(in.Candidates[0].Items, in.Candidates[0].Items[0])
		}, true},
		{"missing thread id", func(in *biz.DiscussionMatchInput) { in.Thread.ID = 0 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := matchingInput()
			tc.mutate(&input)
			// Disabled client makes any unintended request fail immediately.
			match, err := llm.New(config.LLM{}).MatchDiscussion(t.Context(), input)
			if (err != nil) != tc.wantError {
				t.Fatalf("match=%+v err=%v", match, err)
			}
			if !tc.wantError && match.Decision != biz.DiscussionMatchUnknown {
				t.Fatalf("match=%+v", match)
			}
		})
	}
}

func TestDiscussionMatcherComparesFiftyEventsInOneRequest(t *testing.T) {
	input := matchingInput()
	item := input.Candidates[0].Items[0]
	input.Candidates = nil
	for i := range biz.DiscussionEventLimit {
		member := item
		member.ID = int64(i + 1)
		input.Candidates = append(input.Candidates, biz.DiscussionCandidate{StoryID: int64(i + 1), Items: []biz.DiscussionCandidateItem{member}})
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Messages) != 2 {
			t.Errorf("messages=%d", len(request.Messages))
		} else {
			var material struct {
				Candidates []json.RawMessage `json:"candidates"`
			}
			if err := json.Unmarshal([]byte(request.Messages[1].Content), &material); err != nil {
				t.Error(err)
			}
			if len(material.Candidates) != biz.DiscussionEventLimit {
				t.Errorf("candidates=%d", len(material.Candidates))
			}
		}
		writeCompletion(w, `{"decision":"unknown","story_id":0,"evidence":[]}`)
	}))
	defer server.Close()
	client := llm.New(config.LLM{Enabled: true, BaseURL: server.URL, Model: "matching", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}})
	match, err := client.MatchDiscussion(t.Context(), input)
	if err != nil || match.Decision != biz.DiscussionMatchUnknown || calls.Load() != 1 {
		t.Fatalf("match=%+v err=%v calls=%d", match, err, calls.Load())
	}
}
