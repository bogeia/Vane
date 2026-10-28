package data

import (
	"context"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

type hotRepo struct{ data *Data }

func NewHotRepo(data *Data) biz.HotRepository {
	return &hotRepo{data: data}
}

func (r *hotRepo) HotMentions(ctx context.Context, since, until time.Time) ([]biz.HotMention, error) {
	var records []struct {
		StoryID           string
		Title             string
		SourceID          string
		SourceName        string
		SourceTier        string
		SourceIconVersion string
		PublishedAt       time.Time
	}
	err := r.data.ORM.WithContext(ctx).Model(&itemModel{}).Table("items i").Select("st.id::text AS story_id,st.title,s.id AS source_id,s.name AS source_name,s.tier AS source_tier,s.icon_version AS source_icon_version,i.published_at").Joins("JOIN stories st ON st.id=i.story_id").Joins("JOIN sources s ON s.id=i.source_id").Where("i.stage = ? AND i.published_at >= ? AND i.published_at <= ?", biz.StageDone, since, until).Order("st.id,s.id,i.published_at,i.id").Scan(&records).Error
	if err != nil {
		return nil, err
	}
	mentions := make([]biz.HotMention, 0, len(records))
	for _, record := range records {
		mentions = append(mentions, biz.HotMention{StoryID: record.StoryID, Title: record.Title, Source: biz.ItemSource{ID: record.SourceID, Name: record.SourceName, Tier: record.SourceTier, IconVersion: record.SourceIconVersion}, PublishedAt: record.PublishedAt})
	}
	return mentions, nil
}
