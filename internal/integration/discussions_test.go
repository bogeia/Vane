package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/data"
)

func discussionStore(t *testing.T) (*data.Data, biz.DiscussionQuery, int64) {
	t.Helper()
	store, _ := groupStore(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedDiscussionPool(t, store, now)
	item := groupItem(t, store, now, 1)
	if err := data.NewWorkRepo(store).Group(t.Context(), item, now, biz.PlanGrouping(item, now, func(context.Context, []biz.StoryCandidate) (int64, error) { return 0, nil })); err != nil {
		t.Fatal(err)
	}
	var storyID int64
	if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT story_id FROM items WHERE id=$1", item.ID).Scan(&storyID); err != nil {
		t.Fatal(err)
	}
	return store, biz.DiscussionQuery{Since: now.Add(-biz.DiscussionWindow), Now: now, Limit: 50}, storyID
}

type integrationHNFetcher struct{ thread biz.HNThread }

func (f integrationHNFetcher) Recent(context.Context, time.Time) ([]biz.HNThread, error) {
	return []biz.HNThread{f.thread}, nil
}

func (f integrationHNFetcher) Comments(context.Context, biz.HNThread) ([]biz.HNComment, error) {
	return []biz.HNComment{{ID: 11, ParentID: f.thread.ID, Text: "first public comment"}, {ID: 12, ParentID: f.thread.ID, Text: "second public comment"}}, nil
}

func seedDiscussionPool(t *testing.T, store *data.Data, now time.Time) {
	t.Helper()
	fetcher := integrationHNFetcher{thread: biz.HNThread{ID: 10, URL: "https://example.test/1", Title: "OpenAI releases GPT-4.1", PublishedAt: now}}
	u := biz.NewDiscussionsUseCase(data.NewDiscussionsRepo(store), fetcher, nil, nil, biz.DiscussionOptions{Enabled: true, RefreshInterval: 30 * time.Minute}, func() time.Time { return now })
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	var pools, stories int
	if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT (SELECT count(*) FROM hn_discussion_pool WHERE fingerprint<>''),(SELECT count(*) FROM stories)").Scan(&pools, &stories); err != nil || pools != 1 || stories != 0 {
		t.Fatalf("independent pool: pools=%d stories=%d err=%v", pools, stories, err)
	}
}

func discussionState(target biz.DiscussionTarget, now time.Time) biz.DiscussionState {
	pool := target.Pool[0]
	return biz.DiscussionState{TargetSignature: target.Signature, RuleVersion: biz.DiscussionMatchRuleVersion, PoolFingerprint: pool.Fingerprint, Input: pool.Input, Fingerprint: pool.Fingerprint, Points: []biz.DiscussionPoint{{Text: "用户讨论实际体验。", CommentID: 11}}, UpdatedAt: now, ConfirmedAt: pool.ConfirmedAt, CheckedAt: now, NextRefreshAt: now.Add(30 * time.Minute)}
}

func TestDiscussionCachePersistsAndDoesNotChangeNews(t *testing.T) {
	store, query, storyID := discussionStore(t)
	repo := data.NewDiscussionsRepo(store)
	calls := 0
	if err := repo.RefreshDue(t.Context(), query, func(_ context.Context, target biz.DiscussionTarget, _ biz.DiscussionState) (biz.DiscussionState, error) {
		calls++
		if target.StoryID != storyID || len(target.URLs) != 1 {
			t.Fatalf("target: %+v", target)
		}
		return discussionState(target, query.Now), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := data.NewDiscussionsRepo(store).RefreshDue(t.Context(), query, func(context.Context, biz.DiscussionTarget, biz.DiscussionState) (biz.DiscussionState, error) {
		t.Fatal("cache was refreshed before due time")
		return biz.DiscussionState{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	if err := data.Migrate(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	story, err := biz.NewStoriesUseCase(data.NewStoriesRepo(store)).Get(t.Context(), storyID)
	if err != nil || story.Discussion == nil || story.Discussion.SampleSize != 2 || story.Discussion.Points[0].CommentID != 11 {
		t.Fatalf("story cache: %+v %v", story.Discussion, err)
	}
	var score1, score2, sources int
	var stage string
	var selected bool
	if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT stage,selected,score1,score2,(SELECT count(*) FROM sources) FROM items WHERE story_id=$1", storyID).Scan(&stage, &selected, &score1, &score2, &sources); err != nil {
		t.Fatal(err)
	}
	if stage != biz.StageDone || !selected || score1 != 80 || score2 != 80 || sources != 1 {
		t.Fatal("discussion changed news processing or source count")
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), "UPDATE story_discussions SET confirmed_at=$1 WHERE story_id=$2", query.Now.Add(-25*time.Hour), storyID); err != nil {
		t.Fatal(err)
	}
	story, err = biz.NewStoriesUseCase(data.NewStoriesRepo(store)).Get(t.Context(), storyID)
	if err != nil || story.Discussion != nil {
		t.Fatal("stale cache exposed")
	}
	// A failed cache query must not take the already-published event offline.
	if _, err := testDB(t, store).ExecContext(t.Context(), "ALTER TABLE story_discussions RENAME TO unavailable_discussion_cache"); err != nil {
		t.Fatal(err)
	}
	story, err = biz.NewStoriesUseCase(data.NewStoriesRepo(store)).Get(t.Context(), storyID)
	if err != nil || len(story.Items) != 1 || story.Discussion != nil {
		t.Fatalf("cache failure leaked to story: %v", err)
	}
}

func TestDiscussionLockExcludesConcurrentRefreshAndCancellationReleases(t *testing.T) {
	store, query, _ := discussionStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- data.NewDiscussionsRepo(store).RefreshDue(ctx, query, func(ctx context.Context, _ biz.DiscussionTarget, _ biz.DiscussionState) (biz.DiscussionState, error) {
			close(started)
			<-ctx.Done()
			return biz.DiscussionState{}, ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not start")
	}
	if err := data.NewDiscussionsRepo(store).RefreshDue(t.Context(), query, func(context.Context, biz.DiscussionTarget, biz.DiscussionState) (biz.DiscussionState, error) {
		t.Error("second instance acquired discussion lock")
		return biz.DiscussionState{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not release refresh")
	}
	var count int
	if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT count(*) FROM story_discussions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cancelled write: %d %v", count, err)
	}
	invoked := false
	if err := data.NewDiscussionsRepo(store).RefreshDue(t.Context(), query, func(_ context.Context, target biz.DiscussionTarget, _ biz.DiscussionState) (biz.DiscussionState, error) {
		invoked = true
		return discussionState(target, query.Now), nil
	}); err != nil || !invoked {
		t.Fatalf("lock not released: %v", err)
	}
}

func TestDiscussionCommitRechecksURLSnapshotAndEligibility(t *testing.T) {
	for _, change := range []string{"URL", "selected", "summary", "fact", "pool"} {
		t.Run(change, func(t *testing.T) {
			store, query, storyID := discussionStore(t)
			if err := data.NewDiscussionsRepo(store).RefreshDue(t.Context(), query, func(_ context.Context, target biz.DiscussionTarget, _ biz.DiscussionState) (biz.DiscussionState, error) {
				sql := "UPDATE items SET url='https://example.test/changed' WHERE story_id=$1"
				switch change {
				case "selected":
					sql = "UPDATE items SET selected=false WHERE story_id=$1"
				case "summary":
					sql = "UPDATE items SET summary='Different release' WHERE story_id=$1"
				case "fact":
					sql = `UPDATE items SET fact='{"title":"Changed release","subject":"OpenAI","action":"发布","object":"GPT-4.5","occurred_at":null}'::jsonb WHERE story_id=$1`
				case "pool":
					sql = "UPDATE hn_discussion_pool SET fingerprint='changed' WHERE thread_id=10 AND $1>0"
				}
				if _, err := testDB(t, store).ExecContext(t.Context(), sql, storyID); err != nil {
					t.Fatal(err)
				}
				return discussionState(target, query.Now), nil
			}); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT count(*) FROM story_discussions").Scan(&count); err != nil || count != 0 {
				t.Fatalf("stale result committed: %d %v", count, err)
			}
		})
	}
}

func TestDiscussionFailureIsScheduledWithoutLosingConfirmedCache(t *testing.T) {
	store, query, storyID := discussionStore(t)
	repo := data.NewDiscussionsRepo(store)
	var originalFingerprint string
	if err := repo.RefreshDue(t.Context(), query, func(_ context.Context, target biz.DiscussionTarget, _ biz.DiscussionState) (biz.DiscussionState, error) {
		state := discussionState(target, query.Now)
		originalFingerprint = state.Fingerprint
		return state, nil
	}); err != nil {
		t.Fatal(err)
	}
	query.Now = query.Now.Add(time.Hour)
	if err := repo.RefreshDue(t.Context(), query, func(_ context.Context, _ biz.DiscussionTarget, saved biz.DiscussionState) (biz.DiscussionState, error) {
		saved.CheckedAt, saved.NextRefreshAt, saved.LastError = query.Now, query.Now.Add(30*time.Minute), "fetch HN comments failed"
		return saved, errors.New("temporary upstream failure")
	}); err == nil {
		t.Fatal("refresh failure was swallowed")
	}
	var fingerprint, lastError string
	var next time.Time
	if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT fingerprint,last_error,next_refresh_at FROM story_discussions WHERE story_id=$1", storyID).Scan(&fingerprint, &lastError, &next); err != nil {
		t.Fatal(err)
	}
	if fingerprint != originalFingerprint || lastError != "fetch HN comments failed" || !next.Equal(query.Now.Add(30*time.Minute)) {
		t.Fatal("failure lost cache or retry schedule")
	}
}

type integrationDiscussionModel struct{ calls int }

func (m *integrationDiscussionModel) MatchDiscussion(_ context.Context, input biz.DiscussionMatchInput) (biz.DiscussionMatch, error) {
	m.calls++
	candidate := input.Candidates[0]
	item := candidate.Items[0]
	return biz.DiscussionMatch{Decision: biz.DiscussionMatchSame, StoryID: candidate.StoryID, Evidence: []biz.DiscussionMatchEvidence{
		{Role: "subject", HNField: "title", HNQuote: "OpenAI", ItemID: item.ID, EventField: "fact.subject", EventQuote: "OpenAI"},
		{Role: "action", HNField: "title", HNQuote: "releases", ItemID: item.ID, EventField: "fact.action", EventQuote: "发布"},
		{Role: "object", HNField: "title", HNQuote: "GPT-4.1", ItemID: item.ID, EventField: "fact.object", EventQuote: "GPT-4.1"},
		{Role: "anchor", HNField: "title", HNQuote: "GPT-4.1", ItemID: item.ID, EventField: "fact.object", EventQuote: "GPT-4.1"},
	}}, nil
}

func (m *integrationDiscussionModel) SummarizeDiscussion(context.Context, biz.DiscussionInput) ([]biz.DiscussionPoint, error) {
	return []biz.DiscussionPoint{{Text: "用户讨论实际体验。", CommentID: 11}}, nil
}

func TestHNPoolAssociatesAfterEventArrives(t *testing.T) {
	store, query, storyID := discussionStore(t) // Pool was collected before any event existed.
	if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET original_title='OpenAI releases GPT-4.1',fact='{"title":"OpenAI发布GPT-4.1","subject":"OpenAI","action":"发布","object":"GPT-4.1","occurred_at":null}'::jsonb WHERE story_id=$1`, storyID); err != nil {
		t.Fatal(err)
	}
	model := &integrationDiscussionModel{}
	fetcher := integrationHNFetcher{thread: biz.HNThread{ID: 10, URL: "https://example.test/1", Title: "OpenAI releases GPT-4.1", PublishedAt: query.Now}}
	u := biz.NewDiscussionsUseCase(data.NewDiscussionsRepo(store), fetcher, model, model, biz.DiscussionOptions{Enabled: true, RefreshInterval: 30 * time.Minute}, func() time.Time { return query.Now })
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	story, err := biz.NewStoriesUseCase(data.NewStoriesRepo(store)).Get(t.Context(), storyID)
	if err != nil || story.Discussion == nil || story.Discussion.SampleSize != 2 || model.calls != 1 {
		t.Fatalf("story=%+v err=%v matches=%d", story, err, model.calls)
	}
}

func TestDiscussionReadInvalidatesChangedFactsAndPool(t *testing.T) {
	for _, change := range []string{"fact", "pool"} {
		t.Run(change, func(t *testing.T) {
			store, query, storyID := discussionStore(t)
			if err := data.NewDiscussionsRepo(store).RefreshDue(t.Context(), query, func(_ context.Context, target biz.DiscussionTarget, _ biz.DiscussionState) (biz.DiscussionState, error) {
				return discussionState(target, query.Now), nil
			}); err != nil {
				t.Fatal(err)
			}
			sql := "UPDATE items SET summary='Different occurrence' WHERE story_id=$1"
			if change == "pool" {
				sql = "UPDATE hn_discussion_pool SET fingerprint='new-sample' WHERE thread_id=10 AND $1>0"
			}
			if _, err := testDB(t, store).ExecContext(t.Context(), sql, storyID); err != nil {
				t.Fatal(err)
			}
			story, err := biz.NewStoriesUseCase(data.NewStoriesRepo(store)).Get(t.Context(), storyID)
			if err != nil || story.Discussion != nil {
				t.Fatalf("stale association visible: %+v err=%v", story.Discussion, err)
			}
		})
	}
}

func TestHNCollectionPersistsPartialSuccessWithoutEvents(t *testing.T) {
	store, now := groupStore(t)
	query := biz.DiscussionQuery{Since: now.Add(-biz.DiscussionWindow), Now: now, Limit: 50}
	threads := []biz.HNThread{{ID: 1, URL: "https://example.test/first", Title: "First", PublishedAt: now}, {ID: 2, URL: "https://example.test/second", Title: "Second", PublishedAt: now}}
	upstream := errors.New("temporary upstream failure")
	err := data.NewDiscussionsRepo(store).Collect(t.Context(), query, func(context.Context) ([]biz.HNThread, error) { return threads, nil }, func(_ context.Context, state biz.HNPoolState) (biz.HNPoolState, error) {
		state.CheckedAt, state.NextRefreshAt = now, now.Add(30*time.Minute)
		if state.Input.Thread.ID == 1 {
			state.LastError = "fetch HN comments failed"
			return state, upstream
		}
		state.ConfirmedAt = now
		state.Input.Comments = []biz.HNComment{{ID: 21, ParentID: 2, Text: "actual bounded sample"}}
		return state, nil
	})
	if !errors.Is(err, upstream) {
		t.Fatalf("lost upstream failure: %v", err)
	}
	var total, confirmed, stories int
	if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT count(*),count(confirmed_at),(SELECT count(*) FROM stories) FROM hn_discussion_pool").Scan(&total, &confirmed, &stories); err != nil || total != 2 || confirmed != 1 || stories != 0 {
		t.Fatalf("total=%d confirmed=%d stories=%d err=%v", total, confirmed, stories, err)
	}
	if err := data.NewDiscussionsRepo(store).Collect(t.Context(), query, func(context.Context) ([]biz.HNThread, error) { return nil, nil }, func(context.Context, biz.HNPoolState) (biz.HNPoolState, error) {
		t.Error("sample retried before due time")
		return biz.HNPoolState{}, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHNCollectionSessionLockAndCancellation(t *testing.T) {
	store, now := groupStore(t)
	query := biz.DiscussionQuery{Since: now.Add(-biz.DiscussionWindow), Now: now, Limit: 50}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- data.NewDiscussionsRepo(store).Collect(ctx, query, func(ctx context.Context) ([]biz.HNThread, error) { close(started); <-ctx.Done(); return nil, ctx.Err() }, nil)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("collection did not start")
	}
	if err := data.NewDiscussionsRepo(store).Collect(t.Context(), query, func(context.Context) ([]biz.HNThread, error) {
		t.Error("another process entered discovery while collection lock held")
		return nil, nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel error=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not release discovery")
	}
	called := false
	if err := data.NewDiscussionsRepo(store).Collect(t.Context(), query, func(context.Context) ([]biz.HNThread, error) { called = true; return nil, nil }, nil); err != nil || !called {
		t.Fatalf("collection lock leaked: called=%v err=%v", called, err)
	}
}

func TestDiscussionAssociationLimitExcludesUnqualifiedPoolEntries(t *testing.T) {
	store, query, _ := discussionStore(t)
	// More than a full association page of newer submissions have no usable sample:
	// unconfirmed, empty, one comment, expired confirmation, or future confirmation.
	_, err := testDB(t, store).ExecContext(t.Context(), `
 INSERT INTO hn_discussion_pool(thread_id,snapshot,fingerprint,published_at,confirmed_at,next_refresh_at)
 SELECT 1000+n,
        jsonb_build_object('comments',CASE n%5
          WHEN 1 THEN '[]'::jsonb
          WHEN 2 THEN '[{"id":1}]'::jsonb
          ELSE '[{"id":1},{"id":2}]'::jsonb END),
        'fixture', $1::timestamptz,
        CASE n%5 WHEN 0 THEN NULL
          WHEN 3 THEN $2::timestamptz
          WHEN 4 THEN $3::timestamptz
          ELSE $1::timestamptz END,
        $1::timestamptz
 FROM generate_series(1,$4::integer) AS n`, query.Now, query.Now.Add(-biz.DiscussionFreshness), query.Now.Add(time.Hour), biz.DiscussionPoolLimit+1)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = data.NewDiscussionsRepo(store).RefreshDue(t.Context(), query, func(_ context.Context, target biz.DiscussionTarget, _ biz.DiscussionState) (biz.DiscussionState, error) {
		calls++
		if len(target.Pool) != 1 || target.Pool[0].Input.Thread.ID != 10 {
			t.Fatalf("qualified submission starved by unusable entries: pool=%+v", target.Pool)
		}
		return discussionState(target, query.Now), nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("association callback count=%d err=%v", calls, err)
	}
}
