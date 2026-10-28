package push

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
)

func TestRealtimeListSplitsAtPlatformLimitsAndEscapesModelText(t *testing.T) {
	for _, platform := range []string{config.WebhookTypeWecom, config.WebhookTypeFeishu} {
		t.Run(platform, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, _ := io.ReadAll(r.Body)
				var payload struct {
					MsgType  string `json:"msgtype"`
					Markdown struct {
						Content string `json:"content"`
					} `json:"markdown"`
				}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Error(err)
				}
				if platform == config.WebhookTypeWecom && len(payload.Markdown.Content) > wecomMarkdownLimit {
					t.Error("wecom part over limit")
				}
				if platform == config.WebhookTypeFeishu && len(body) > feishuRequestLimit {
					t.Error("feishu part over limit")
				}
				if platform == config.WebhookTypeWecom {
					_, _ = io.WriteString(w, `{"errcode":0}`)
				} else {
					_, _ = io.WriteString(w, `{"code":0}`)
				}
			}))
			defer server.Close()
			client := New(config.Push{RequestTimeout: time.Second, Webhooks: []config.Webhook{{Name: "test", Type: platform, URL: server.URL}}}, config.Site{Name: "Vane", PublicURL: "https://vane.test"})
			items := make([]biz.Item, 50)
			for index := range items {
				items[index] = biz.Item{ID: fmt.Sprint(index), Title: strings.Repeat("中文", 80) + " [伪链接](https://bad.test) **粗体** <at id=all>"}
			}
			parts, err := client.RealtimeMessages("test", items, true, "精选")
			if err != nil {
				t.Fatal(err)
			}
			if len(parts) < 2 {
				t.Fatal("long list was not split")
			}
			content := ""
			for _, part := range parts {
				content += part.Message.Content
			}
			for _, want := range []string{`\[伪链接\]\(https://bad.test\)`, `\*\*粗体\*\*`, `&lt;at id=all&gt;`} {
				if !strings.Contains(content, want) {
					t.Errorf("missing escaped %q", want)
				}
			}
			for index := range items {
				if count := strings.Count(content, fmt.Sprintf("https://vane.test/items/%d)", index)); count != 1 {
					t.Errorf("item %d occurred %d times", index, count)
				}
			}
			messages := make([]biz.PushMessage, len(parts))
			for i, part := range parts {
				messages[i] = part.Message
			}
			if err := client.Send(t.Context(), "test", messages); err != nil {
				t.Fatal(err)
			}
			if calls != len(parts) {
				t.Fatalf("sent %d of %d parts", calls, len(parts))
			}
		})
	}
}
