package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"

	"github.com/bogeia/vane/internal/biz"
)

type ReportSummary struct {
	Date     string `json:"date"`
	Headline string `json:"headline"`
}

type ReportSection struct {
	Category string `json:"category"`
	Items    []Item `json:"items"`
}

type Report struct {
	Date       string          `json:"date"`
	Headline   *Item           `json:"headline"`
	Highlights []Item          `json:"highlights"`
	Sections   []ReportSection `json:"sections"`
	Briefs     []Item          `json:"briefs"`
}

type ReportsService struct {
	reports *biz.ReportsUseCase
	log     *slog.Logger
}

func NewReportsService(reports *biz.ReportsUseCase, log *slog.Logger) *ReportsService {
	return &ReportsService{reports, log}
}

type (
	ReportsOutput struct {
		Body struct {
			Items []ReportSummary `json:"items"`
		}
	}
	ReportInput struct {
		Date string `path:"date"`
	}
	ReportOutput struct{ Body Report }
)

func (r *ReportsService) List(ctx context.Context, _ *struct{}) (*ReportsOutput, error) {
	items, err := r.reports.List(ctx)
	if err != nil {
		r.log.ErrorContext(ctx, "list reports", "error", err)
		return nil, huma.Error500InternalServerError("internal server error")
	}
	output := &ReportsOutput{}
	if items != nil {
		output.Body.Items = make([]ReportSummary, len(items))
		for index, item := range items {
			output.Body.Items[index] = ReportSummary{Date: item.Date, Headline: item.Headline}
		}
	}
	return output, nil
}

func (r *ReportsService) Get(ctx context.Context, input *ReportInput) (*ReportOutput, error) {
	if _, _, err := biz.ReportWindow(input.Date); err != nil {
		return nil, huma.Error400BadRequest("invalid report date")
	}
	report, err := r.reports.Get(ctx, input.Date)
	if errors.Is(err, biz.ErrReportNotFound) {
		return nil, huma.Error404NotFound("report not found")
	}
	if err != nil {
		r.log.ErrorContext(ctx, "get report", "error", err)
		return nil, huma.Error500InternalServerError("internal server error")
	}
	return &ReportOutput{Body: reportDTO(report)}, nil
}

func reportDTO(in biz.Report) Report {
	out := Report{Date: in.Date, Highlights: itemsDTO(in.Highlights), Briefs: itemsDTO(in.Briefs)}
	if in.Headline != nil {
		headline := itemDTO(*in.Headline)
		out.Headline = &headline
	}
	if in.Sections != nil {
		out.Sections = make([]ReportSection, len(in.Sections))
		for i, section := range in.Sections {
			out.Sections[i] = ReportSection{Category: section.Category, Items: itemsDTO(section.Items)}
		}
	}
	return out
}
