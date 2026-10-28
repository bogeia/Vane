package biz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type latestItemsRepository struct {
	ItemsRepository
	limit  int
	filter ItemFilter
}

func (r *latestItemsRepository) ListItems(_ context.Context, _ *Cursor, limit int, filter ItemFilter) ([]Item, error) {
	r.limit, r.filter = limit, filter
	return []Item{{ID: "1", Title: "latest"}}, nil
}

func TestItemsUseCaseLatestUsesWindowAndPassesFilters(t *testing.T) {
	now := time.Date(2026, 10, 26, 8, 34, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	for _, tc := range []struct {
		name   string
		window Window
		since  time.Time
	}{
		{"past day", Window24h, now.Add(-24 * time.Hour)},
		{"past week", Window7d, now.Add(-7 * 24 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &latestItemsRepository{}
			page, err := NewItemsUseCase(repo, func() time.Time { return now }).Latest(t.Context(), LatestQuery{
				Window: tc.window, View: "selected", Topic: "openai", Limit: 7,
			})
			require.NoError(t, err)
			assert.Equal(t, []Item{{ID: "1", Title: "latest"}}, page.Items)
			assert.Equal(t, tc.since, repo.filter.Since)
			assert.Equal(t, "selected", repo.filter.View)
			assert.Equal(t, "openai", repo.filter.Topic)
			assert.Equal(t, 8, repo.limit)
		})
	}
}
