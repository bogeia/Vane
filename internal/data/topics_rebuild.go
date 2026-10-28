package data

import (
	"context"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/bogeia/vane/internal/biz"
)

type topicsRepo struct{ data *Data }

func NewTopicsRepo(data *Data) biz.TopicsRepository { return &topicsRepo{data: data} }

func (r *topicsRepo) Rebuild(ctx context.Context, assign func(biz.TopicItem) []string) (biz.TopicsRebuildResult, error) {
	var result biz.TopicsRebuildResult
	var after int64
	for {
		var batch []itemModel
		var updated int64
		err := r.data.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Select("id", "title", "original_title", "subjects", "tags", "category", "topics").Where("stage = ? AND id > ?", biz.StageDone, after).Order("id").Limit(100).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&batch).Error; err != nil {
				return err
			}
			for _, item := range batch {
				topics := assign(biz.TopicItem{Title: item.Title, OriginalTitle: item.OriginalTitle, Subjects: []string(item.Subjects), Tags: []string(item.Tags), Category: item.Category})
				if slices.Equal([]string(item.Topics), topics) {
					continue
				}
				if err := tx.Model(&itemModel{}).Where("id = ?", item.ID).UpdateColumn("topics", textArray(topics)).Error; err != nil {
					return err
				}
				updated++
			}
			return nil
		})
		if err != nil {
			return result, err
		}
		result.Checked += int64(len(batch))
		result.Updated += updated
		if len(batch) < 100 {
			return result, nil
		}
		after = batch[len(batch)-1].ID
	}
}
