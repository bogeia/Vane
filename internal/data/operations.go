package data

import (
	"context"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

type operationsRepo struct{ data *Data }

type sourceStatusRecord struct {
	ID              string
	Name            string
	Tier            string
	Enabled         bool
	LastError       string
	ItemCount       int64
	LastCollectedAt *time.Time
}

type failedItemRecord struct {
	ID            string
	OriginalTitle string
	Attempts      int
	LastError     string
}

func NewOperationsRepo(data *Data) biz.OperationsRepository {
	return &operationsRepo{data: data}
}

func (r *operationsRepo) Ping(ctx context.Context) error {
	db, err := r.data.ORM.DB()
	if err != nil {
		return err
	}
	return db.PingContext(ctx)
}

func (r *operationsRepo) Status(ctx context.Context, query biz.StatusQuery) (biz.Status, error) {
	status := biz.Status{RecentBlocked: []biz.BlockedContent{}, Sources: []biz.SourceStatus{}, FailedItems: []biz.FailedItem{}, Stages: map[string]int64{}}
	db := r.data.ORM.WithContext(ctx)
	var sources []sourceStatusRecord
	if err := db.Table("sources s").Select("s.id,s.name,s.tier,s.enabled,s.last_error,s.last_collected_at,(SELECT count(*) FROM items i WHERE i.source_id=s.id) AS item_count").Order("s.id").Scan(&sources).Error; err != nil {
		return status, err
	}
	for _, row := range sources {
		status.Sources = append(status.Sources, row.domain())
	}
	health, err := r.sourceHealth(ctx, query)
	if err != nil {
		return status, err
	}
	for i := range status.Sources {
		status.Sources[i].Health = health[status.Sources[i].ID]
	}
	var blocked []blockedContentModel
	blockedQuery := db.Order("url").Limit(query.RecentBlockedLimit)
	if query.RecentBlockedNewestFirst {
		blockedQuery = db.Order("first_blocked_at DESC").Order("url").Limit(query.RecentBlockedLimit)
	}
	if err := blockedQuery.Find(&blocked).Error; err != nil {
		return status, err
	}
	for _, row := range blocked {
		status.RecentBlocked = append(status.RecentBlocked, biz.BlockedContent{SourceID: row.SourceID, OriginalTitle: row.OriginalTitle, URL: row.URL, FirstBlockedAt: row.FirstBlockedAt})
	}
	var stages []struct {
		Stage string
		Count int64
	}
	if err := db.Model(&itemModel{}).Select("stage,count(*) AS count").Group("stage").Scan(&stages).Error; err != nil {
		return status, err
	}
	for _, stage := range stages {
		status.Stages[stage.Stage] = stage.Count
	}
	var failed []failedItemRecord
	err = db.Model(&itemModel{}).Select("id::text,original_title,attempts,last_error").Where("stage = ?", biz.StageFailed).Order("id").Scan(&failed).Error
	for _, row := range failed {
		status.FailedItems = append(status.FailedItems, row.domain())
	}
	return status, err
}

func (r sourceStatusRecord) domain() biz.SourceStatus {
	return biz.SourceStatus{ID: r.ID, Name: r.Name, Tier: r.Tier, Enabled: r.Enabled, LastError: r.LastError, ItemCount: r.ItemCount, LastCollectedAt: r.LastCollectedAt}
}

func (r failedItemRecord) domain() biz.FailedItem {
	return biz.FailedItem{ID: r.ID, OriginalTitle: r.OriginalTitle, Attempts: r.Attempts, LastError: r.LastError}
}
