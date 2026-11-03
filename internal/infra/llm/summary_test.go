package llm_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/llm"
)

func TestEnhanceSummaryContract(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		wantError      bool
	}{
		{"valid", `{"summary":"研究以对照实验检验推理方法，并报告任务表现。"}`, false},
		{"uncertain", `{"summary":""}`, false},
		{"missing", `{}`, true},
		{"null", `{"summary":null}`, true},
		{"refusal", `{"summary":"抱歉，我无法完成摘要。"}`, true},
		{"degenerate", `{"summary":"片段片段片段片段"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Model    string `json:"model"`
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Model != "writer" || !strings.Contains(request.Messages[1].Content, `"type":"paper"`) {
					t.Errorf("unexpected request: %+v", request)
				}
				writeCompletion(w, tc.response)
			}))
			defer server.Close()
			client := llm.New(config.LLM{Enabled: true, BaseURL: server.URL, Model: "base", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}, Steps: config.LLMSteps{Write: config.LLMStep{Model: "writer"}}})
			item := biz.WorkItem{Selected: true, Body: "原文研究内容", Writing: &biz.Writing{Title: "研究标题", Summary: "基础摘要内容", Category: "论文"}}
			summary, err := client.EnhanceSummary(t.Context(), item)
			if (err != nil) != tc.wantError {
				t.Fatalf("summary=%q error=%v", summary, err)
			}
			if item.Writing.Summary != "基础摘要内容" {
				t.Fatal("mutated base writing")
			}
			if tc.name == "valid" && summary != "研究以对照实验检验推理方法，并报告任务表现。" {
				t.Fatalf("summary=%q", summary)
			}
			if tc.name == "uncertain" && summary != "" {
				t.Fatalf("summary=%q", summary)
			}
		})
	}
}

func TestEnhanceSummaryRoutesTypesAndSkipsIneligible(t *testing.T) {
	for _, tc := range []struct {
		category, action, wantType string
		selected                   bool
	}{
		{"产品", "发布", "product_release", true},
		{"论文", "", "paper", true},
		{"教程", "", "tutorial", true},
		{"模型", "发布", "", true},
		{"观点", "", "", true},
		{"产品", "评测", "", true},
		{"论文", "", "", false},
	} {
		t.Run(tc.category+tc.action+tc.wantType, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var request struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if !strings.Contains(request.Messages[1].Content, `"type":"`+tc.wantType+`"`) {
					t.Errorf("unexpected material: %s", request.Messages[1].Content)
				}
				writeCompletion(w, `{"summary":"原文提供了具体做法与适用条件，帮助读者理解内容。"}`)
			}))
			defer server.Close()
			client := llm.New(config.LLM{Enabled: true, BaseURL: server.URL, Model: "fake", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}})
			item := biz.WorkItem{Selected: tc.selected, Body: "具体原文", Writing: &biz.Writing{Title: "资料标题", Summary: "基础摘要内容", Category: tc.category}}
			if tc.action != "" {
				item.Writing.Fact = &biz.Fact{Title: "产品变化", Action: tc.action}
			}
			summary, err := client.EnhanceSummary(t.Context(), item)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantType == "" {
				if calls != 0 || summary != "" {
					t.Fatalf("ineligible calls=%d summary=%q", calls, summary)
				}
			} else if calls != 1 || summary == "" {
				t.Fatalf("calls=%d summary=%q", calls, summary)
			}
		})
	}
}

func TestEnhanceSummaryPreservesInputWriting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeCompletion(w, `{"summary":"研究使用对照实验验证方法，并报告适用条件。"}`)
	}))
	defer server.Close()
	client := llm.New(config.LLM{Enabled: true, BaseURL: server.URL, Model: "fake", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}})
	date := "invalid-date"
	writing := biz.Writing{Title: " 研究标题 ", Summary: " 基础摘要 ", Category: "论文", Tags: []string{"Agent", "unknown", "Agent"}, Subjects: []string{"openai", "unknown", "openai"}, Fact: &biz.Fact{Title: " 研究发现 ", Subject: " 团队 ", Action: " 提出 ", Object: " 方法 ", OccurredAt: &date}}
	before := writing
	before.Tags = append([]string(nil), writing.Tags...)
	before.Subjects = append([]string(nil), writing.Subjects...)
	fact := *writing.Fact
	before.Fact = &fact
	if _, err := client.EnhanceSummary(t.Context(), biz.WorkItem{Selected: true, Writing: &writing}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(writing, before) || date != "invalid-date" {
		t.Fatalf("input writing changed: got=%+v want=%+v", writing, before)
	}
}
