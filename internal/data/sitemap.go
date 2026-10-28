package data

import (
	"context"
	"strconv"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

func (r *itemsRepo) SitemapReports(ctx context.Context, limit int) ([]biz.SitemapResource, error) {
	var reports []struct {
		Date      string
		CreatedAt time.Time
	}
	if err := r.data.ORM.WithContext(ctx).Model(&reportModel{}).Select("date::text,created_at").Order("date DESC").Limit(limit).Scan(&reports).Error; err != nil {
		return nil, err
	}
	resources := make([]biz.SitemapResource, 0, len(reports))
	for _, report := range reports {
		resources = append(resources, biz.SitemapResource{Kind: biz.SitemapReport, ID: report.Date, LastModified: report.CreatedAt})
	}
	return resources, nil
}

func (r *itemsRepo) SitemapItems(ctx context.Context, query biz.SitemapQuery) ([]biz.SitemapResource, error) {
	var items []struct {
		ID          int64
		ProcessedAt time.Time
	}
	if err := r.data.ORM.WithContext(ctx).Model(&itemModel{}).Select("id,processed_at").Where("stage = ? AND selected AND NOT roundup AND processed_at >= ? AND processed_at <= ?", biz.StageDone, query.Since, query.Until).Order("processed_at DESC,id DESC").Limit(query.Limit).Scan(&items).Error; err != nil {
		return nil, err
	}
	resources := make([]biz.SitemapResource, 0, len(items))
	for _, item := range items {
		resources = append(resources, biz.SitemapResource{Kind: biz.SitemapItem, ID: strconv.FormatInt(item.ID, 10), LastModified: item.ProcessedAt})
	}
	return resources, nil
}
