package biz

import (
	"context"
	"time"
)

type SitemapResourceKind string

const (
	SitemapReport SitemapResourceKind = "report"
	SitemapItem   SitemapResourceKind = "item"
)

type SitemapResource struct {
	Kind         SitemapResourceKind
	ID           string
	LastModified time.Time
}

func (i *ItemsUseCase) SitemapPages(ctx context.Context, now time.Time, limit int) ([]SitemapResource, error) {
	pages := make([]SitemapResource, 0)
	if limit <= 0 {
		return pages, nil
	}
	reports, err := i.repo.SitemapReports(ctx, limit)
	if err != nil {
		return nil, err
	}
	pages = append(pages, reports[:min(limit, len(reports))]...)
	remaining := limit - len(pages)
	if remaining == 0 {
		return pages, nil
	}
	items, err := i.repo.SitemapItems(ctx, SitemapQuery{Since: now.Add(-365 * 24 * time.Hour), Until: now, Limit: remaining})
	if err != nil {
		return nil, err
	}
	return append(pages, items[:min(remaining, len(items))]...), nil
}

type SitemapQuery struct {
	Since, Until time.Time
	Limit        int
}
