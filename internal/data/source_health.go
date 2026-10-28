package data

import (
	"context"

	"github.com/bogeia/vane/internal/biz"
)

type sourceHealthRecord struct {
	NewBlocked       int64
	Fetches          int64
	FetchSuccessRate *float64
	Prefiltered      int64
	AIRelatedRate    *float64
	Done             int64
	SelectionRate    *float64
	Scored           int64
	AverageScore     *float64
}

func (r *operationsRepo) sourceHealth(ctx context.Context, query biz.StatusQuery) (map[string]biz.SourceHealth, error) {
	db := r.data.ORM.WithContext(ctx)
	fetches := db.Model(&fetchRecordModel{}).
		Select(`source_id, count(*) AS fetches,
   round(count(*) FILTER (WHERE success)::numeric / count(*), 3) AS fetch_success_rate`).
		Where("fetched_at >= ? AND fetched_at <= ?", query.Since, query.Until).Group("source_id")
	blocked := db.Model(&blockedContentModel{}).Select("source_id, count(*) AS new_blocked").Where("first_blocked_at >= ? AND first_blocked_at <= ?", query.Since, query.Until).Group("source_id")
	items := db.Model(&itemModel{}).
		Select(`source_id,
   count(*) FILTER (WHERE prefilter IS NOT NULL) AS prefiltered,
   round(count(*) FILTER (WHERE prefilter <> ?)::numeric / NULLIF(count(*) FILTER (WHERE prefilter IS NOT NULL), 0), 3) AS ai_related_rate,
   count(*) FILTER (WHERE stage = ?) AS done,
   round(count(*) FILTER (WHERE stage = ? AND selected)::numeric / NULLIF(count(*) FILTER (WHERE stage = ?), 0), 3) AS selection_rate,
   count(*) FILTER (WHERE score1 IS NOT NULL AND score2 IS NOT NULL) AS scored,
   avg((score1 + score2) / 2.0) AS average_score`, biz.PrefilterBlock, biz.StageDone, biz.StageDone, biz.StageDone).
		Where("discovered_at >= ? AND discovered_at <= ?", query.Since, query.Until).Group("source_id")
	var rows []struct {
		SourceID string
		Health   sourceHealthRecord `gorm:"embedded"`
	}
	err := db.Table("sources s").
		Select(`s.id AS source_id, COALESCE(b.new_blocked, 0) AS new_blocked,
   COALESCE(f.fetches, 0) AS fetches, f.fetch_success_rate,
   COALESCE(i.prefiltered, 0) AS prefiltered, i.ai_related_rate,
   COALESCE(i.done, 0) AS done, i.selection_rate,
   COALESCE(i.scored, 0) AS scored, i.average_score`).
		Joins("LEFT JOIN (?) b ON b.source_id = s.id", blocked).
		Joins("LEFT JOIN (?) f ON f.source_id = s.id", fetches).
		Joins("LEFT JOIN (?) i ON i.source_id = s.id", items).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]biz.SourceHealth, len(rows))
	for _, row := range rows {
		health := row.Health.domain()
		health.Days = query.Days
		result[row.SourceID] = health
	}
	return result, nil
}

func (r sourceHealthRecord) domain() biz.SourceHealth {
	return biz.SourceHealth{NewBlocked: r.NewBlocked, Fetches: r.Fetches, FetchSuccessRate: r.FetchSuccessRate, Prefiltered: r.Prefiltered, AIRelatedRate: r.AIRelatedRate, Done: r.Done, SelectionRate: r.SelectionRate, Scored: r.Scored, AverageScore: r.AverageScore}
}
