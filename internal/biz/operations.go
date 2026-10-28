package biz

import (
	"context"
	"time"
)

type OperationsRepository interface {
	Ping(context.Context) error
	Status(context.Context, StatusQuery) (Status, error)
}
type SourceHealth struct {
	NewBlocked       int64
	Days             int
	Fetches          int64
	FetchSuccessRate *float64
	Prefiltered      int64
	AIRelatedRate    *float64
	Done             int64
	SelectionRate    *float64
	Scored           int64
	AverageScore     *float64
}

type SourceStatus struct {
	Health          SourceHealth
	ID              string
	Name            string
	Tier            string
	Enabled         bool
	LastError       string
	ItemCount       int64
	LastCollectedAt *time.Time
}

type BlockedContent struct {
	SourceID, OriginalTitle, URL string
	FirstBlockedAt               time.Time
}

type Status struct {
	RecentBlocked []BlockedContent
	Sources       []SourceStatus
	Stages        map[string]int64
	FailedItems   []FailedItem
}

type FailedItem struct {
	ID            string
	OriginalTitle string
	Attempts      int
	LastError     string
}

type OperationsUseCase struct{ repo OperationsRepository }

func NewOperationsUseCase(repo OperationsRepository) *OperationsUseCase {
	return &OperationsUseCase{repo: repo}
}

func (u *OperationsUseCase) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return u.repo.Ping(ctx)
}

func (u *OperationsUseCase) Status(ctx context.Context, days int) (Status, error) {
	now := time.Now()
	status, err := u.repo.Status(ctx, StatusQuery{RecentBlockedLimit: 20, RecentBlockedNewestFirst: true, Days: days, Since: now.Add(-time.Duration(days) * 24 * time.Hour), Until: now})
	if status.Stages == nil {
		status.Stages = map[string]int64{}
	}
	for _, stage := range []string{StageNew, StagePrefiltered, StageAnalyzed, StageDone, StageBlocked, StageFailed} {
		if _, exists := status.Stages[stage]; !exists {
			status.Stages[stage] = 0
		}
	}
	return status, err
}

type StatusQuery struct {
	RecentBlockedLimit       int
	RecentBlockedNewestFirst bool
	Days                     int
	Since, Until             time.Time
}
