package data

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/bogeia/vane/internal/biz"
)

type storyMemberRecord struct {
	StoryID int64
	Title   string
	Summary string
}

func (r *workRepo) Group(ctx context.Context, item biz.WorkItem, now time.Time, plan biz.GroupingPlan) error {
	changed := errors.New("group candidates changed")
	recall := func(db *gorm.DB) ([]biz.StoryCandidate, error) {
		members := make([]biz.StoryMember, 0)
		var rows []storyMemberRecord
		err := db.Model(&itemModel{}).Select("story_id,title,summary").Where("stage = ? AND NOT roundup AND story_id IS NOT NULL AND published_at >= ? AND published_at <= ?", biz.StageDone, plan.Since, plan.Until).Order("story_id,id").Scan(&rows).Error
		for _, row := range rows {
			members = append(members, row.domain())
		}
		return plan.Recall(members), err
	}
	for range plan.MaxAttempts {
		candidates, err := recall(r.data.ORM.WithContext(ctx))
		if err != nil {
			return err
		}
		storyID, err := plan.Choose(ctx, candidates)
		if err != nil {
			return err
		}
		err = r.data.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Positive bigint key is global grouping; item processing holds negative keys.
			if err := tx.Exec("SELECT pg_advisory_xact_lock(606001::bigint)").Error; err != nil {
				return err
			}
			current, err := recall(tx)
			if err != nil {
				return err
			}
			if !slices.Equal(candidates, current) {
				return changed
			}
			if storyID == 0 {
				record := storyModel{Title: item.Writing.Title, CreatedAt: now}
				if err := tx.Create(&record).Error; err != nil {
					return err
				}
				storyID = record.ID
			}
			if err := checkStageUpdate(ctx, tx.Model(&itemModel{}).Where("id = ? AND stage = ?", item.ID, plan.PreviousStage).Updates(map[string]any{"story_id": storyID, "stage": plan.Stage, "selected": item.Selected, "processed_at": now, "last_error": "", "topics": gorm.Expr("?::text[]", textArray(plan.Topics))}), item.ID, plan.PreviousStage); err != nil {
				return err
			}
			var records []struct {
				ID             int64
				Title          string
				Roundup        bool
				Tier           string
				Score1, Score2 *int
				PublishedAt    time.Time
			}
			if err := tx.Model(&itemModel{}).Table("items i").Select("i.id,i.title,i.roundup,s.tier,i.score1,i.score2,i.published_at").Joins("JOIN sources s ON s.id=i.source_id").Where("i.story_id = ? AND i.stage = ?", storyID, biz.StageDone).Order("i.id").Scan(&records).Error; err != nil {
				return err
			}
			items := make([]biz.Item, 0, len(records))
			for _, record := range records {
				items = append(items, biz.Item{Roundup: record.Roundup, ID: strconv.FormatInt(record.ID, 10), Title: record.Title, Source: biz.ItemSource{Tier: record.Tier}, Score1: record.Score1, Score2: record.Score2, PublishedAt: record.PublishedAt})
			}
			representative := plan.Representative(items)
			if representative.ID == "" {
				return tx.Model(&storyModel{}).Where("id = ?", storyID).Update("representative_item_id", nil).Error
			}
			representativeID, err := strconv.ParseInt(representative.ID, 10, 64)
			if err != nil {
				return err
			}
			return tx.Model(&storyModel{}).Where("id = ?", storyID).Updates(map[string]any{"title": representative.Title, "representative_item_id": representativeID}).Error
		})
		if !errors.Is(err, changed) {
			return err
		}
	}
	return fmt.Errorf("group candidates changed after %d attempts", plan.MaxAttempts)
}

func (r storyMemberRecord) domain() biz.StoryMember {
	return biz.StoryMember{StoryID: r.StoryID, Title: r.Title, Summary: r.Summary}
}
