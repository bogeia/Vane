package push

import (
	"fmt"
	"strings"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
)

// Escape model-written text before embedding it in either platform's markdown.
func escapeRealtime(text string) string {
	return strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "~", "\\~", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "#", "\\#", ">", "&gt;", "<", "&lt;", "!", "\\!", "|", "\\|", "\r", " ", "\n", " ").Replace(text)
}

func linkURL(raw string) string {
	return strings.NewReplacer("(", "%28", ")", "%29", "\n", "%0A", "\r", "%0D", "<", "%3C", ">", "%3E").Replace(raw)
}

func (c *Client) RealtimeMessages(name string, items []biz.Item, list bool, reason string) ([]biz.RealtimePart, error) {
	var hook config.Webhook
	found := false
	for _, candidate := range c.cfg.Webhooks {
		if candidate.Name == name {
			hook = candidate
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%s: group not configured", name)
	}
	base := strings.TrimRight(c.site.PublicURL, "/")
	if !list {
		item := biz.CompleteItem(items[0])
		title := item.Title
		if hook.Type == config.WebhookTypeWecom {
			title = escapeRealtime(title)
		}
		content := fmt.Sprintf("%s\n%s · %s\n\n%s\n\n推荐理由：%s\n\n[本站资料](%s/items/%s) · [原文](%s)\n\n%s", escapeRealtime(item.Category), escapeRealtime(item.Source.Name), item.PublishedAt.Format("2006-01-02 15:04"), escapeRealtime(item.Summary), escapeRealtime(item.Reason), base, item.ID, linkURL(item.URL), escapeRealtime(reason))
		message := biz.PushMessage{Title: title, Content: content}
		if _, err := encodeMessage(hook, message); err != nil {
			return nil, fmt.Errorf("%s %s: content exceeds limit", name, hook.Type)
		}
		return []biz.RealtimePart{{Message: message, ItemIDs: []string{item.ID}}}, nil
	}
	heading := c.site.Name + " 实时推送"
	if hook.Type == config.WebhookTypeWecom {
		heading = escapeRealtime(heading)
	}
	var parts []biz.RealtimePart
	var ids []string
	message := biz.PushMessage{Title: heading}
	for _, item := range items {
		line := fmt.Sprintf("- [%s](%s/items/%s)\n", escapeRealtime(biz.CompleteItem(item).Title), base, item.ID)
		next := biz.PushMessage{Title: heading, Content: message.Content + line}
		if _, err := encodeMessage(hook, next); err != nil {
			if message.Content == "" {
				return nil, fmt.Errorf("%s %s: content exceeds limit", name, hook.Type)
			}
			parts = append(parts, biz.RealtimePart{Message: message, ItemIDs: ids})
			ids = nil
			message = biz.PushMessage{Title: heading, Content: line}
			if _, err := encodeMessage(hook, message); err != nil {
				return nil, fmt.Errorf("%s %s: content exceeds limit", name, hook.Type)
			}
		} else {
			message = next
		}
		ids = append(ids, item.ID)
	}
	if message.Content != "" {
		parts = append(parts, biz.RealtimePart{Message: message, ItemIDs: ids})
	}
	return parts, nil
}
