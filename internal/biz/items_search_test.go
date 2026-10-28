package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type searchRepositoryCall struct {
	limit  int
	filter ItemFilter
}

type searchItemsRepository struct {
	ItemsRepository
	selected    []Item
	all         []Item
	selectedErr error
	allErr      error
	calls       []searchRepositoryCall
}

func (r *searchItemsRepository) ListItems(_ context.Context, _ *Cursor, limit int, filter ItemFilter) ([]Item, error) {
	r.calls = append(r.calls, searchRepositoryCall{limit: limit, filter: filter})
	if filter.View == ViewSelected {
		return r.selected, r.selectedErr
	}
	return r.all, r.allErr
}

func TestItemsUseCaseSearchKeepsSelectedResultsWithoutFallback(t *testing.T) {
	now := time.Date(2026, 10, 26, 8, 34, 0, 0, time.UTC)
	selected := Item{ID: "11", Title: "selected result"}
	repo := &searchItemsRepository{selected: []Item{selected}, all: []Item{{ID: "22", Title: "all result"}}}
	usecase := NewItemsUseCase(repo, func() time.Time { return now })

	result, err := usecase.Search(t.Context(), SearchQuery{Q: "OpenAI", Window: Window7d, Limit: 3})
	require.NoError(t, err)
	assert.False(t, result.Expanded)
	assert.Equal(t, []Item{selected}, result.Items)
	require.Len(t, repo.calls, 1)
	assert.Equal(t, 4, repo.calls[0].limit)
	assert.Equal(t, ItemFilter{View: ViewSelected, Q: "OpenAI", Since: now.Add(-7 * 24 * time.Hour)}, repo.calls[0].filter)
}

func TestItemsUseCaseSearchFallsBackToAllWhenSelectedIsEmpty(t *testing.T) {
	now := time.Date(2026, 10, 26, 8, 34, 0, 0, time.UTC)
	all := Item{ID: "22", Title: "all result"}
	repo := &searchItemsRepository{selected: []Item{}, all: []Item{all}}
	usecase := NewItemsUseCase(repo, func() time.Time { return now })

	result, err := usecase.Search(t.Context(), SearchQuery{Q: "OpenAI", Window: Window24h, Limit: 5})
	require.NoError(t, err)
	assert.True(t, result.Expanded)
	assert.Equal(t, []Item{all}, result.Items)
	require.Len(t, repo.calls, 2)
	assert.Equal(t, 6, repo.calls[0].limit)
	assert.Equal(t, 6, repo.calls[1].limit)
	assert.Equal(t, ItemFilter{View: ViewSelected, Q: "OpenAI", Since: now.Add(-24 * time.Hour)}, repo.calls[0].filter)
	assert.Equal(t, ItemFilter{View: ViewAll, Q: "OpenAI", Since: now.Add(-24 * time.Hour)}, repo.calls[1].filter)
}

func TestItemsUseCaseSearchReturnsEmptyWithoutMarkingExpansion(t *testing.T) {
	repo := &searchItemsRepository{selected: []Item{}, all: []Item{}}
	usecase := NewItemsUseCase(repo, time.Now)

	result, err := usecase.Search(t.Context(), SearchQuery{Q: "OpenAI", Window: Window7d, Limit: 10})
	require.NoError(t, err)
	assert.False(t, result.Expanded)
	assert.Empty(t, result.Items)
	require.Len(t, repo.calls, 2)
}

func TestItemsUseCaseSearchPropagatesRepositoryErrors(t *testing.T) {
	failure := errors.New("search query failed")
	for _, tc := range []struct {
		name string
		repo *searchItemsRepository
	}{
		{name: "selected query", repo: &searchItemsRepository{selectedErr: failure}},
		{name: "all query after empty selected", repo: &searchItemsRepository{selected: []Item{}, allErr: failure}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usecase := NewItemsUseCase(tc.repo, time.Now)
			_, err := usecase.Search(t.Context(), SearchQuery{Q: "OpenAI", Window: Window7d, Limit: 10})
			require.ErrorIs(t, err, failure)
		})
	}
}
