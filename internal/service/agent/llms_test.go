package agent

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/bogeia/vane/internal/config"
)

func TestAgentLLMSMatchesPublishedMarkdown(t *testing.T) {
	agent := newTestAgentService(&latestAgentItems{}, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	want := `# Vane

> AI 行业中文资讯站：编辑精选、全部动态、热点事件和每天 08:00 出刊的日报。

给 AI 助手：先读[使用说明](https://vane.example.com/api/agent)，按用户的问题选地址；返回中文 Markdown，末尾有回答提示。

## 给 Agent

- [使用说明](https://vane.example.com/api/agent)：能查什么、按问题选哪个地址、怎么回答
- [MCP 服务](https://vane.example.com/api/mcp)：Streamable HTTP，工具与使用说明里的地址一一对应
- [接入页](https://vane.example.com/agent)：给人看的接入方式，可复制 MCP 配置

## 订阅

- [精选 RSS](https://vane.example.com/feed.xml)
- [全部动态 RSS](https://vane.example.com/feed/all.xml)
- [日报 RSS](https://vane.example.com/feed/daily.xml)
- 分类精选 RSS：https://vane.example.com/feed/category/{model,product,industry,paper,tutorial,opinion}.xml
- [信源列表 OPML](https://vane.example.com/sources.opml)

## 程序接口

- [OpenAPI](https://vane.example.com/openapi.json)：网站自用的 JSON 接口，字段可能调整
`
	if diff := cmp.Diff(want, agent.LLMS()); diff != "" {
		t.Fatalf("LLMS() markdown mismatch (-want +got):\n%s", diff)
	}
}

func TestAgentLLMSTakesSiteNameAndPublicURLFromConfiguration(t *testing.T) {
	const name, baseURL = "Northstar", "https://news.example.test/root"
	agent := newTestAgentService(&latestAgentItems{}, config.Site{Name: name, PublicURL: baseURL + "/"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	got := agent.LLMS()
	assert.Contains(t, got, "# "+name+"\n")
	assert.Contains(t, got, "[使用说明]("+baseURL+"/api/agent)")
	assert.Contains(t, got, "[MCP 服务]("+baseURL+"/api/mcp)")
	assert.Contains(t, got, "[接入页]("+baseURL+"/agent)")
	assert.NotContains(t, got, "Vane")
	assert.NotContains(t, got, "https://vane.example.com")
}
