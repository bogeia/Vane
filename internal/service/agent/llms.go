package agent

import (
	"strings"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

func (a *AgentService) LLMS() string {
	const text = `# {name}

> AI 行业中文资讯站：编辑精选、全部动态、热点事件和每天 08:00 出刊的日报。

给 AI 助手：先读[使用说明]({url}/api/agent)，按用户的问题选地址；返回中文 Markdown，末尾有回答提示。

## 给 Agent

- [使用说明]({url}/api/agent)：能查什么、按问题选哪个地址、怎么回答
- [MCP 服务]({url}/api/mcp)：Streamable HTTP，工具与使用说明里的地址一一对应
- [接入页]({url}/agent)：给人看的接入方式，可复制 MCP 配置

## 订阅

- [精选 RSS]({url}/feed.xml)
- [全部动态 RSS]({url}/feed/all.xml)
- [日报 RSS]({url}/feed/daily.xml)
- 分类精选 RSS：{url}/feed/category/{categories}.xml
- [信源列表 OPML]({url}/sources.opml)

## 程序接口

- [OpenAPI]({url}/openapi.json)：网站自用的 JSON 接口，字段可能调整
`
	categories := make([]string, 0)
	for _, topic := range taxonomy.Topics {
		if topic.Category != "" {
			categories = append(categories, topic.Slug)
		}
	}
	return strings.NewReplacer("{name}", a.site.Name, "{url}", a.site.PublicURL, "{categories}", "{"+strings.Join(categories, ",")+"}").Replace(text)
}
