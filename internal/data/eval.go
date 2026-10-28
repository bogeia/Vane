package data

import (
	"context"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

type evalRepo struct{ data *Data }

type goldItemRecord struct {
	ID            int64
	URL           string
	Tier          string
	Source        string
	Title         string
	OriginalTitle string
	PublishedAt   time.Time
	Score1        int
	Score2        int
	Selected      bool
	Body          string
}

func NewEvalRepo(data *Data) biz.EvalRepository {
	return &evalRepo{data: data}
}

func (r *evalRepo) ExportGold(ctx context.Context, since, until time.Time, n int) (biz.GoldSet, error) {
	result := biz.GoldSet{Items: []biz.GoldItem{}}
	var rows []goldItemRecord
	err := r.data.ORM.WithContext(ctx).Model(&itemModel{}).Table("items i").
		Select("i.id,i.url,s.tier,s.name AS source,i.title,i.original_title,i.published_at,i.score1,i.score2,i.selected,i.body").
		Joins("JOIN sources s ON s.id=i.source_id").
		Where("i.discovered_at >= ? AND i.discovered_at <= ? AND i.stage = ? AND i.score1 IS NOT NULL AND i.score2 IS NOT NULL AND NOT i.roundup", since, until, biz.StageDone).
		Order("random()").Limit(n).Scan(&rows).Error
	for _, row := range rows {
		result.Items = append(result.Items, row.domain())
	}
	return result, err
}

func (r goldItemRecord) domain() biz.GoldItem {
	return biz.GoldItem{ID: r.ID, URL: r.URL, Tier: r.Tier, Source: r.Source, Title: r.Title, OriginalTitle: r.OriginalTitle, PublishedAt: r.PublishedAt, Score1: r.Score1, Score2: r.Score2, Selected: r.Selected, Body: r.Body}
}
