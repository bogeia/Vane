package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/data"
)

func groupStore(t *testing.T) (*data.Data, time.Time) {
	t.Helper()
	url := testDatabaseURL(t)
	resetTestSchema(t, url)
	store, cleanup, err := data.Open(t.Context(), loadTestConfig(t, url))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := data.Migrate(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('group','Group','official','https://example.test/feed',false,60)`); err != nil {
		t.Fatal(err)
	}
	return store, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
}

func groupItem(t *testing.T, store *data.Data, now time.Time, n int) biz.WorkItem {
	t.Helper()
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('group','Group','official','https://example.test/feed',false,60) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	writing := &biz.Writing{Title: "OpenAI Atlas release", Summary: "OpenAI Atlas release details"}
	var id int64
	if err := testDB(t, store).QueryRowContext(t.Context(), `INSERT INTO items(source_id,url,original_title,published_at,next_attempt_at,title,summary,stage,score1,score2) VALUES('group',$1,$2,$3,$3,$2,$4,'analyzed',80,80) RETURNING id`, fmt.Sprintf("https://example.test/%d", n), writing.Title, now, writing.Summary).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return biz.WorkItem{Stage: biz.StageAnalyzed, ID: id, Writing: writing, Selected: true}
}

func TestSlowGroupChoiceDoesNotHoldGlobalLock(t *testing.T) {
	store, now := groupStore(t)
	// One connection per worker plus one shared grouping connection must suffice.
	testDB(t, store).SetMaxOpenConns(3)
	first, second := groupItem(t, store, now, 1), groupItem(t, store, now, 2)
	for range 2 {
		conn, err := testDB(t, store).Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
	}
	started, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	calls := 0
	go func() {
		result <- data.NewWorkRepo(store).Group(t.Context(), first, now, biz.PlanGrouping(first, now, func(ctx context.Context, candidates []biz.StoryCandidate) (int64, error) {
			calls++
			if calls == 1 {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return 0, ctx.Err()
				}
			}
			if len(candidates) > 0 {
				return candidates[0].StoryID, nil
			}
			return 0, nil
		}))
	}()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	err := data.NewWorkRepo(store).Group(ctx, second, now, biz.PlanGrouping(second, now, func(context.Context, []biz.StoryCandidate) (int64, error) { return 0, nil }))
	cancel()
	close(release)
	if firstErr := <-result; firstErr != nil {
		t.Fatal(firstErr)
	}
	if err != nil {
		t.Fatalf("slow choice blocked another group: %v", err)
	}
	if calls != 2 {
		t.Fatalf("choices=%d; expected redecision after empty candidates changed", calls)
	}
	var stories int
	if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT count(*) FROM stories").Scan(&stories); err != nil {
		t.Fatal(err)
	}
	if stories != 1 {
		t.Fatalf("concurrent same event created %d stories", stories)
	}
}

func TestGroupRechecksFullCandidateContent(t *testing.T) {
	store, now := groupStore(t)
	seed, item := groupItem(t, store, now, 1), groupItem(t, store, now, 2)
	if err := data.NewWorkRepo(store).Group(t.Context(), seed, now, biz.PlanGrouping(seed, now, func(context.Context, []biz.StoryCandidate) (int64, error) { return 0, nil })); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := data.NewWorkRepo(store).Group(t.Context(), item, now, biz.PlanGrouping(item, now, func(ctx context.Context, candidates []biz.StoryCandidate) (int64, error) {
		calls++
		if len(candidates) != 1 {
			t.Fatalf("candidates=%v", candidates)
		}
		if calls == 1 {
			if _, err := testDB(t, store).ExecContext(ctx, "UPDATE items SET summary = summary || ' changed' WHERE id=$1", seed.ID); err != nil {
				return 0, err
			}
		} else if candidates[0].Summary == seed.Writing.Summary {
			t.Error("redecision used stale candidate content")
		}
		return candidates[0].StoryID, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("choices=%d; expected full candidate recheck", calls)
	}
}

func TestGroupStopsAfterRepeatedCandidateChanges(t *testing.T) {
	store, now := groupStore(t)
	seed, item := groupItem(t, store, now, 1), groupItem(t, store, now, 2)
	if err := data.NewWorkRepo(store).Group(t.Context(), seed, now, biz.PlanGrouping(seed, now, func(context.Context, []biz.StoryCandidate) (int64, error) { return 0, nil })); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := data.NewWorkRepo(store).Group(t.Context(), item, now, biz.PlanGrouping(item, now, func(ctx context.Context, candidates []biz.StoryCandidate) (int64, error) {
		calls++
		_, err := testDB(t, store).ExecContext(ctx, "UPDATE items SET summary = summary || ' changed' WHERE id=$1", seed.ID)
		return candidates[0].StoryID, err
	}))
	if err == nil || calls != 3 {
		t.Fatalf("choices=%d error=%v; expected bounded failure", calls, err)
	}
	var stage string
	if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT stage FROM items WHERE id=$1", item.ID).Scan(&stage); err != nil {
		t.Fatal(err)
	}
	if stage != "analyzed" {
		t.Fatalf("stale grouping published item: %s", stage)
	}
}
