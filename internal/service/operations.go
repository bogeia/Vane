package service

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"

	"github.com/bogeia/vane/internal/biz"
)

type OperationsService struct {
	operations *biz.OperationsUseCase
	log        *slog.Logger
}

func NewOperationsService(operations *biz.OperationsUseCase, log *slog.Logger) *OperationsService {
	return &OperationsService{operations: operations, log: log}
}

type HealthOutput struct {
	Body struct {
		Status   string `json:"status"`
		Database string `json:"database"`
	}
}

func (o *OperationsService) Health(ctx context.Context, _ *struct{}) (*HealthOutput, error) {
	if err := o.operations.Health(ctx); err != nil {
		o.log.ErrorContext(ctx, "database health check failed", "error", err)
		return nil, huma.Error503ServiceUnavailable("database unavailable")
	}
	output := &HealthOutput{}
	output.Body.Status = "ok"
	output.Body.Database = "connected"
	return output, nil
}
