package llm_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/llm"
)

func TestClientRoutesEachStepAndPreservesReasoningEffort(t *testing.T) {
	type capture struct {
		path, auth string
		body       map[string]json.RawMessage
	}
	requests := make(chan capture, 4)
	server := func(content string) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			requests <- capture{r.URL.Path, r.Header.Get("Authorization"), body}
			writeCompletion(w, content)
		}))
		t.Cleanup(s.Close)
		return s
	}
	fallback := server(`{"label":"PASS"}`)
	score := server(`{"attention_score":85}`)
	write := server(`{"title":"中文标题","summary":"资料摘要内容","subjects":[],"fact":null,"roundup":false,"category":"论文"}`)
	group := server(`{"relations":[]}`)
	client := llm.New(config.LLM{Enabled: true, BaseURL: fallback.URL + "/default", APIKey: "default-key", Model: "default-model", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}, Steps: config.LLMSteps{
		Prefilter: config.LLMStep{ReasoningEffort: "none"},
		Score:     config.LLMStep{BaseURL: score.URL + "/score", APIKey: "score-key", Model: "score-model"},
		Write:     config.LLMStep{BaseURL: write.URL + "/write", Model: "writer", ReasoningEffort: "custom VALUE"},
		Group:     config.LLMStep{BaseURL: group.URL + "/group", APIKey: "group-key"},
	}})
	item := biz.WorkItem{Writing: &biz.Writing{Title: "资料标题", Summary: "资料摘要"}}
	if _, err := client.Prefilter(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Score(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Group(t.Context(), item, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ path, auth, model, reason string }{
		{"/default/chat/completions", "Bearer default-key", "default-model", "none"},
		{"/score/chat/completions", "Bearer score-key", "score-model", ""},
		{"/write/chat/completions", "Bearer default-key", "writer", "custom VALUE"},
		{"/group/chat/completions", "Bearer group-key", "default-model", ""},
	} {
		got := <-requests
		var model, reason string
		_ = json.Unmarshal(got.body["model"], &model)
		value, present := got.body["reasoning_effort"]
		_ = json.Unmarshal(value, &reason)
		if got.path != want.path || got.auth != want.auth || model != want.model || reason != want.reason || present != (want.reason != "") {
			t.Errorf("request = %+v model=%s reasoning=%s present=%v, want %+v", got, model, reason, present, want)
		}
	}
}
