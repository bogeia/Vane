package integration

import (
	"io"
	"log/slog"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/data"
)

func TestAgentLatestRepositoryFiltersSinceAndKeepsPublicFilters(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Collect.Enabled = false
	cfg.LLM.Enabled = false
	now := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	app, cleanup, err := newTestApp(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now })
	require.NoError(t, err)
	t.Cleanup(cleanup)
	db := testDB(t, app.Data)
	_, err = db.ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('agent-fixture','Agent Fixture','official','https://example.test/feed',false,60)`)
	require.NoError(t, err)
	cutoff := now.Add(-24 * time.Hour)
	add := func(title string, published time.Time, stage string, selected bool) int64 {
		t.Helper()
		story := insertFoldTestStory(t, t.Context(), db, title, now)
		id := insertFoldTestItem(t, t.Context(), db, story, stage, "agent-fixture", title, title, "summary", "reason", "https://example.test/"+title, "产品", []string{"Agent"}, published, selected)
		_, err := db.ExecContext(t.Context(), `UPDATE items SET topics = ARRAY['agent'] WHERE id = $1`, id)
		require.NoError(t, err)
		if selected && stage == biz.StageDone {
			_, err := db.ExecContext(t.Context(), `UPDATE stories SET representative_item_id = $2 WHERE id = $1`, story, id)
			require.NoError(t, err)
		}
		return id
	}
	exact := add("since fixture exact", cutoff, biz.StageDone, true)
	older := add("since fixture older", cutoff.Add(-time.Nanosecond), biz.StageDone, true)
	add("since fixture unselected", cutoff, biz.StageDone, false)
	add("since fixture unfinished", now, biz.StageNew, true)
	repository := data.NewItemsRepo(app.Data)

	filtered, err := repository.ListItems(t.Context(), nil, 20, biz.ItemFilter{
		Since: cutoff, View: biz.ViewSelected, Topic: "agent", Q: "since fixture",
	})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, strconv.FormatInt(exact, 10), filtered[0].ID)

	unbounded, err := repository.ListItems(t.Context(), nil, 20, biz.ItemFilter{View: biz.ViewSelected, Topic: "agent", Q: "since fixture"})
	require.NoError(t, err)
	ids := make([]string, len(unbounded))
	for index, item := range unbounded {
		ids[index] = item.ID
	}
	slices.Sort(ids)
	want := []string{strconv.FormatInt(exact, 10), strconv.FormatInt(older, 10)}
	slices.Sort(want)
	assert.Equal(t, want, ids)
}
