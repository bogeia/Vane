package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bogeia/vane/internal/biz"
)

type AgentStoryInput struct {
	ID    string
	Limit *int
}

func (a *AgentService) Story(ctx context.Context, input AgentStoryInput, transport AgentTransport) AgentAnswer {
	id, err := strconv.ParseInt(input.ID, 10, 64)
	if err != nil || id <= 0 {
		return a.invalidParameter("story_id", 50, transport)
	}
	limit := 20
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > 50 {
		return a.invalidParameter("limit", 50, transport)
	}
	story, err := a.stories.Get(ctx, id)
	if errors.Is(err, biz.ErrStoryNotFound) {
		return a.storyNotFound()
	}
	if err != nil {
		return a.unavailable(ctx, err)
	}
	if len(story.Items) == 0 {
		return a.storyNotFound()
	}
	sources := map[string]bool{}
	for _, item := range story.Items {
		sources[item.Source.ID] = true
	}
	total := len(story.Items)
	latest := story.Items[total-1]
	description := fmt.Sprintf("%d 篇报道 · %d 家信源 · 首次报道 %s（北京时间）\n事件页：%s/stories/%s", total, len(sources), a.timeText(story.Items[0].PublishedAt), a.site.PublicURL, story.ID)
	external := fmt.Sprintf("最新进展（%s）：[%s](%s/items/%s)", a.timeText(latest.PublishedAt), agentText(latest.Title), a.site.PublicURL, latest.ID)
	if latest.Summary != "" {
		external += "\n摘要：" + agentText(latest.Summary)
	}
	external += "\n\n报道时间线（从新到旧）：\n"
	entries := make([]string, 0, min(total, limit))
	for index := range min(total, limit) {
		item := story.Items[total-1-index]
		entries = append(entries, fmt.Sprintf("%d. %s · %s · [%s](%s/items/%s)", index+1, a.timeText(item.PublishedAt), agentText(item.Source.Name), agentText(item.Title), a.site.PublicURL, item.ID))
	}
	external += strings.Join(entries, "\n")
	hints := []string{"先讲最新进展，再按时间讲清来龙去脉。"}
	if total > limit {
		action := "加 limit"
		if transport == AgentMCP {
			action = "调大 limit"
		}
		hints = append(hints, fmt.Sprintf("时间线只列了最新 %d 篇，共 %d 篇；要看更多%s（最多 50）。", limit, total, action))
	}
	hints = append(hints, agentTechnicalHint)
	return agentAnswer(http.StatusOK, a.site.Name+" 事件："+agentText(story.Title), description, external, a.site.Name, hints...)
}

func (a *AgentService) storyNotFound() AgentAnswer {
	return agentAnswer(http.StatusNotFound, a.site.Name+" 事件", "没有找到这个事件。", "", a.site.Name, "如实告诉用户没找到；事件编号要从热点榜结果里取，不要猜。")
}
