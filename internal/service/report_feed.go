package service

import (
	"context"
	"strings"
	"time"

	"github.com/gorilla/feeds"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
)

func (r *ReportsService) DailyFeed(ctx context.Context, site config.Site) ([]byte, error) {
	summaries, err := r.reports.List(ctx)
	if err != nil {
		return nil, err
	}
	document := feeds.RssFeed{Title: site.Name + " · 日报", Link: site.PublicURL, Description: "AI 行业日报", Language: "zh-CN"}
	for _, summary := range summaries {
		report, err := r.reports.Get(ctx, summary.Date)
		if err != nil {
			return nil, err
		}
		_, published, err := biz.ReportWindow(report.Date)
		if err != nil {
			return nil, err
		}
		description := "本期暂无符合条件的资料。"
		if report.Headline != nil {
			description = report.Headline.Title + "\n" + report.Headline.Summary
		}
		for _, item := range report.Highlights {
			description += "\n" + item.Title
		}
		link := strings.TrimRight(site.PublicURL, "/") + "/reports/" + report.Date
		document.Items = append(document.Items, &feeds.RssItem{Title: site.Name + " 日报 · " + report.Date, Link: link, Description: description, PubDate: published.Format(time.RFC1123Z), Guid: &feeds.RssGuid{Id: link, IsPermaLink: "true"}})
	}
	body, err := feeds.ToXML(&document)
	if err != nil {
		return nil, err
	}
	return []byte(body), nil
}
