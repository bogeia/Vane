//go:build wireinject

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/wire"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/job"
	"github.com/bogeia/vane/internal/server"
	"github.com/bogeia/vane/internal/service"
)

func NewServe(ctx context.Context, cfg config.Config, log *slog.Logger, now func() time.Time) (*App, func(), error) {
	panic(wire.Build(
		data.ProviderSet, biz.ProviderSet, service.ProviderSet, server.ProviderSet,
		job.ProviderSet, New,
	))
}

func NewStatus(ctx context.Context, cfg config.Config) (*biz.OperationsUseCase, func(), error) {
	panic(wire.Build(data.StatusProviderSet, biz.StatusProviderSet))
}

func NewReport(ctx context.Context, cfg config.Config, log *slog.Logger, now func() time.Time) (*biz.ReportsUseCase, func(), error) {
	panic(wire.Build(data.ProviderSet, biz.ProviderSet))
}

func NewEvalExport(ctx context.Context, cfg config.Config) (*biz.EvalExportUseCase, func(), error) {
	panic(wire.Build(data.EvalExportProviderSet, biz.EvalExportProviderSet))
}

func NewEvalRun(cfg config.Config) *biz.EvaluationUseCase {
	panic(wire.Build(data.EvaluationProviderSet, biz.EvaluationProviderSet))
}

func NewSourcesImport(cfg config.Config, now func() time.Time) *biz.SourcesUseCase {
	panic(wire.Build(data.SourcesImportProviderSet, biz.SourcesProviderSet))
}

func NewTopicsRebuild(ctx context.Context, cfg config.Config) (*biz.TopicsUseCase, func(), error) {
	panic(wire.Build(data.TopicsProviderSet, biz.TopicsProviderSet))
}
