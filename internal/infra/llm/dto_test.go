package llm

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/bogeia/vane/internal/biz"
)

func assertModelJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var actual, expected any
	if err := json.Unmarshal(got, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("JSON=%s\nwant=%s", got, want)
	}
}

func TestWritingDTOContractAndDomainMapping(t *testing.T) {
	const payload = `{"roundup":true,"title":"中文","summary":"摘要","reason":"理由","category":"论文","tags":["RAG"],"subjects":["openai"],"fact":{"title":"发布","subject":"OpenAI","action":"发布","object":"模型","occurred_at":"2026-10-22"}}`
	var dto writingDTO
	if err := json.Unmarshal([]byte(payload), &dto); err != nil {
		t.Fatal(err)
	}
	date := "2026-10-22"
	want := biz.Writing{Roundup: true, Title: "中文", Summary: "摘要", Reason: "理由", Category: "论文", Tags: []string{"RAG"}, Subjects: []string{"openai"}, Fact: &biz.Fact{Title: "发布", Subject: "OpenAI", Action: "发布", Object: "模型", OccurredAt: &date}}
	if got := dto.domain(); !reflect.DeepEqual(got, want) {
		t.Fatalf("domain=%+v", got)
	}
	encoded, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	assertModelJSON(t, encoded, payload)
	var empty writingDTO
	if err := json.Unmarshal([]byte(`{"tags":[],"subjects":[],"fact":null}`), &empty); err != nil {
		t.Fatal(err)
	}
	if got := empty.domain(); got.Fact != nil || got.Tags == nil || got.Subjects == nil {
		t.Fatalf("empty=%+v", got)
	}
	if (writingDTO{}).domain().Tags != nil {
		t.Fatal("nil tags became empty")
	}
	fact, err := json.Marshal(factDTO{Title: "发布"})
	if err != nil {
		t.Fatal(err)
	}
	assertModelJSON(t, fact, `{"title":"发布","subject":"","action":"","object":"","occurred_at":null}`)
}

func TestStoryCandidateDTOContract(t *testing.T) {
	candidates := []biz.StoryCandidate{{StoryMember: biz.StoryMember{StoryID: 42, Title: "标题", Summary: "摘要"}, Similarity: 0.75}}
	encoded, err := json.Marshal(storyCandidatesDTO(candidates))
	if err != nil {
		t.Fatal(err)
	}
	assertModelJSON(t, encoded, `[{"story_id":42,"title":"标题","summary":"摘要","similarity":0.75}]`)
	for _, tc := range []struct {
		input []biz.StoryCandidate
		want  string
	}{{nil, `null`}, {[]biz.StoryCandidate{}, `[]`}} {
		encoded, err := json.Marshal(storyCandidatesDTO(tc.input))
		if err != nil {
			t.Fatal(err)
		}
		assertModelJSON(t, encoded, tc.want)
	}
}
