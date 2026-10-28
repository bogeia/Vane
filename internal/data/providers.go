package data

import (
	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/fetch"
	"github.com/bogeia/vane/internal/infra/llm"
	"github.com/bogeia/vane/internal/infra/push"
)

func NewWriter(cfg config.Config) *llm.Client      { return llm.New(cfg.LLM) }
func NewPushClient(cfg config.Config) *push.Client { return push.New(cfg.Push, cfg.Site) }

func NewPusher(cfg config.Config, client *push.Client) biz.ReportPusher {
	for _, webhook := range cfg.Push.Webhooks {
		if webhook.Daily {
			return client
		}
	}
	return nil
}

func NewProductionFeedFetcher(cfg config.Config) *FeedFetcher {
	selectors := make(map[string]WebListSelectors)
	for _, source := range cfg.Sources {
		if w := source.WebList; w != nil {
			selectors[source.ID] = WebListSelectors{Item: w.Item, Link: w.Link, Title: w.Title, Date: w.Date}
		}
	}
	return NewFeedFetcher(fetch.New(false, cfg.Fetch), selectors)
}
