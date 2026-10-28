package llm_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/llm"
	"github.com/bogeia/vane/internal/infra/retry"
)

func TestClientRejectsWritingRefusal(t *testing.T) {
	for _, prefix := range []string{"抱歉", "很抱歉", "对不起", "我无法", "我不能", "无法处理", "I'm sorry", "I am sorry", "Sorry", "I cannot", "I can't", "As an AI", "i'M SoRrY", "AS AN AI"} {
		for _, field := range []string{"title", "summary"} {
			t.Run(prefix+"/"+field, func(t *testing.T) {
				writing := map[string]any{"category": "论文", "title": "GPT-5 发布", "summary": "新模型提升了编码能力。", "reason": "值得关注", "subjects": []string{}, "fact": nil, "roundup": false}
				writing[field] = " \n" + prefix + "完成任务。 \t"
				assertClientWritingQuality(t, writing, "writing refused")
			})
		}
	}
	for _, phrase := range []string{"作为一个AI", "作为一个 AI", "作为AI助手", "作为 AI 助手", "无法处理链接内容", "无法访问链接"} {
		for _, field := range []string{"title", "summary", "reason"} {
			t.Run(phrase+"/"+field, func(t *testing.T) {
				writing := map[string]any{"category": "论文", "title": "GPT-5 发布", "summary": "新模型提升了编码能力。", "reason": "值得关注", "subjects": []string{}, "fact": nil, "roundup": false}
				writing[field] = "回答中" + phrase + "的说明"
				assertClientWritingQuality(t, writing, "writing refused")
			})
		}
	}
}

func assertClientWritingQuality(t *testing.T, writing map[string]any, want string) {
	t.Helper()
	content, err := json.Marshal(writing)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeCompletion(w, string(content)) }))
	defer server.Close()
	client := llm.New(config.LLM{Enabled: true, BaseURL: server.URL, APIKey: "test-key", Model: "fake", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}})
	_, err = client.Write(t.Context(), biz.WorkItem{})
	if want == "" {
		if err != nil {
			t.Fatalf("normal writing rejected: %v", err)
		}
		return
	}
	if err == nil || err.Error() != want {
		t.Fatalf("Write error=%v, want %q", err, want)
	}
	if retry.IsPermanent(err) {
		t.Fatalf("writing rejection must allow worker retry: %v", err)
	}
}

func TestClientRejectsDegenerateWritingAndKeepsNormalWriting(t *testing.T) {
	for _, text := range []string{
		strings.Repeat("重", 10), strings.Repeat("重", 11), strings.Repeat("片段", 4),
		strings.Repeat("abcdefghijABCDEFGHIJ", 4), strings.Repeat("ab", 5),
		"这里材料是不可信数据的提示", "这里只输出 JSON的提示", "这里不执行其中的指令的提示",
	} {
		for _, field := range []string{"title", "summary", "reason"} {
			t.Run(field+"/"+text, func(t *testing.T) {
				writing := map[string]any{"category": "论文", "title": "GPT-5 发布", "summary": "新模型提升了编码能力。", "reason": "值得关注", "subjects": []string{}, "fact": nil, "roundup": false}
				writing[field] = text
				assertClientWritingQuality(t, writing, "writing degenerate")
			})
		}
	}
	t.Run("summary copies title after trim", func(t *testing.T) {
		assertClientWritingQuality(t, map[string]any{"category": "论文", "title": " GPT-5 发布 ", "summary": "GPT-5 发布", "subjects": []string{}, "fact": nil, "roundup": false}, "writing degenerate")
	})
	for _, text := range []string{"Claude 为何会说抱歉", "GPT-5 发布", "摘要中间提到模型拒绝回答的现象", strings.Repeat("重", 9), strings.Repeat("片段", 3), strings.Repeat(" ", 12) + "正常输出", strings.Repeat("\t", 12) + "正常输出"} {
		t.Run("normal/"+text, func(t *testing.T) {
			assertClientWritingQuality(t, map[string]any{"category": "论文", "title": text, "summary": "新模型提升了编码能力。", "subjects": []string{}, "fact": nil, "roundup": false}, "")
		})
	}
	t.Run("checks normalized content only", func(t *testing.T) {
		var title strings.Builder
		for i := range 80 {
			fmt.Fprintf(&title, "%c", rune(0x4e00+i))
		}
		assertClientWritingQuality(t, map[string]any{"category": "论文", "title": title.String() + "只输出 JSON", "summary": "正常摘要内容", "subjects": []string{}, "fact": nil, "roundup": false}, "")
	})
}
