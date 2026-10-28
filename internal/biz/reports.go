package biz

import (
	"context"
	"errors"
	"time"
)

var (
	ErrReportNotFound = errors.New("report not found")
	ErrNoWebhooks     = errors.New("no webhooks configured")
	ErrEmptyReport    = errors.New("report body is empty")
)

type ReportSummary struct {
	Date     string
	Headline string
}
type PushMessage struct{ Title, Content string }

type MessageSender interface {
	Send(context.Context, string, []PushMessage) error
}

type ReportPusher interface {
	Push(context.Context, Report) error
}

type ReportsRepository interface {
	MarkReportPushed(context.Context, string, time.Time) error
	GenerateReport(context.Context, ReportRequest, func([]ReportCandidate, []Report) (Report, error)) (Report, error)
	Report(context.Context, string) (Report, error)
	ListReports(context.Context, int) ([]ReportSummary, error)
}
type ReportsUseCase struct {
	pusher ReportPusher
	repo   ReportsRepository
	now    func() time.Time
}

func NewReportsUseCase(repo ReportsRepository, now func() time.Time, pusher ReportPusher) *ReportsUseCase {
	return &ReportsUseCase{pusher, repo, now}
}

func (r *ReportsUseCase) RunOnce(ctx context.Context) error {
	now := r.now().In(beijing)
	if now.Hour() < 8 {
		return nil
	}
	report, err := r.generate(ctx, now.Format("2006-01-02"), false)
	if err != nil {
		return err
	}
	if r.pusher == nil || report.Headline == nil || report.PushedAt != nil {
		return nil
	}
	if err := r.pusher.Push(ctx, report); err != nil {
		return err
	}
	return r.repo.MarkReportPushed(ctx, report.Date, r.now())
}

func (r *ReportsUseCase) Push(ctx context.Context, date string) error {
	if _, _, err := ReportWindow(date); err != nil {
		return err
	}
	report, err := r.repo.Report(ctx, date)
	if err != nil {
		return err
	}
	if r.pusher == nil {
		return ErrNoWebhooks
	}
	if report.Headline == nil {
		return ErrEmptyReport
	}
	if err := r.pusher.Push(ctx, report); err != nil {
		return err
	}
	return r.repo.MarkReportPushed(ctx, date, r.now())
}

func (r *ReportsUseCase) Generate(ctx context.Context, date string) (Report, error) {
	return r.generate(ctx, date, true)
}

func (r *ReportsUseCase) Get(ctx context.Context, date string) (Report, error) {
	return r.repo.Report(ctx, date)
}

func (r *ReportsUseCase) List(ctx context.Context) ([]ReportSummary, error) {
	return r.repo.ListReports(ctx, 60)
}

type ReportRequest struct {
	Date         string
	Overwrite    bool
	Since, Until time.Time
	HistoryLimit int
}

func (r *ReportsUseCase) generate(ctx context.Context, date string, overwrite bool) (Report, error) {
	start, end, err := ReportWindow(date)
	if err != nil {
		return Report{}, err
	}
	return r.repo.GenerateReport(ctx, ReportRequest{Date: date, Overwrite: overwrite, Since: start, Until: end, HistoryLimit: 7}, func(candidates []ReportCandidate, history []Report) (Report, error) {
		recent := make([]string, 0)
		for _, old := range history {
			for _, section := range old.Sections {
				for _, item := range section.Items {
					recent = append(recent, item.StoryID)
				}
			}
			for _, item := range old.Briefs {
				recent = append(recent, item.StoryID)
			}
		}
		for index := range candidates {
			candidates[index].Item = CompleteItem(candidates[index].Item)
		}
		return CompileReport(date, candidates, recent)
	})
}
