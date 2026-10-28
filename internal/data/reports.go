package data

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/bogeia/vane/internal/biz"
)

type reportsRepo struct{ data *Data }

type reportSummaryRecord struct {
	Date     string
	Headline string
}

func NewReportsRepo(data *Data) biz.ReportsRepository {
	return &reportsRepo{data: data}
}

func (r *reportsRepo) GenerateReport(ctx context.Context, request biz.ReportRequest, compile func([]biz.ReportCandidate, []biz.Report) (biz.Report, error)) (biz.Report, error) {
	date, overwrite, start, end := request.Date, request.Overwrite, request.Since, request.Until
	var err error
	tx := r.data.ORM.WithContext(ctx).Begin()
	if err := tx.Error; err != nil {
		return biz.Report{}, err
	}
	defer tx.Rollback()
	// Report generation has its own positive lock, separate from grouping and items.
	if err = tx.Exec("SELECT pg_advisory_xact_lock(1000010::bigint)").Error; err != nil {
		return biz.Report{}, err
	}
	var content []byte
	if !overwrite {
		var saved struct {
			Content  []byte
			PushedAt *time.Time
		}
		err = tx.Model(&reportModel{}).Select("content,pushed_at").Where("date = ?", date).Take(&saved).Error
		content = saved.Content
		if err == nil {
			var report biz.Report
			report, err = decodeReport(content)
			report.PushedAt = saved.PushedAt
			if err == nil {
				err = reportIcons(tx, &report)
			}
			return report, err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return biz.Report{}, err
		}
	}
	var records []publicItemRow
	if err := publicItems(tx).Where("i.selected AND i.processed_at >= ? AND i.processed_at < ?", start, end).Order("i.id").Scan(&records).Error; err != nil {
		return biz.Report{}, err
	}
	candidates := make([]biz.ReportCandidate, 0, len(records))
	for _, record := range records {
		candidates = append(candidates, biz.ReportCandidate{Item: record.item(), ProcessedAt: *record.Item.ProcessedAt, DiscoveredAt: record.Item.DiscoveredAt})
	}
	var snapshots []reportModel
	if err := tx.Model(&reportModel{}).Select("content").Where("date < ?", date).Order("date DESC").Limit(request.HistoryLimit).Find(&snapshots).Error; err != nil {
		return biz.Report{}, err
	}
	history := make([]biz.Report, 0, len(snapshots))
	for _, snapshot := range snapshots {
		old, err := decodeReport(snapshot.Content)
		if err != nil {
			return biz.Report{}, err
		}
		history = append(history, old)
	}
	report, err := compile(candidates, history)
	if err != nil {
		return biz.Report{}, err
	}
	content, err = encodeReport(report)
	if err != nil {
		return biz.Report{}, err
	}
	if err = tx.Model(&reportModel{}).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "date"}}, DoUpdates: clause.AssignmentColumns([]string{"content"})}).Create(map[string]any{"date": date, "content": string(content)}).Error; err != nil {
		return biz.Report{}, err
	}
	if err = tx.Commit().Error; err != nil {
		return biz.Report{}, err
	}
	return report, nil
}

func (r *reportsRepo) Report(ctx context.Context, date string) (biz.Report, error) {
	var saved struct {
		Content  []byte
		PushedAt *time.Time
	}
	err := r.data.ORM.WithContext(ctx).Model(&reportModel{}).Select("content,pushed_at").Where("date = ?", date).Take(&saved).Error
	content := saved.Content
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return biz.Report{}, biz.ErrReportNotFound
	}
	if err != nil {
		return biz.Report{}, err
	}
	var report biz.Report
	report, err = decodeReport(content)
	report.PushedAt = saved.PushedAt
	if err == nil {
		err = reportIcons(r.data.ORM.WithContext(ctx), &report)
	}
	return report, err
}

func (r *reportsRepo) ListReports(ctx context.Context, limit int) ([]biz.ReportSummary, error) {
	summaries := make([]biz.ReportSummary, 0)
	var rows []reportSummaryRecord
	err := r.data.ORM.WithContext(ctx).Model(&reportModel{}).Select("date::text,COALESCE(content->'headline'->>'title','') AS headline").Order("date DESC").Limit(limit).Scan(&rows).Error
	for _, row := range rows {
		summaries = append(summaries, row.domain())
	}
	return summaries, err
}

func (r *reportsRepo) MarkReportPushed(ctx context.Context, date string, now time.Time) error {
	return r.data.ORM.WithContext(ctx).Model(&reportModel{}).Where("date = ?", date).Update("pushed_at", now).Error
}

func (r reportSummaryRecord) domain() biz.ReportSummary {
	return biz.ReportSummary{Date: r.Date, Headline: r.Headline}
}

// Artwork is live publisher metadata, even when the report's editorial content is a frozen snapshot.
func reportIcons(db *gorm.DB, report *biz.Report) error {
	items := []*biz.Item{}
	if report.Headline != nil {
		items = append(items, report.Headline)
	}
	for i := range report.Highlights {
		items = append(items, &report.Highlights[i])
	}
	for i := range report.Sections {
		for j := range report.Sections[i].Items {
			items = append(items, &report.Sections[i].Items[j])
		}
	}
	for i := range report.Briefs {
		items = append(items, &report.Briefs[i])
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.Source.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	var sources []sourceModel
	if err := db.Select("id,icon_version").Where("id IN ?", ids).Find(&sources).Error; err != nil {
		return err
	}
	icons := map[string]string{}
	for _, source := range sources {
		icons[source.ID] = source.IconVersion
	}
	for _, item := range items {
		item.Source.IconVersion = icons[item.Source.ID]
	}
	return nil
}
