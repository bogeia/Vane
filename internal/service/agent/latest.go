package agent

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/biz/taxonomy"
)

type AgentLatestInput struct {
	Window string `json:"window,omitempty" jsonschema:"时间范围：24h 为过去 24 小时，7d 为最近 7 天；默认 24h"`
	Mode   string `json:"mode,omitempty" jsonschema:"selected 为精选，all 为全部动态；默认 selected"`
	Topic  string `json:"topic,omitempty" jsonschema:"按公司、方向或内容形态主题筛选；省略时不筛选"`
	Limit  *int   `json:"limit,omitempty" jsonschema:"返回条数，1–30，默认 10"`
}

func (a *AgentService) Latest(ctx context.Context, input AgentLatestInput, transport AgentTransport) AgentAnswer {
	window, windowLabel := biz.Window24h, "过去 24 小时"
	switch input.Window {
	case "", "24h":
	case "7d":
		window, windowLabel = biz.Window7d, "最近 7 天"
	default:
		return a.invalidParameter("window", 30, transport)
	}
	view, label := biz.ViewSelected, "精选"
	switch input.Mode {
	case "", "selected":
	case "all":
		view, label = biz.ViewAll, "全部动态"
	default:
		return a.invalidParameter("mode", 30, transport)
	}
	title := a.site.Name + " " + label
	if input.Topic != "" {
		if !taxonomy.ValidTopic(input.Topic) {
			return a.invalidParameter("topic", 30, transport)
		}
		for _, topic := range taxonomy.Topics {
			if topic.Slug == input.Topic {
				title += " · " + topic.Name
				break
			}
		}
	}
	limit := 10
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > 30 {
		return a.invalidParameter("limit", 30, transport)
	}
	title += " · " + windowLabel
	page, err := a.items.Latest(ctx, biz.LatestQuery{Window: window, View: view, Topic: input.Topic, Limit: limit})
	if err != nil {
		return a.unavailable(ctx, err)
	}
	if len(page.Items) == 0 {
		hint := "如实告诉用户这段时间没有；可以换成 window=7d 或 mode=all 再查一次。"
		if transport == AgentMCP {
			hint = "如实告诉用户这段时间没有；可以把 window 设为 7d，或把 mode 设为 all，再调用一次 vane_get_latest。"
		}
		return agentAnswer(http.StatusOK, title, windowLabel+"没有符合条件的"+label+"。", "", a.site.Name, hint, "不要用训练记忆补成“最新消息”。")
	}
	description := fmt.Sprintf("%d 条，从新到旧，时间为北京时间。", len(page.Items))
	if page.NextCursor != nil {
		if limit < 30 {
			description += "后面还有，调大 limit（最多 30）可以多看。"
		} else {
			description += "后面还有，范围更大时请缩小到某个主题。"
		}
	}
	return agentAnswer(http.StatusOK, title, description, a.itemMarkdown(page.Items), a.site.Name,
		"先用一两句话概括，再挑最重要的 3–8 条（用户要全部就全列）；保持上面的先后顺序，不要自己排成榜单。", fmt.Sprintf(agentItemHint, a.site.Name), agentLatestHint, agentTechnicalHint)
}
