package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/infra/fetch"
	"github.com/bogeia/vane/internal/service"
)

func TestHotAPIOrdersDoneStoriesAndCachesEmptyAndPopulatedResults(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Collect.Enabled = false
	cfg.LLM.Enabled = false
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	ctx := t.Context()
	for _, source := range []struct{ id, name, tier string }{
		{"hot-a", "Hot Source A", "official"},
		{"hot-b", "Hot Source B", "media"},
		{"hot-c", "Hot Source C", "media"},
	} {
		if _, err := testDB(t, application.Data).ExecContext(ctx, `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES($1,$2,$3,$4,false,60)`, source.id, source.name, source.tier, "https://example.test/"+source.id); err != nil {
			t.Fatalf("seed source: %v", err)
		}
	}

	initial := requestHotPage(t, application.Router)
	if len(initial) != 0 {
		t.Fatalf("empty hot board returned %d items, want empty list", len(initial))
	}
	oldStoryID := insertHotTestStory(t, ctx, testDB(t, application.Data), "Old event", now)
	for index, source := range []string{"hot-a", "hot-b"} {
		insertHotTestItem(t, ctx, testDB(t, application.Data), oldStoryID, source, "old-"+strconv.Itoa(index), now.Add(2*time.Minute), "done")
	}
	if _, err := testDB(t, application.Data).ExecContext(ctx, `UPDATE stories SET representative_item_id=(SELECT id FROM items WHERE story_id=$1 ORDER BY id LIMIT 1) WHERE id=$1`, oldStoryID); err != nil {
		t.Fatalf("set old representative: %v", err)
	}
	newStoryID := insertHotTestStory(t, ctx, testDB(t, application.Data), "New high-heat event", now)
	for index, source := range []string{"hot-a", "hot-b", "hot-c"} {
		insertHotTestItem(t, ctx, testDB(t, application.Data), newStoryID, source, "new-"+strconv.Itoa(index), now.Add(7*time.Minute), "done")
	}
	if _, err := testDB(t, application.Data).ExecContext(ctx, `UPDATE stories SET representative_item_id=(SELECT id FROM items WHERE story_id=$1 ORDER BY id LIMIT 1) WHERE id=$1`, newStoryID); err != nil {
		t.Fatalf("set new representative: %v", err)
	}
	hiddenStoryID := insertHotTestStory(t, ctx, testDB(t, application.Data), "Hidden stage event", now)
	for index, source := range []string{"hot-a", "hot-b", "hot-c"} {
		insertHotTestItem(t, ctx, testDB(t, application.Data), hiddenStoryID, source, "hidden-"+strconv.Itoa(index), now, "blocked")
	}

	now = now.Add(4*time.Minute + 59*time.Second)
	stillEmpty := requestHotPage(t, application.Router)
	if len(stillEmpty) != 0 {
		t.Fatalf("empty board cache changed before expiration: %+v", stillEmpty)
	}
	now = now.Add(time.Second)
	populated := requestHotPage(t, application.Router)
	if len(populated) != 1 || populated[0].StoryID != strconv.FormatInt(oldStoryID, 10) || populated[0].Trend != "new" {
		t.Fatalf("hot board at empty-cache expiration = %+v, want only old event as new", populated)
	}
	if len(populated[0].Sources) != 2 {
		t.Errorf("hot item sources = %+v, want only two done sources", populated[0].Sources)
	}

	now = now.Add(4*time.Minute + 59*time.Second)
	const concurrentRequests = 5
	responses := make([][]service.HotStory, concurrentRequests)
	var wait sync.WaitGroup
	for index := range concurrentRequests {
		wait.Go(func() {
			responses[index] = requestHotPage(t, application.Router)
		})
	}
	wait.Wait()
	for index, stories := range responses {
		if len(stories) != 1 || stories[0].StoryID != strconv.FormatInt(oldStoryID, 10) {
			t.Errorf("cached hot response %d before 5 minutes = %+v, want cached old event", index, stories)
		}
	}

	now = now.Add(time.Second)
	refreshed := requestHotPage(t, application.Router)
	if len(refreshed) != 2 || refreshed[0].StoryID != strconv.FormatInt(newStoryID, 10) || refreshed[1].StoryID != strconv.FormatInt(oldStoryID, 10) {
		t.Fatalf("hot board at populated-cache expiration = %+v, want newly available event ranked first", refreshed)
	}
}

func requestHotPage(t *testing.T, router http.Handler) []service.HotStory {
	t.Helper()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/hot", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/hot returned %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []service.HotStory `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /api/hot response: %v", err)
	}
	if payload.Items == nil {
		t.Fatal("GET /api/hot returned null items, want an array")
	}
	return payload.Items
}

func insertHotTestStory(t *testing.T, ctx context.Context, db *sql.DB, title string, createdAt time.Time) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(ctx, `INSERT INTO stories(title,created_at) VALUES($1,$2) RETURNING id`, title, createdAt).Scan(&id); err != nil {
		t.Fatalf("seed hot story: %v", err)
	}
	return id
}

func insertHotTestItem(t *testing.T, ctx context.Context, db *sql.DB, storyID int64, sourceID, suffix string, publishedAt time.Time, stage string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(ctx, `INSERT INTO items(source_id,url,original_title,published_at,stage,score1,score2,title,summary,reason,category,tags,selected,story_id) VALUES($1,$2,$3,$4,$5,80,90,$3,'summary','reason','行业',ARRAY['AI'],false,$6) RETURNING id`, sourceID, "https://example.test/hot/"+suffix, "Original "+suffix, publishedAt, stage, storyID).Scan(&id); err != nil {
		t.Fatalf("seed hot %s item: %v", stage, err)
	}
	return id
}
