package biz

import (
	"context"
	"time"
)

// HNThread is an independently collected submitted external link.
type HNThread struct {
	ID          int64
	URL         string
	Title       string
	PublishedAt time.Time
}

// HNComment contains plain text from one live top-level comment.
type HNComment struct {
	ID       int64
	ParentID int64
	Text     string
}

type DiscussionInput struct {
	Thread   HNThread
	Comments []HNComment
}

type DiscussionPoint struct {
	Text      string
	CommentID int64
}

type DiscussionSummary struct {
	Thread     HNThread
	SampleSize int
	UpdatedAt  time.Time
	Points     []DiscussionPoint
}

type HNDiscussionFetcher interface {
	Recent(context.Context, time.Time) ([]HNThread, error)
	Comments(context.Context, HNThread) ([]HNComment, error)
}

type DiscussionSummarizer interface {
	SummarizeDiscussion(context.Context, DiscussionInput) ([]DiscussionPoint, error)
}

const (
	DiscussionWindow             = 7 * 24 * time.Hour
	DiscussionFreshness          = 24 * time.Hour
	DiscussionEventLimit         = 50
	DiscussionCommentLimit       = 10
	DiscussionCommentMinimum     = 2
	DiscussionCommentRunes       = 1200
	DiscussionPointRunes         = 240
	DiscussionDiscoveryLimit     = 100
	DiscussionCollectionLimit    = 24
	DiscussionPoolLimit          = 500
	DiscussionMatchLimit         = 24
	DiscussionCandidateItemLimit = 3
	DiscussionMatchCacheLimit    = 16
)

type DiscussionOptions struct {
	Enabled         bool
	RefreshInterval time.Duration
}

type DiscussionQuery struct {
	Since              time.Time
	Now                time.Time
	Limit              int
	DiscoveryLimit     int
	CollectionLimit    int
	PoolLimit          int
	CandidateItemLimit int
	PoolConfirmedAfter time.Time
	PoolMinComments    int
}

type DiscussionTarget struct {
	StoryID    int64
	URLs       []string
	Signature  string
	Candidates []DiscussionCandidate
	Pool       []HNPoolState
}

// DiscussionState keeps successful input and citations separate from failed checks.
type DiscussionState struct {
	URLs            []string
	TargetSignature string
	RuleVersion     string
	PoolFingerprint string
	Input           DiscussionInput
	Fingerprint     string
	Points          []DiscussionPoint
	UpdatedAt       time.Time
	ConfirmedAt     time.Time
	CheckedAt       time.Time
	NextRefreshAt   time.Time
	LastError       string
}

type DiscussionsRepository interface {
	Collect(context.Context, DiscussionQuery, func(context.Context) ([]HNThread, error), func(context.Context, HNPoolState) (HNPoolState, error)) error
	RefreshDue(context.Context, DiscussionQuery, func(context.Context, DiscussionTarget, DiscussionState) (DiscussionState, error)) error
}

// HNPoolState survives independently of whether any selected event exists.
type DiscussionCachedMatch struct {
	Signature string
	Match     DiscussionMatch
}

type HNPoolState struct {
	MatchCache     []DiscussionCachedMatch
	MatchSignature string
	Match          DiscussionMatch
	MatchNextAt    time.Time
	MatchError     string
	Input          DiscussionInput
	Fingerprint    string
	ConfirmedAt    time.Time
	CheckedAt      time.Time
	NextRefreshAt  time.Time
	LastError      string
}

func NormalizeDiscussionQuery(query DiscussionQuery) DiscussionQuery {
	query.DiscoveryLimit = DiscussionDiscoveryLimit
	query.CollectionLimit = DiscussionCollectionLimit
	query.PoolLimit = DiscussionPoolLimit
	query.CandidateItemLimit = DiscussionCandidateItemLimit
	query.PoolConfirmedAfter = query.Now.Add(-DiscussionFreshness)
	query.PoolMinComments = DiscussionCommentMinimum
	return query
}

func DiscussionQueryAt(now time.Time) DiscussionQuery {
	return NormalizeDiscussionQuery(DiscussionQuery{Since: now.Add(-DiscussionWindow), Now: now, Limit: DiscussionEventLimit})
}

// DiscoveredHNState invalidates evidence only when the submitted link changes.
func DiscoveredHNState(thread HNThread, saved HNPoolState, now time.Time) (HNPoolState, bool) {
	if saved.Input.Thread == thread {
		return saved, false
	}
	next := HNPoolState{Input: DiscussionInput{Thread: thread}, NextRefreshAt: now}
	if saved.LastError != "" {
		next.LastError = saved.LastError
		next.NextRefreshAt = maxDiscussionTime(now, saved.NextRefreshAt)
	}
	if saved.MatchError != "" {
		next.MatchError = saved.MatchError
		next.MatchNextAt = saved.MatchNextAt
	}
	return next, true
}

// DiscussionRead contains persisted evidence needed by the public reading policy.
// It is never included in HTTP DTOs or frozen report snapshots.
type DiscussionRead struct {
	State  DiscussionState
	Target DiscussionTarget
	Pool   HNPoolState
	ReadAt time.Time
}

func (r *DiscussionRead) Summary() *DiscussionSummary {
	if r == nil {
		return nil
	}
	return VisibleDiscussion(r.State, r.Target, r.Pool, r.ReadAt)
}
