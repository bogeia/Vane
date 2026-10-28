package data

import (
	"context"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/bogeia/vane/internal/biz"
)

type realtimeRepo struct{ data *Data }

func NewRealtimeRepo(data *Data) biz.RealtimeRepository { return &realtimeRepo{data: data} }
func (r *realtimeRepo) SyncTargets(ctx context.Context, names []string, now time.Time) error {
	return r.data.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, name := range names {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&pushTargetModel{Name: name, FirstSeenAt: now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *realtimeRepo) Target(ctx context.Context, name string) (biz.PushTarget, error) {
	var row pushTargetModel
	err := r.data.ORM.WithContext(ctx).Where("name = ?", name).First(&row).Error
	return biz.PushTarget{Name: row.Name, FirstSeenAt: row.FirstSeenAt}, err
}

func (r *realtimeRepo) Candidates(ctx context.Context, q biz.RealtimeQuery) ([]biz.RealtimeCandidate, error) {
	var rows []publicItemRow
	err := publicItems(r.data.ORM.WithContext(ctx)).Where("i.processed_at >= ? AND i.published_at >= ? AND i.published_at <= ?", q.ProcessedSince, q.PublishedSince, q.Until).Order("i.processed_at,i.id").Scan(&rows).Error
	result := make([]biz.RealtimeCandidate, 0, len(rows))
	for _, row := range rows {
		result = append(result, biz.RealtimeCandidate{Item: row.item(), ProcessedAt: *row.Item.ProcessedAt})
	}
	return result, err
}

func (r *realtimeRepo) Deliveries(ctx context.Context, name string) ([]biz.PushDelivery, error) {
	var rows []pushDeliveryModel
	if err := r.data.ORM.WithContext(ctx).Where("target_name = ?", name).Order("created_at,item_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.Status == biz.DeliveryFailed {
			ids = append(ids, row.ItemID)
		}
	}
	var itemRows []publicItemRow
	if len(ids) > 0 {
		if err := publicItems(r.data.ORM.WithContext(ctx)).Where("i.id IN ?", ids).Scan(&itemRows).Error; err != nil {
			return nil, err
		}
	}
	byID := map[int64]publicItemRow{}
	for _, row := range itemRows {
		byID[row.Item.ID] = row
	}
	result := make([]biz.PushDelivery, 0, len(rows))
	for _, row := range rows {
		delivery := biz.PushDelivery{Target: row.TargetName, StoryID: strconv.FormatInt(row.StoryID, 10), Status: row.Status, Attempts: row.Attempts, NextAttemptAt: row.NextAttemptAt, LastError: row.LastError, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
		if item, ok := byID[row.ItemID]; ok {
			delivery.Item = item.item()
			delivery.ProcessedAt = *item.Item.ProcessedAt
		}
		result = append(result, delivery)
	}
	return result, nil
}

func (r *realtimeRepo) SaveDeliveries(ctx context.Context, deliveries []biz.PushDelivery) error {
	return r.data.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, delivery := range deliveries {
			storyID, err := strconv.ParseInt(delivery.StoryID, 10, 64)
			if err != nil {
				return err
			}
			itemID, err := strconv.ParseInt(delivery.Item.ID, 10, 64)
			if err != nil {
				return err
			}
			row := pushDeliveryModel{TargetName: delivery.Target, StoryID: storyID, ItemID: itemID, Status: delivery.Status, Attempts: delivery.Attempts, NextAttemptAt: delivery.NextAttemptAt, LastError: delivery.LastError, CreatedAt: delivery.CreatedAt, UpdatedAt: delivery.UpdatedAt}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "target_name"}, {Name: "story_id"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "attempts", "next_attempt_at", "last_error", "updated_at"})}).Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
