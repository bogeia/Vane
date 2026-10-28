package data

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

func (f *FeedFetcher) hfModels(ctx context.Context, source biz.CollectSource) (biz.Feed, error) {
	organization, err := url.Parse(source.FeedURL)
	if err != nil {
		return biz.Feed{}, fmt.Errorf("invalid HF organization URL: %w", err)
	}
	query := url.Values{"author": {strings.TrimPrefix(organization.Path, "/")}, "sort": {"createdAt"}, "direction": {"-1"}, "limit": {"50"}}
	endpoint := organization.ResolveReference(&url.URL{Path: "/api/models", RawQuery: query.Encode()})
	response, err := f.client.Get(ctx, endpoint.String(), nil)
	if err != nil {
		return biz.Feed{}, err
	}
	var models []struct {
		ID        string     `json:"id"`
		CreatedAt *time.Time `json:"createdAt"`
	}
	body := bytes.TrimSpace(response.Body)
	if len(body) == 0 || body[0] != '[' {
		return biz.Feed{}, fmt.Errorf("%w: hf models: expected array", biz.ErrFeedParse)
	}
	if err := json.Unmarshal(body, &models); err != nil {
		return biz.Feed{}, fmt.Errorf("%w: hf models: invalid response: %w", biz.ErrFeedParse, err)
	}
	result := biz.Feed{URL: source.FeedURL}
	for _, model := range models {
		if strings.TrimSpace(model.ID) == "" || model.CreatedAt == nil {
			return biz.Feed{}, fmt.Errorf("%w: hf models: missing id or createdAt", biz.ErrFeedParse)
		}
		modelURL := organization.ResolveReference(&url.URL{Path: "/" + model.ID})
		result.Items = append(result.Items, biz.FeedItem{URL: modelURL.String(), Title: model.ID, Published: model.CreatedAt})
	}
	return result, nil
}

func hfModelCard(body string) (string, error) {
	body = strings.TrimSpace(body)
	first, rest, hasNewline := strings.Cut(body, "\n")
	if strings.TrimSpace(first) != "---" {
		return body, nil
	}
	for hasNewline {
		var line string
		line, rest, hasNewline = strings.Cut(rest, "\n")
		if strings.TrimSpace(line) == "---" {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("hf model card: unterminated metadata")
}
