package push

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
)

const (
	wecomMarkdownLimit = 4096
	feishuRequestLimit = 20 * 1024
)

type Client struct {
	cfg  config.Push
	site config.Site
	http *http.Client
}

func New(cfg config.Push, site config.Site) *Client {
	return &Client{cfg, site, &http.Client{Timeout: cfg.RequestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: true}}}
}

func (c *Client) Push(ctx context.Context, report biz.Report) error {
	var failures error
	daily := false
	for _, webhook := range c.cfg.Webhooks {
		if !webhook.Daily {
			continue
		}
		daily = true
		parts, err := c.messages(report, webhook)
		if err == nil {
			err = c.Send(ctx, webhook.Name, parts)
		}
		failures = errors.Join(failures, err)
	}
	if !daily {
		return biz.ErrNoWebhooks
	}
	return failures
}

// Send sends messages to one configured group, stopping at its first failed part.
func (c *Client) Send(ctx context.Context, name string, messages []biz.PushMessage) error {
	for _, webhook := range c.cfg.Webhooks {
		if webhook.Name != name {
			continue
		}
		if webhook.Type != config.WebhookTypeWecom && webhook.Type != config.WebhookTypeFeishu {
			return fmt.Errorf("%s %s part 1: unsupported platform", webhook.Name, webhook.Type)
		}
		for i, content := range messages {
			if err := c.send(ctx, webhook, content, i+1); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("%s: group not configured", name)
}

func (c *Client) messages(report biz.Report, webhook config.Webhook) ([]biz.PushMessage, error) {
	var items []biz.Item
	if report.Headline != nil {
		items = append(items, *report.Headline)
	}
	items = append(items, report.Highlights...)
	for _, section := range report.Sections {
		items = append(items, section.Items...)
	}
	heading := fmt.Sprintf("%s 日报 · %s", c.site.Name, report.Date)
	var lines []string
	seen := map[string]bool{}
	escape := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "\n", " ", "\r", " ")
	base := strings.TrimRight(c.site.PublicURL, "/")
	for _, item := range items {
		if seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		lines = append(lines, fmt.Sprintf("- [%s](%s/items/%s)", escape.Replace(item.Title), base, item.ID))
	}
	footer := fmt.Sprintf("[查看整期日报](%s/reports/%s)", base, report.Date)
	render := func(title string, items []string) biz.PushMessage {
		return biz.PushMessage{Title: title, Content: strings.Join(items, "\n") + "\n\n" + footer}
	}
	fits := func(part biz.PushMessage) bool { _, err := encodeMessage(webhook, part); return err == nil }
	if single := render(heading, lines); fits(single) {
		return []biz.PushMessage{single}, nil
	}
	reservedHeading := heading + fmt.Sprintf("（%d/%d）", len(lines), len(lines))
	var groups [][]string
	var group []string
	for _, line := range lines {
		if !fits(render(reservedHeading, []string{line})) {
			return nil, fmt.Errorf("%s %s part 1: content exceeds limit", webhook.Name, webhook.Type)
		}
		if len(group) > 0 && !fits(render(reservedHeading, append(group, line))) {
			groups = append(groups, group)
			group = nil
		}
		group = append(group, line)
	}
	if len(group) > 0 {
		groups = append(groups, group)
	}
	parts := make([]biz.PushMessage, len(groups))
	for i, group := range groups {
		parts[i] = render(heading+fmt.Sprintf("（%d/%d）", i+1, len(groups)), group)
	}
	return parts, nil
}

func encodeMessage(webhook config.Webhook, part biz.PushMessage) ([]byte, error) {
	var payload map[string]any
	if webhook.Type == config.WebhookTypeWecom {
		content := part.Title + "\n\n" + part.Content
		if len(content) > wecomMarkdownLimit {
			return nil, errors.New("content exceeds limit")
		}
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": content}}
	} else {
		payload = map[string]any{
			"msg_type": "interactive",
			"card": map[string]any{
				"schema": "2.0",
				"header": map[string]any{"title": map[string]string{"tag": "plain_text", "content": part.Title}},
				"body":   map[string]any{"elements": []map[string]string{{"tag": "markdown", "content": part.Content}}},
			},
		}
		if webhook.Secret != "" {
			timestamp := strconv.FormatInt(time.Now().Unix(), 10)
			// Feishu signs an empty message with timestamp and secret as the HMAC key.
			digest := hmac.New(sha256.New, []byte(timestamp+"\n"+webhook.Secret))
			payload["timestamp"] = timestamp
			payload["sign"] = base64.StdEncoding.EncodeToString(digest.Sum(nil))
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("invalid message")
	}
	if webhook.Type == config.WebhookTypeFeishu && len(body) > feishuRequestLimit {
		return nil, errors.New("content exceeds limit")
	}
	return body, nil
}

func (c *Client) send(ctx context.Context, webhook config.Webhook, content biz.PushMessage, part int) error {
	body, err := encodeMessage(webhook, content)
	if err != nil {
		return fmt.Errorf("%s %s part %d: %w", webhook.Name, webhook.Type, part, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s %s part %d: invalid request", webhook.Name, webhook.Type, part)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s part %d: request failed", webhook.Name, webhook.Type, part)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s part %d: HTTP %d", webhook.Name, webhook.Type, part, response.StatusCode)
	}
	var result struct {
		Errcode *int `json:"errcode"`
		Code    *int `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("%s %s part %d: invalid response", webhook.Name, webhook.Type, part)
	}
	code := result.Errcode
	if webhook.Type == config.WebhookTypeFeishu {
		code = result.Code
	}
	if code == nil {
		return fmt.Errorf("%s %s part %d: invalid response", webhook.Name, webhook.Type, part)
	}
	if *code != 0 {
		return fmt.Errorf("%s %s part %d: business code %d", webhook.Name, webhook.Type, part, *code)
	}
	return nil
}
