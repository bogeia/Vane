package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gorilla/feeds"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/biz/taxonomy"
	"github.com/bogeia/vane/internal/config"
)

func (i *ItemsService) AllFeed(ctx context.Context, site config.Site) ([]byte, error) {
	return i.feed(ctx, site, false, "")
}

func (i *ItemsService) SelectedFeed(ctx context.Context, site config.Site) ([]byte, error) {
	return i.feed(ctx, site, true, "")
}

var ErrCategoryFeedNotFound = errors.New("category feed not found")

func (i *ItemsService) CategoryFeed(ctx context.Context, site config.Site, filename string) ([]byte, error) {
	if slug, ok := strings.CutSuffix(filename, ".xml"); ok {
		for _, topic := range taxonomy.Topics {
			if topic.Slug == slug && topic.Category != "" {
				return i.feed(ctx, site, true, topic.Category)
			}
		}
	}
	return nil, ErrCategoryFeedNotFound
}

func (i *ItemsService) feed(ctx context.Context, site config.Site, selected bool, category string) ([]byte, error) {
	view, label := biz.ViewAll, "全部动态"
	if selected {
		view, label = biz.ViewSelected, "精选"
	}
	page, err := i.items.List(ctx, nil, 100, biz.ItemFilter{View: view, Category: category})
	if err != nil {
		return nil, err
	}
	if category != "" {
		label += " · " + category
	}
	document := feeds.RssFeed{Title: site.Name + " · " + label, Link: site.PublicURL, Description: "AI 行业" + label, Language: "zh-CN"}
	for _, item := range page.Items {
		description := item.Summary
		if item.Reason != "" {
			description += "\n\n" + item.Reason
		}
		guid := strings.TrimRight(site.PublicURL, "/") + "/items/" + item.ID
		if selected && item.StoryID != "" {
			guid = strings.TrimRight(site.PublicURL, "/") + "/stories/" + item.StoryID
		}
		document.Items = append(document.Items, &feeds.RssItem{Title: item.Title, Link: item.URL, Description: description, PubDate: item.PublishedAt.Format(time.RFC1123Z), Guid: &feeds.RssGuid{Id: guid, IsPermaLink: "false"}})
	}
	body, err := feeds.ToXML(&document)
	if err != nil {
		return nil, err
	}
	return []byte(body), nil
}
