package data

import (
	"context"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/biz/taxonomy"
)

func (r *itemsRepo) Topics(ctx context.Context) ([]taxonomy.Topic, error) {
	var counts []struct {
		Slug          string
		SelectedCount int64
		AllCount      int64
	}
	err := r.data.ORM.WithContext(ctx).Table("items i CROSS JOIN LATERAL unnest(i.topics) AS topic(slug)").Select("topic.slug,count(*) FILTER (WHERE i.selected AND NOT i.roundup) AS selected_count,count(*) AS all_count").Where("i.stage=?", biz.StageDone).Group("topic.slug").Scan(&counts).Error
	if err != nil {
		return nil, err
	}
	topics := make([]taxonomy.Topic, 0, len(counts))
	for _, count := range counts {
		topics = append(topics, taxonomy.Topic{Slug: count.Slug, SelectedCount: count.SelectedCount, AllCount: count.AllCount})
	}
	return topics, nil
}
