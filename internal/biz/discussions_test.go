package biz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type discussionRepoFake struct {
	targets []DiscussionTarget
	states  map[int64]DiscussionState
	pool    []HNPoolState
}

func (r *discussionRepoFake) Collect(ctx context.Context, q DiscussionQuery, discover func(context.Context) ([]HNThread, error), sample func(context.Context, HNPoolState) (HNPoolState, error)) error {
	threads, err := discover(ctx)
	for _, thread := range threads {
		found := false
		for _, saved := range r.pool {
			if saved.Input.Thread.ID == thread.ID {
				found = true
			}
		}
		if !found {
			r.pool = append(r.pool, HNPoolState{Input: DiscussionInput{Thread: thread}})
		}
	}
	for i, saved := range r.pool {
		next, sampleErr := sample(ctx, saved)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.pool[i] = next
		err = errors.Join(err, sampleErr)
	}
	return err
}

func (r *discussionRepoFake) RefreshDue(ctx context.Context, q DiscussionQuery, refresh func(context.Context, DiscussionTarget, DiscussionState) (DiscussionState, error)) error {
	var candidates []DiscussionCandidate
	for _, target := range r.targets {
		candidates = append(candidates, target.Candidates...)
	}
	var failures error
	for _, target := range r.targets {
		target.Pool = r.pool
		target.Candidates = candidates
		next, err := refresh(ctx, target, r.states[target.StoryID])
		if ctx.Err() != nil {
			return ctx.Err()
		}
		next.URLs = target.URLs
		r.states[target.StoryID] = next
		failures = errors.Join(failures, err)
	}
	return failures
}

type discussionFetcherFake struct {
	threads       []HNThread
	comments      []HNComment
	recentErr     error
	commentErrors map[int64]error
	advance       func()
}

func (f *discussionFetcherFake) Recent(context.Context, time.Time) ([]HNThread, error) {
	return f.threads, f.recentErr
}

func (f *discussionFetcherFake) Comments(_ context.Context, thread HNThread) ([]HNComment, error) {
	if f.advance != nil {
		f.advance()
	}
	if err := f.commentErrors[thread.ID]; err != nil {
		return nil, err
	}
	return f.comments, nil
}

type discussionModelFake struct {
	calls      int
	matchCalls int
	err        error
	decision   DiscussionMatchDecision
	cancel     context.CancelFunc
}

func (m *discussionModelFake) SummarizeDiscussion(_ context.Context, input DiscussionInput) ([]DiscussionPoint, error) {
	m.calls++
	if m.cancel != nil {
		m.cancel()
	}
	return []DiscussionPoint{{Text: "有用户分享了实际使用体验。", CommentID: input.Comments[0].ID}}, m.err
}

func (m *discussionModelFake) MatchDiscussion(_ context.Context, input DiscussionMatchInput) (DiscussionMatch, error) {
	m.matchCalls++
	if m.decision != DiscussionMatchSame {
		return DiscussionMatch{Decision: m.decision}, nil
	}
	if len(input.Candidates) == 0 {
		return DiscussionMatch{Decision: DiscussionMatchUnknown}, nil
	}
	item := input.Candidates[0].Items[0]
	match := DiscussionMatch{Decision: DiscussionMatchSame, StoryID: input.Candidates[0].StoryID}
	for _, entry := range []struct{ role, quote string }{{"subject", "OpenAI"}, {"action", "releases"}, {"object", "GPT-4.1"}, {"anchor", "GPT-4.1"}} {
		match.Evidence = append(match.Evidence, DiscussionMatchEvidence{Role: entry.role, HNField: "title", HNQuote: entry.quote, ItemID: item.ID, EventField: "original_title", EventQuote: entry.quote})
	}
	return match, nil
}

func discussionFixture() (*DiscussionsUseCase, *discussionRepoFake, *discussionFetcherFake, *discussionModelFake, *time.Time) {
	now := time.Date(2026, 11, 3, 2, 0, 0, 0, time.UTC)
	candidate := DiscussionCandidate{StoryID: 1, Items: []DiscussionCandidateItem{{ID: 9, URL: "https://example.com/release", OriginalTitle: "OpenAI releases GPT-4.1", Summary: "OpenAI发布GPT-4.1。", PublishedAt: now, Fact: Fact{Subject: "OpenAI", Action: "发布", Object: "GPT-4.1"}}}}
	repo := &discussionRepoFake{states: map[int64]DiscussionState{}, targets: []DiscussionTarget{{StoryID: 1, URLs: []string{"https://example.com/release"}, Signature: "event-v1", Candidates: []DiscussionCandidate{candidate}}}}
	fetcher := &discussionFetcherFake{threads: []HNThread{{ID: 5, URL: "https://other.example/openai-gpt-4.1", Title: "OpenAI releases GPT-4.1", PublishedAt: now.Add(-time.Hour)}}, comments: []HNComment{{ID: 6, ParentID: 5, Text: "Experience one"}, {ID: 7, ParentID: 5, Text: "Experience two"}}}
	model := &discussionModelFake{decision: DiscussionMatchSame}
	u := NewDiscussionsUseCase(repo, fetcher, model, model, DiscussionOptions{Enabled: true, RefreshInterval: 30 * time.Minute}, func() time.Time { return now })
	return u, repo, fetcher, model, &now
}

func TestDiscussionCollectsWithoutEventsAndAssociatesLater(t *testing.T) {
	u, repo, _, model, _ := discussionFixture()
	targets := repo.targets
	repo.targets = nil
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(repo.pool) != 1 || len(repo.pool[0].Input.Comments) != 2 || model.matchCalls != 0 {
		t.Fatal("independent collection failed")
	}
	repo.targets = targets
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(repo.states[1].Points) != 1 || model.matchCalls != 1 {
		t.Fatal("persisted thread was not associated across different URLs")
	}
}

func TestDiscussionFreshCollectionUsesCompletionClockAndCachesModels(t *testing.T) {
	u, repo, fetcher, model, now := discussionFixture()
	fetcher.advance = func() { *now = now.Add(time.Second) }
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	first := repo.states[1]
	if len(first.Points) != 1 || !first.ConfirmedAt.Equal(*now) {
		t.Fatal("freshly collected input skipped or timestamp fabricated")
	}
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || model.matchCalls != 1 || !repo.states[1].ConfirmedAt.Equal(*now) {
		t.Fatal("unchanged evidence reran models or lost collection confirmation")
	}
	fetcher.comments[0].Text = "Edited comment"
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if model.calls != 2 || model.matchCalls != 1 {
		t.Fatal("comment edit must resummarize without reclassifying event")
	}
}

func TestDiscussionUnknownCachedUntilCandidatesChange(t *testing.T) {
	u, repo, _, model, _ := discussionFixture()
	model.decision = DiscussionMatchUnknown
	for range 2 {
		if err := u.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if model.matchCalls != 1 || model.calls != 0 || len(repo.states[1].Points) != 0 {
		t.Fatal("unknown must remain unmatched and cached")
	}
	repo.targets[0].Candidates[0].Items[0].Summary = "A changed event fact summary"
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if model.matchCalls != 2 {
		t.Fatal("changed candidates did not invalidate match cache")
	}
}

func TestDiscussionCollectionFailureDoesNotConfirmOrDiscardOtherThreads(t *testing.T) {
	u, repo, fetcher, _, now := discussionFixture()
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	confirmed := repo.pool[0].ConfirmedAt
	*now = now.Add(time.Hour)
	fetcher.threads = append(fetcher.threads, HNThread{ID: 20, URL: "https://example.com/other", Title: "Other story", PublishedAt: *now})
	fetcher.commentErrors = map[int64]error{5: errors.New("upstream failed")}
	if err := u.RunOnce(t.Context()); err == nil {
		t.Fatal("partial failure swallowed")
	}
	if !repo.pool[0].ConfirmedAt.Equal(confirmed) || repo.pool[1].ConfirmedAt.IsZero() {
		t.Fatal("failed sample confirmed or unrelated thread not saved")
	}
}

func TestDiscussionCancellationKeepsPoolAndDoesNotPublish(t *testing.T) {
	u, repo, _, model, _ := discussionFixture()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	model.cancel = cancel
	if err := u.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(repo.pool) != 1 || repo.pool[0].ConfirmedAt.IsZero() || repo.states[1].Fingerprint != "" {
		t.Fatal("pool lost or cancelled summary published")
	}
}

func TestVisibleDiscussionRejectsChangedEvidenceAndExpiredConfirmation(t *testing.T) {
	u, repo, _, _, now := discussionFixture()
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, target, pool := repo.states[1], repo.targets[0], repo.pool[0]
	if VisibleDiscussion(state, target, pool, *now) == nil {
		t.Fatal("valid result hidden")
	}
	changed := target
	changed.Signature = "event-v2"
	if VisibleDiscussion(state, changed, pool, *now) != nil {
		t.Fatal("changed same-URL facts exposed stale discussion")
	}
	pool.Fingerprint = "different"
	if VisibleDiscussion(state, target, pool, *now) != nil {
		t.Fatal("changed pool exposed stale discussion")
	}
	if VisibleDiscussion(state, target, repo.pool[0], now.Add(DiscussionFreshness)) != nil {
		t.Fatal("expired confirmation exposed")
	}
}

func TestDiscussionFingerprintIncludesThreadIdentityAndURL(t *testing.T) {
	input := DiscussionInput{Thread: HNThread{ID: 1, URL: "https://example.com/one", Title: "same"}}
	a, _ := discussionFingerprint(input)
	input.Thread.ID = 2
	b, _ := discussionFingerprint(input)
	input.Thread.URL = "https://example.com/two"
	c, _ := discussionFingerprint(input)
	if a == b || b == c {
		t.Fatal("different thread or URL shares fingerprint")
	}
}

func TestDiscussionCommentBoundsAndPointReferences(t *testing.T) {
	comments := []HNComment{{ID: 1, ParentID: 8, Text: "reply"}, {ID: 2, ParentID: 5, Text: "  "}, {ID: 3, ParentID: 5, Text: "  valid   text "}, {ID: 3, ParentID: 5, Text: "duplicate"}}
	for i := range 15 {
		comments = append(comments, HNComment{ID: int64(10 + i), ParentID: 5, Text: strings.Repeat("字", 1300)})
	}
	clean := discussionComments(5, comments)
	if len(clean) != 10 || clean[0].Text != "valid text" || len([]rune(clean[1].Text)) != 1200 {
		t.Fatal("comment validation or bounds changed")
	}
	for _, points := range [][]DiscussionPoint{{{Text: "越界引用", CommentID: 999}}, {{Text: "English only", CommentID: 3}}, {{Text: strings.Repeat("字", 241), CommentID: 3}}} {
		if _, err := NormalizeDiscussionPoints(DiscussionInput{Comments: clean}, points); err == nil {
			t.Fatal("invalid point accepted")
		}
	}
}

func TestVisibleDiscussionKeepsIdenticalPoolRefreshBeforeEventDue(t *testing.T) {
	u, repo, _, _, now := discussionFixture()
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, target, pool := repo.states[1], repo.targets[0], repo.pool[0]
	pool.ConfirmedAt = now.Add(time.Hour)
	if VisibleDiscussion(state, target, pool, pool.ConfirmedAt) == nil {
		t.Fatal("same pool contents became hidden only because collection confirmed them again")
	}
}

func TestDiscussionDeferredMatchKeepsVerifiedEvidenceButUnknownClears(t *testing.T) {
	u, repo, _, model, now := discussionFixture()
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	old := repo.states[1]
	repo.targets[0].Candidates = append(repo.targets[0].Candidates, DiscussionCandidate{StoryID: 2, Items: []DiscussionCandidateItem{{ID: 20, OriginalTitle: "Different release", Fact: Fact{Subject: "Other", Action: "发布", Object: "Other-2.0"}}}})
	repo.pool[0].MatchError = "retry later"
	repo.pool[0].MatchNextAt = now.Add(time.Hour)
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(repo.states[1].Points) == 0 || repo.states[1].Fingerprint != old.Fingerprint {
		t.Fatal("deferred check erased verified unchanged evidence")
	}
	repo.pool[0].MatchNextAt = *now
	model.decision = DiscussionMatchUnknown
	if err := u.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(repo.states[1].Points) != 0 {
		t.Fatal("verified unknown retained prior association")
	}
}

func TestDiscoveryChangeKeepsExternalRetryBoundaries(t *testing.T) {
	now := time.Now()
	thread := HNThread{ID: 1, Title: "Changed", URL: "https://example.com/new"}
	saved := HNPoolState{Input: DiscussionInput{Thread: HNThread{ID: 1}}, LastError: "rate limited", NextRefreshAt: now.Add(time.Hour), MatchError: "rate limited", MatchNextAt: now.Add(2 * time.Hour)}
	next, changed := DiscoveredHNState(thread, saved, now)
	if !changed || !next.NextRefreshAt.Equal(saved.NextRefreshAt) || !next.MatchNextAt.Equal(saved.MatchNextAt) || !next.ConfirmedAt.IsZero() {
		t.Fatal("changed submission bypassed upstream retry boundary or reused confirmation")
	}
}
