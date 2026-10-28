package agent

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/bogeia/vane/internal/biz"
)

type AgentSearchInput struct {
	Q      string  `json:"q" jsonschema:"搜索关键词，去掉首尾空白后 1–200 个字符，不含 NUL"`
	Window string  `json:"window,omitempty" jsonschema:"时间范围：24h 为过去 24 小时，7d 为最近 7 天；默认 7d"`
	Limit  *int    `json:"limit,omitempty" jsonschema:"返回条数，1–30，默认 10"`
	Topic  *string `json:"topic,omitempty"`
	Mode   *string `json:"mode,omitempty"`
}

func (a *AgentService) Search(ctx context.Context, input AgentSearchInput, transport AgentTransport) AgentAnswer {
	if input.Topic != nil || input.Mode != nil {
		return a.badParameter("搜索不接受 topic 或 mode 参数。", transport)
	}
	q := strings.TrimSpace(input.Q)
	if length := utf8.RuneCountInString(q); length < 1 || length > 200 || strings.ContainsRune(q, '\x00') {
		return a.invalidParameter("q", 30, transport)
	}
	window, windowLabel := biz.Window7d, "最近 7 天"
	switch input.Window {
	case "", "7d":
	case "24h":
		window, windowLabel = biz.Window24h, "过去 24 小时"
	default:
		return a.invalidParameter("window", 30, transport)
	}
	limit := 10
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > 30 {
		return a.invalidParameter("limit", 30, transport)
	}
	result, err := a.items.Search(ctx, biz.SearchQuery{Q: q, Window: window, Limit: limit})
	if err != nil {
		return a.unavailable(ctx, err)
	}
	title := a.site.Name + " 搜索「" + agentText(q) + "」 · " + windowLabel
	if len(result.Items) == 0 {
		hint := "如实告诉用户 " + a.site.Name + " 最近 7 天没有这方面的报道；更早的内容这里查不到。"
		if window == biz.Window24h {
			hint = "如实告诉用户 " + a.site.Name + " 过去 24 小时没有这方面的报道（可以用 window=7d 看最近一周）。"
			if transport == AgentMCP {
				hint = "如实告诉用户 " + a.site.Name + " 过去 24 小时没有这方面的报道（可以把 window 设为 7d 看最近一周）。"
			}
		}
		return agentAnswer(http.StatusOK, title, windowLabel+"的精选和全部动态里都没有相关报道。", "", a.site.Name, hint,
			"可以换个说法或更短的关键词再查一次（比如只用公司或产品名）。", "不要用训练记忆冒充最新消息。")
	}
	description := "以下是 " + a.site.Name + " 精选里的相关报道。"
	hints := []string{"只根据这些结果回答：这是 " + a.site.Name + " 收录的相关报道，不是全网搜索，别说成“全网只有这些”。"}
	if result.Expanded {
		description = "精选里没有，以下来自全部动态（没有进入精选）。"
		hints = append(hints, "告诉用户这些没有进入 "+a.site.Name+" 精选。")
	}
	description += fmt.Sprintf("%d 条，从新到旧，时间为北京时间。", len(result.Items))
	if result.NextCursor != nil {
		if limit < 30 {
			description += "后面还有，调大 limit（最多 30）可以多看。"
		} else {
			description += "后面还有，请换更具体的关键词。"
		}
	}
	hints = append(hints, fmt.Sprintf(agentItemHint, a.site.Name), agentLatestHint, agentTechnicalHint)
	return agentAnswer(http.StatusOK, title, description, a.itemMarkdown(result.Items), a.site.Name, hints...)
}
