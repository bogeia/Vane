package biz

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bogeia/vane/internal/infra/retry"
)

type DiscussionsUseCase struct {
	repo       DiscussionsRepository
	fetcher    HNDiscussionFetcher
	summarizer DiscussionSummarizer
	matcher    DiscussionMatcher
	options    DiscussionOptions
	now        func() time.Time
}

func NewDiscussionsUseCase(repo DiscussionsRepository, fetcher HNDiscussionFetcher, summarizer DiscussionSummarizer, matcher DiscussionMatcher, options DiscussionOptions, now func() time.Time) *DiscussionsUseCase {
	return &DiscussionsUseCase{repo: repo, fetcher: fetcher, summarizer: summarizer, matcher: matcher, options: options, now: now}
}

func (u *DiscussionsUseCase) RefreshInterval() time.Duration { return u.options.RefreshInterval }

func (u *DiscussionsUseCase) RunOnce(ctx context.Context) error {
	if !u.options.Enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, u.options.RefreshInterval)
	defer cancel()
	now := u.now()
	query := DiscussionQuery{Since: now.Add(-DiscussionWindow), Now: now, Limit: DiscussionEventLimit, DiscoveryLimit: DiscussionDiscoveryLimit, CollectionLimit: DiscussionCollectionLimit, PoolLimit: DiscussionPoolLimit, CandidateItemLimit: DiscussionCandidateItemLimit}
	collectErr := u.repo.Collect(ctx, query, func(ctx context.Context) ([]HNThread, error) {
		threads, err := u.fetcher.Recent(ctx, query.Since)
		valid := make([]HNThread, 0, min(len(threads), DiscussionDiscoveryLimit))
		for _, thread := range threads {
			if len(valid) == DiscussionDiscoveryLimit {
				break
			}
			if !utf8.ValidString(thread.Title) || strings.ContainsRune(thread.Title, '\x00') || thread.ID <= 0 || thread.PublishedAt.Before(query.Since) || thread.PublishedAt.After(u.now()) {
				continue
			}
			if _, urlErr := NormalizeDiscussionURL(thread.URL); urlErr != nil {
				continue
			}
			thread.Title = truncate(strings.TrimSpace(thread.Title), 256)
			thread.PublishedAt = thread.PublishedAt.UTC()
			valid = append(valid, thread)
		}
		return valid, err
	}, func(ctx context.Context, saved HNPoolState) (HNPoolState, error) {
		saved.CheckedAt = u.now()
		saved.NextRefreshAt = saved.CheckedAt.Add(u.options.RefreshInterval)
		comments, err := u.fetcher.Comments(ctx, saved.Input.Thread)
		if err != nil {
			saved.LastError = "fetch HN comments failed"
			saved.NextRefreshAt = maxDiscussionTime(saved.NextRefreshAt, retry.NotBefore(err))
			return saved, err
		}
		saved.Input.Comments = discussionComments(saved.Input.Thread.ID, comments)
		saved.Fingerprint, err = discussionFingerprint(saved.Input)
		if err != nil {
			return saved, err
		}
		saved.ConfirmedAt = u.now()
		saved.LastError = ""
		return saved, nil
	})
	if ctx.Err() != nil {
		return errors.Join(collectErr, ctx.Err())
	}
	query.Now = u.now()
	type decision struct {
		match DiscussionMatch
		err   error
	}
	matches := map[int64]decision{}
	matchCalls := 0
	associationErr := u.repo.RefreshDue(ctx, query, func(ctx context.Context, target DiscussionTarget, saved DiscussionState) (DiscussionState, error) {
		saved.CheckedAt = u.now()
		saved.NextRefreshAt = saved.CheckedAt.Add(u.options.RefreshInterval)
		saved.LastError = ""
		previous := saved
		// Invalidate before attempting a fresh association; failed checks never confirm old evidence.
		saved.Points = nil
		saved.Fingerprint = ""
		saved.ConfirmedAt = time.Time{}
		saved.TargetSignature = target.Signature
		saved.RuleVersion = DiscussionMatchRuleVersion
		var chosen *HNPoolState
		verified := map[int64]bool{}
		var failures error
		for i := range target.Pool {
			pool := &target.Pool[i]
			if len(pool.Input.Comments) < DiscussionCommentMinimum || pool.ConfirmedAt.IsZero() || pool.ConfirmedAt.After(u.now()) || u.now().Sub(pool.ConfirmedAt) >= DiscussionFreshness {
				continue
			}
			input := DiscussionMatchInput{Thread: pool.Input.Thread, Candidates: target.Candidates}
			signature := DiscussionMatchSignature(input)
			result, ok := matches[pool.Input.Thread.ID]
			if !ok {
				for _, cached := range pool.MatchCache {
					if cached.Signature == signature {
						result = decision{match: cached.Match}
						ok = true
						break
					}
				}
			}
			if !ok && pool.MatchSignature == signature && pool.MatchError == "" {
				result = decision{match: pool.Match}
				ok = true
			}
			if !ok && pool.MatchError != "" && pool.MatchNextAt.After(u.now()) {
				continue
			}
			if !ok {
				if matchCalls >= DiscussionMatchLimit {
					continue
				}
				matchCalls++
				result.match, result.err = u.matcher.MatchDiscussion(ctx, input)
				if result.err == nil {
					result.match, result.err = ValidateDiscussionMatch(input, result.match)
				}
				matches[pool.Input.Thread.ID] = result
				pool.MatchSignature = signature
				pool.Match = result.match
				pool.MatchError = ""
				pool.MatchNextAt = u.now()
				if result.err == nil {
					pool.MatchCache = append(pool.MatchCache, DiscussionCachedMatch{Signature: signature, Match: result.match})
					if len(pool.MatchCache) > DiscussionMatchCacheLimit {
						pool.MatchCache = pool.MatchCache[len(pool.MatchCache)-DiscussionMatchCacheLimit:]
					}
				}
				if result.err != nil {
					pool.MatchError = "match HN discussion failed"
					pool.MatchNextAt = maxDiscussionTime(now.Add(u.options.RefreshInterval), retry.NotBefore(result.err))
				}
			}
			if result.err != nil {
				failures = errors.Join(failures, result.err)
				saved.NextRefreshAt = maxDiscussionTime(saved.NextRefreshAt, retry.NotBefore(result.err))
				continue
			}
			verified[pool.Input.Thread.ID] = true
			if result.match.Decision == DiscussionMatchSame && result.match.StoryID == target.StoryID {
				if chosen == nil || pool.Input.Thread.PublishedAt.After(chosen.Input.Thread.PublishedAt) {
					chosen = pool
				}
			}
		}
		if chosen == nil {
			// A deferred check does not negate previously verified, unchanged evidence.
			if !verified[previous.Input.Thread.ID] {
				for _, pool := range target.Pool {
					if pool.Input.Thread.ID == previous.Input.Thread.ID && VisibleDiscussion(previous, target, pool, u.now()) != nil {
						checked, next := saved.CheckedAt, saved.NextRefreshAt
						saved = previous
						saved.CheckedAt = checked
						saved.NextRefreshAt = next
						if failures != nil {
							saved.LastError = "match HN discussion failed"
						}
						return saved, failures
					}
				}
			}
			if failures != nil {
				saved.LastError = "match HN discussion failed"
			}
			return saved, failures
		}
		saved.Input = chosen.Input
		saved.PoolFingerprint = chosen.Fingerprint
		if previous.TargetSignature == target.Signature && previous.RuleVersion == DiscussionMatchRuleVersion && previous.Fingerprint == chosen.Fingerprint && previous.PoolFingerprint == chosen.Fingerprint {
			saved.Points = previous.Points
			saved.Fingerprint = chosen.Fingerprint
			saved.UpdatedAt = previous.UpdatedAt
			saved.ConfirmedAt = chosen.ConfirmedAt
			return saved, failures
		}
		var points []DiscussionPoint
		var err error
		if len(saved.Input.Comments) >= DiscussionCommentMinimum {
			points, err = u.summarizer.SummarizeDiscussion(ctx, saved.Input)
			if err == nil {
				points, err = NormalizeDiscussionPoints(saved.Input, points)
			}
		}
		if err != nil {
			saved.LastError = "summarize HN discussions failed"
			saved.NextRefreshAt = maxDiscussionTime(saved.NextRefreshAt, retry.NotBefore(err))
			return saved, errors.Join(failures, err)
		}
		if err := ctx.Err(); err != nil {
			return saved, err
		}
		saved.Points = points
		saved.Fingerprint = chosen.Fingerprint
		saved.UpdatedAt = u.now()
		saved.ConfirmedAt = chosen.ConfirmedAt
		return saved, failures
	})
	return errors.Join(collectErr, associationErr)
}

func discussionComments(threadID int64, comments []HNComment) []HNComment {
	result := make([]HNComment, 0, DiscussionCommentLimit)
	seen := map[int64]bool{}
	for _, comment := range comments {
		if comment.ID <= 0 || comment.ParentID != threadID || seen[comment.ID] || !utf8.ValidString(comment.Text) || strings.ContainsRune(comment.Text, '\x00') {
			continue
		}
		comment.Text = strings.Join(strings.Fields(comment.Text), " ")
		if comment.Text == "" {
			continue
		}
		text := []rune(comment.Text)
		comment.Text = string(text[:min(len(text), DiscussionCommentRunes)])
		result = append(result, comment)
		seen[comment.ID] = true
		if len(result) == DiscussionCommentLimit {
			break
		}
	}
	return result
}

func discussionFingerprint(input DiscussionInput) (string, error) {
	type comment struct {
		ID   int64
		Text string
	}
	material := struct {
		Thread   HNThread
		Comments []comment
	}{Thread: input.Thread, Comments: make([]comment, 0, len(input.Comments))}
	for _, entry := range input.Comments {
		material.Comments = append(material.Comments, comment{ID: entry.ID, Text: entry.Text})
	}
	content, err := json.Marshal(material)
	if err != nil {
		return "", err
	}
	// Include both prompt and bounded-input semantics; refreshing time is deliberately absent.
	return fmt.Sprintf("hn-discussion-v3:%x", sha256.Sum256(content)), nil
}

func NormalizeDiscussionPoints(input DiscussionInput, points []DiscussionPoint) ([]DiscussionPoint, error) {
	if len(points) > 3 {
		return nil, errors.New("too many discussion points")
	}
	seen := map[string]bool{}
	result := make([]DiscussionPoint, 0, len(points))
	for _, point := range points {
		point.Text = strings.TrimSpace(point.Text)
		hasChinese := strings.ContainsFunc(point.Text, func(r rune) bool { return unicode.Is(unicode.Han, r) })
		if point.Text == "" || !hasChinese || !utf8.ValidString(point.Text) || strings.ContainsRune(point.Text, '\x00') || utf8.RuneCountInString(point.Text) > DiscussionPointRunes || seen[point.Text] || !slices.ContainsFunc(input.Comments, func(comment HNComment) bool { return comment.ID == point.CommentID }) {
			return nil, errors.New("invalid discussion point or citation")
		}
		seen[point.Text] = true
		result = append(result, point)
	}
	return result, nil
}

// VisibleDiscussion applies the publication boundary to the persisted cache.
func VisibleDiscussion(state DiscussionState, target DiscussionTarget, pool HNPoolState, now time.Time) *DiscussionSummary {
	if len(state.Points) == 0 || len(state.Input.Comments) < DiscussionCommentMinimum || state.Input.Thread.ID <= 0 || state.ConfirmedAt.IsZero() || now.Sub(state.ConfirmedAt) >= DiscussionFreshness || state.ConfirmedAt.After(now) {
		return nil
	}
	if !slices.Equal(sortedDiscussionURLs(state.URLs), sortedDiscussionURLs(target.URLs)) {
		return nil
	}
	if state.RuleVersion != DiscussionMatchRuleVersion || state.TargetSignature == "" || state.TargetSignature != target.Signature || state.PoolFingerprint == "" || state.PoolFingerprint != pool.Fingerprint || pool.ConfirmedAt.IsZero() || pool.ConfirmedAt.After(now) || now.Sub(pool.ConfirmedAt) >= DiscussionFreshness {
		return nil
	}
	poolFingerprint, poolErr := discussionFingerprint(pool.Input)
	if poolErr != nil || poolFingerprint != pool.Fingerprint {
		return nil
	}
	fingerprint, err := discussionFingerprint(state.Input)
	if err != nil || fingerprint != state.Fingerprint || fingerprint != pool.Fingerprint {
		return nil
	}
	points, err := NormalizeDiscussionPoints(state.Input, state.Points)
	if err != nil {
		return nil
	}
	return &DiscussionSummary{Thread: state.Input.Thread, SampleSize: len(state.Input.Comments), UpdatedAt: state.UpdatedAt, Points: points}
}

func sortedDiscussionURLs(urls []string) []string {
	result := slices.Clone(urls)
	slices.SortFunc(result, cmp.Compare[string])
	return slices.Compact(result)
}

func maxDiscussionTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// NormalizeDiscussionURL matches discussion links without changing collection URL identity.
func NormalizeDiscussionURL(raw string) (string, error) {
	normalized, err := NormalizeItemURL(raw)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	if (parsed.Scheme == "http" && parsed.Port() == "80") || (parsed.Scheme == "https" && parsed.Port() == "443") {
		parsed.Host = parsed.Hostname()
		if strings.Contains(parsed.Host, ":") {
			parsed.Host = "[" + parsed.Host + "]"
		}
	}
	return parsed.String(), nil
}
