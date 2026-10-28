package data

import (
	"context"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

type sourceAlertsRepo struct{ data *Data }

func NewSourceAlertsRepo(data *Data) biz.SourceAlertsRepository { return &sourceAlertsRepo{data: data} }
func (r *sourceAlertsRepo) SourcesForAlerts(ctx context.Context) ([]biz.SourceAlertState, error) {
	var sources []sourceModel
	if err := r.data.ORM.WithContext(ctx).Select("id,name,alert_started_at").Where("enabled = ?", true).Order("id").Find(&sources).Error; err != nil {
		return nil, err
	}
	result := make([]biz.SourceAlertState, 0, len(sources))
	for _, source := range sources {
		// Include the latest success and every attempt after it; biz decides whether to alert.
		var records []fetchRecordModel
		err := r.data.ORM.WithContext(ctx).Where("source_id = ? AND id >= COALESCE((SELECT MAX(id) FROM fetch_records WHERE source_id = ? AND success = true), 0)", source.ID, source.ID).Order("id DESC").Find(&records).Error
		if err != nil {
			return nil, err
		}
		state := biz.SourceAlertState{ID: source.ID, Name: source.Name, AlertStartedAt: source.AlertStartedAt}
		for _, record := range records {
			state.Records = append(state.Records, biz.SourceAlertFetch{FetchedAt: record.FetchedAt, Success: record.Success, Error: record.Error})
		}
		result = append(result, state)
	}
	return result, nil
}

func (r *sourceAlertsRepo) SetSourceAlert(ctx context.Context, id string, start *time.Time) error {
	return r.data.ORM.WithContext(ctx).Model(&sourceModel{}).Where("id = ? AND enabled = ?", id, true).Update("alert_started_at", start).Error
}
