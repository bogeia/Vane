package agent

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/bogeia/vane/internal/biz"
)

type AgentHotInput struct {
	Limit *int `json:"limit,omitempty" jsonschema:"返回热点名次的数量，1–20，默认 10"`
}

func (a *AgentService) Hot(ctx context.Context, input AgentHotInput, transport AgentTransport) AgentAnswer {
	limit := 10
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > 20 {
		return a.invalidParameter("limit", 20, transport)
	}
	stories, err := a.hot.List(ctx)
	if err != nil {
		return a.unavailable(ctx, err)
	}
	count := min(limit, len(stories))
	title := fmt.Sprintf("%s 当前热点 Top %d", a.site.Name, count)
	if count == 0 {
		return agentAnswer(http.StatusOK, title, "热点榜暂时是空的。", "", a.site.Name, "如实告诉用户最近 48 小时还没有至少两家信源同时关注的事件，可以改看最新精选。")
	}
	entries := make([]string, 0, count)
	for index, story := range stories[:count] {
		names := make([]string, 0)
		seen := map[string]bool{}
		for _, source := range story.Sources {
			if !seen[source.Name] {
				names = append(names, agentText(source.Name))
				seen[source.Name] = true
			}
		}
		sources := strings.Join(names[:min(6, len(names))], "、")
		if len(names) > 6 {
			sources += " 等"
		}
		entry := fmt.Sprintf("第 %d 名：[%s](%s/stories/%s)\n   信源：%s（%d 家）", index+1, agentText(story.Title), a.site.PublicURL, story.StoryID, sources, len(names))
		switch story.Trend {
		case biz.TrendNew:
			entry += "· 新上榜"
		case biz.TrendRising:
			entry += "· 上升"
		}
		next := a.site.PublicURL + "/api/agent/stories/" + story.StoryID
		if transport == AgentMCP {
			next = "vane_get_story，story_id=" + story.StoryID
		}
		entries = append(entries, entry+"\n   来龙去脉："+next)
	}
	hint := "用户追问某个事件的来龙去脉、时间线或最新进展时，请求它的「来龙去脉」地址；不要自己拼地址。"
	if transport == AgentMCP {
		hint = "用户追问某个事件的来龙去脉、时间线或最新进展时，用 vane_get_story 和上面给出的 story_id；不要猜。"
	}
	return agentAnswer(http.StatusOK, title, "多个独立信源正在同时讨论的事件，按名次排列。", strings.Join(entries, "\n\n"), a.site.Name,
		"按名次完整列出，写「第 N 名」；不要说热度分数，也不要把信源数量说成热度。", hint, agentTechnicalHint)
}
