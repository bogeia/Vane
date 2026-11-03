package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/bogeia/vane/internal/biz"
)

const discussionLockNamespace = 484663

type discussionsRepo struct{ data *Data }

func NewDiscussionsRepo(data *Data) biz.DiscussionsRepository { return &discussionsRepo{data: data} }

// withLock holds a session lock, never a database transaction, during network work.
func (r *discussionsRepo) withLock(ctx context.Context, run func(*gorm.DB) error) error {
	pool, err := r.data.ORM.DB()
	if err != nil {
		return err
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1, 0)", discussionLockNamespace).Scan(&locked); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return err
	}
	if !locked {
		return nil
	}
	defer releaseDiscussionLock(conn)
	orm, err := openORM(conn)
	if err != nil {
		return err
	}
	return run(orm.WithContext(ctx))
}

func (r *discussionsRepo) Collect(ctx context.Context, query biz.DiscussionQuery, discover func(context.Context) ([]biz.HNThread, error), sample func(context.Context, biz.HNPoolState) (biz.HNPoolState, error)) error {
	query = biz.NormalizeDiscussionQuery(query)
	return r.withLock(ctx, func(orm *gorm.DB) error {
		threads, discoverErr := discover(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Retention is bounded by the existing public-event window, independent of matching.
		if err := orm.Where("published_at < ?", query.Since).Delete(&hnPoolModel{}).Error; err != nil {
			return errors.Join(discoverErr, err)
		}
		for _, thread := range threads[:min(len(threads), query.DiscoveryLimit)] {
			var old hnPoolModel
			err := orm.Where("thread_id=?", thread.ID).Take(&old).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.Join(discoverErr, err)
			}
			state, err := old.domain()
			if err != nil {
				return errors.Join(discoverErr, err)
			}
			state, changed := biz.DiscoveredHNState(thread, state, query.Now)
			if !changed {
				continue
			}
			row, err := poolRecord(state)
			if err != nil {
				return err
			}
			if err := orm.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "thread_id"}}, UpdateAll: true}).Create(&row).Error; err != nil {
				return errors.Join(discoverErr, err)
			}
		}
		var rows []hnPoolModel
		// A fixed budget and earliest-due ordering rotate through the whole pool.
		if err := orm.Where("published_at >= ? AND next_refresh_at <= ?", query.Since, query.Now).Order("next_refresh_at,checked_at NULLS FIRST,thread_id").Limit(query.CollectionLimit).Find(&rows).Error; err != nil {
			return errors.Join(discoverErr, err)
		}
		failures := discoverErr
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return errors.Join(failures, err)
			}
			state, err := row.domain()
			if err != nil {
				return errors.Join(failures, err)
			}
			next, sampleErr := sample(ctx, state)
			if err := ctx.Err(); err != nil {
				return errors.Join(failures, err)
			}
			record, err := poolRecord(next)
			if err != nil {
				return errors.Join(failures, err)
			}
			// Independent success persists before any association or summarization.
			writeErr := orm.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "thread_id"}}, UpdateAll: true}).Create(&record).Error
			failures = errors.Join(failures, sampleErr, writeErr)
		}
		return failures
	})
}

func (r *discussionsRepo) RefreshDue(ctx context.Context, query biz.DiscussionQuery, refresh func(context.Context, biz.DiscussionTarget, biz.DiscussionState) (biz.DiscussionState, error)) error {
	query = biz.NormalizeDiscussionQuery(query)
	return r.withLock(ctx, func(orm *gorm.DB) error {
		var rows []hnPoolModel
		if err := orm.Where("published_at>=? AND published_at<=?", query.Since, query.Now).
			Where("confirmed_at > ? AND confirmed_at <= ?", query.PoolConfirmedAfter, query.Now).
			Where("CASE WHEN jsonb_typeof(snapshot->'comments') = 'array' THEN jsonb_array_length(snapshot->'comments') ELSE 0 END >= ?", query.PoolMinComments).
			Order("match_next_at NULLS FIRST,published_at DESC,thread_id DESC").Limit(query.PoolLimit).Find(&rows).Error; err != nil {
			return err
		}
		originalSnapshots := map[int64]string{}
		pool := make([]biz.HNPoolState, 0, len(rows))
		for _, row := range rows {
			originalSnapshots[row.ThreadID] = string(row.Snapshot)
			state, err := row.domain()
			if err != nil {
				return err
			}
			pool = append(pool, state)
		}
		var ids []int64
		if err := discussionEligible(orm, query).Select("s.id").Joins("LEFT JOIN story_discussions d ON d.story_id=s.id").Where("d.next_refresh_at IS NULL OR d.next_refresh_at<=?", query.Now).Order("d.next_refresh_at NULLS FIRST,s.id").Limit(query.Limit).Pluck("s.id", &ids).Error; err != nil {
			return err
		}
		candidates, err := discussionCandidates(orm, query, ids)
		if err != nil {
			return err
		}
		var failures error
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return errors.Join(failures, err)
			}
			target, err := discussionTarget(orm, id, query)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return errors.Join(failures, err)
			}
			target.Candidates = candidates
			target.Pool = pool
			var row storyDiscussionModel
			err = orm.Where("story_id=?", id).Take(&row).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.Join(failures, err)
			}
			state, err := row.domain()
			if err != nil {
				return err
			}
			next, refreshErr := refresh(ctx, target, state)
			if ctx.Err() != nil {
				return errors.Join(failures, ctx.Err())
			}
			next.URLs = slices.Clone(target.URLs)
			writeErr := orm.Transaction(func(tx *gorm.DB) error {
				var story storyModel
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&story).Error; err != nil {
					return err
				}
				current, err := discussionTarget(tx, id, query)
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				if err != nil {
					return err
				}
				currentCandidates, err := discussionCandidates(tx, query, ids)
				if err != nil {
					return err
				}
				if current.Signature != target.Signature || biz.DiscussionMatchSignature(biz.DiscussionMatchInput{Candidates: currentCandidates}) != biz.DiscussionMatchSignature(biz.DiscussionMatchInput{Candidates: candidates}) {
					return nil
				}
				// Recheck pool inputs with one locked query; network/model calls are finished.
				threadIDs := make([]int64, 0, len(pool))
				for _, entry := range pool {
					threadIDs = append(threadIDs, entry.Input.Thread.ID)
				}
				var latest []hnPoolModel
				if len(threadIDs) > 0 {
					if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("thread_id IN ?", threadIDs).Find(&latest).Error; err != nil {
						return err
					}
				}
				if len(latest) != len(pool) {
					return nil
				}
				byID := map[int64]hnPoolModel{}
				for _, row := range latest {
					byID[row.ThreadID] = row
				}
				for _, entry := range pool {
					if byID[entry.Input.Thread.ID].Fingerprint != entry.Fingerprint || string(byID[entry.Input.Thread.ID].Snapshot) != originalSnapshots[entry.Input.Thread.ID] {
						return nil
					}
				}
				for _, entry := range pool {
					old := byID[entry.Input.Thread.ID]
					if old.MatchSignature == entry.MatchSignature && old.MatchError == entry.MatchError && ((old.MatchNextAt == nil && entry.MatchNextAt.IsZero()) || (old.MatchNextAt != nil && old.MatchNextAt.Equal(entry.MatchNextAt))) {
						continue
					}
					record, err := poolRecord(entry)
					if err != nil {
						return err
					}
					if err := tx.Model(&hnPoolModel{}).Where("thread_id=?", record.ThreadID).Updates(map[string]any{"match_signature": record.MatchSignature, "match_snapshot": record.MatchSnapshot, "match_next_at": record.MatchNextAt, "match_error": record.MatchError}).Error; err != nil {
						return err
					}
				}
				record, err := discussionRecord(id, next)
				if err != nil {
					return err
				}
				return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "story_id"}}, DoUpdates: clause.AssignmentColumns([]string{"snapshot", "fingerprint", "updated_at", "confirmed_at", "checked_at", "next_refresh_at", "last_error"})}).Create(&record).Error
			})
			failures = errors.Join(failures, refreshErr, writeErr)
		}
		return failures
	})
}

func discussionEligible(db *gorm.DB, query biz.DiscussionQuery) *gorm.DB {
	return db.Model(&storyModel{}).Table("stories s").Where("EXISTS (SELECT 1 FROM items i WHERE i.story_id=s.id AND i.stage=? AND i.selected AND i.published_at >= ? AND i.published_at <= ?)", biz.StageDone, query.Since, query.Now)
}

func discussionEventItems(db *gorm.DB, id int64) ([]biz.DiscussionEventItem, error) {
	var rows []itemModel
	if err := db.Select("id,url,original_title,summary,published_at,fact,selected,roundup").Where("story_id=? AND stage=?", id, biz.StageDone).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return discussionEventRows(rows)
}

func discussionEventRows(rows []itemModel) ([]biz.DiscussionEventItem, error) {
	items := make([]biz.DiscussionEventItem, 0, len(rows))
	for _, row := range rows {
		item := biz.DiscussionEventItem{DiscussionCandidateItem: biz.DiscussionCandidateItem{ID: row.ID, URL: row.URL, OriginalTitle: row.OriginalTitle, Summary: row.Summary, PublishedAt: row.PublishedAt}, Selected: row.Selected, Roundup: row.Roundup}
		var fact *biz.Fact
		var err error
		if len(row.Fact) > 0 {
			fact, err = decodeFact(row.Fact)
		}
		if err != nil {
			return nil, err
		}
		if fact != nil {
			item.Fact = *fact
		}
		items = append(items, item)
	}
	return items, nil
}

func discussionTarget(db *gorm.DB, id int64, query biz.DiscussionQuery) (biz.DiscussionTarget, error) {
	var row struct{ ID int64 }
	if err := discussionEligible(db, query).Select("s.id").Where("s.id=?", id).Take(&row).Error; err != nil {
		return biz.DiscussionTarget{}, err
	}
	items, err := discussionEventItems(db, id)
	if err != nil {
		return biz.DiscussionTarget{}, err
	}
	target := biz.DiscussionTarget{StoryID: id, Signature: biz.DiscussionTargetSignature(id, items)}
	for _, item := range items {
		target.URLs = append(target.URLs, item.URL)
	}
	return target, nil
}

func discussionCandidates(db *gorm.DB, query biz.DiscussionQuery, ids []int64) ([]biz.DiscussionCandidate, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var eligible []int64
	if err := discussionEligible(db, query).Select("s.id").Where("s.id IN ?", ids).Order("s.id").Pluck("s.id", &eligible).Error; err != nil {
		return nil, err
	}
	result := make([]biz.DiscussionCandidate, 0, len(eligible))
	for _, id := range eligible {
		items, err := discussionEventItems(db, id)
		if err != nil {
			return nil, err
		}
		candidate := biz.BuildDiscussionCandidate(id, items)
		if len(candidate.Items) > 0 {
			result = append(result, candidate)
		}
	}
	return result, nil
}

func releaseDiscussionLock(conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var unlocked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1, 0)", discussionLockNamespace).Scan(&unlocked); err != nil || !unlocked {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
}

// Persist only protocol-free domain input and citations through an explicit data projection.
type discussionSnapshot struct {
	URLs            []string            `json:"urls"`
	TargetSignature string              `json:"target_signature"`
	RuleVersion     string              `json:"rule_version"`
	PoolFingerprint string              `json:"pool_fingerprint"`
	Thread          discussionThread    `json:"thread"`
	Comments        []discussionComment `json:"comments"`
	Points          []discussionPoint   `json:"points"`
}
type discussionThread struct {
	ID          int64     `json:"id"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	PublishedAt time.Time `json:"published_at"`
}
type discussionComment struct {
	ID       int64  `json:"id"`
	ParentID int64  `json:"parent_id"`
	Text     string `json:"text"`
}
type discussionPoint struct {
	Text      string `json:"text"`
	CommentID int64  `json:"comment_id"`
}

func discussionRecord(id int64, state biz.DiscussionState) (storyDiscussionModel, error) {
	thread := state.Input.Thread
	snapshot := discussionSnapshot{URLs: state.URLs, TargetSignature: state.TargetSignature, RuleVersion: state.RuleVersion, PoolFingerprint: state.PoolFingerprint, Thread: discussionThread{ID: thread.ID, URL: thread.URL, Title: thread.Title, PublishedAt: thread.PublishedAt}}
	for _, comment := range state.Input.Comments {
		snapshot.Comments = append(snapshot.Comments, discussionComment{ID: comment.ID, ParentID: comment.ParentID, Text: comment.Text})
	}
	for _, point := range state.Points {
		snapshot.Points = append(snapshot.Points, discussionPoint{Text: point.Text, CommentID: point.CommentID})
	}
	content, err := json.Marshal(snapshot)
	if err != nil {
		return storyDiscussionModel{}, err
	}
	return storyDiscussionModel{StoryID: id, Snapshot: content, Fingerprint: state.Fingerprint, UpdatedAt: nullableDiscussionTime(state.UpdatedAt), ConfirmedAt: nullableDiscussionTime(state.ConfirmedAt), CheckedAt: state.CheckedAt, NextRefreshAt: state.NextRefreshAt, LastError: state.LastError}, nil
}

func (row storyDiscussionModel) domain() (biz.DiscussionState, error) {
	state := biz.DiscussionState{Fingerprint: row.Fingerprint, CheckedAt: row.CheckedAt, NextRefreshAt: row.NextRefreshAt, LastError: row.LastError}
	if row.UpdatedAt != nil {
		state.UpdatedAt = *row.UpdatedAt
	}
	if row.ConfirmedAt != nil {
		state.ConfirmedAt = *row.ConfirmedAt
	}
	if len(row.Snapshot) == 0 {
		return state, nil
	}
	var snapshot discussionSnapshot
	if err := json.Unmarshal(row.Snapshot, &snapshot); err != nil {
		return state, fmt.Errorf("invalid discussion snapshot: %w", err)
	}
	state.URLs = snapshot.URLs
	state.TargetSignature = snapshot.TargetSignature
	state.RuleVersion = snapshot.RuleVersion
	state.PoolFingerprint = snapshot.PoolFingerprint
	thread := snapshot.Thread
	state.Input.Thread = biz.HNThread{ID: thread.ID, URL: thread.URL, Title: thread.Title, PublishedAt: thread.PublishedAt}
	for _, comment := range snapshot.Comments {
		state.Input.Comments = append(state.Input.Comments, biz.HNComment{ID: comment.ID, ParentID: comment.ParentID, Text: comment.Text})
	}
	for _, point := range snapshot.Points {
		state.Points = append(state.Points, biz.DiscussionPoint{Text: point.Text, CommentID: point.CommentID})
	}
	return state, nil
}

func nullableDiscussionTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

// Storage projections keep persistence names out of the domain contract.
type discussionCachedMatchSnapshot struct {
	Signature string                  `json:"signature"`
	Match     discussionMatchSnapshot `json:"match"`
}
type discussionMatchSnapshot struct {
	Decision string                            `json:"decision"`
	StoryID  int64                             `json:"story_id"`
	Evidence []discussionMatchEvidenceSnapshot `json:"evidence"`
	Cache    []discussionCachedMatchSnapshot   `json:"cache"`
}
type discussionMatchEvidenceSnapshot struct {
	Role       string `json:"role"`
	HNField    string `json:"hn_field"`
	HNQuote    string `json:"hn_quote"`
	ItemID     int64  `json:"item_id"`
	EventField string `json:"event_field"`
	EventQuote string `json:"event_quote"`
}

func poolRecord(state biz.HNPoolState) (hnPoolModel, error) {
	record, err := discussionRecord(0, biz.DiscussionState{Input: state.Input})
	if err != nil {
		return hnPoolModel{}, err
	}
	match := discussionMatchSnapshot{Decision: string(state.Match.Decision), StoryID: state.Match.StoryID}
	for _, e := range state.Match.Evidence {
		match.Evidence = append(match.Evidence, discussionMatchEvidenceSnapshot{Role: e.Role, HNField: e.HNField, HNQuote: e.HNQuote, ItemID: e.ItemID, EventField: e.EventField, EventQuote: e.EventQuote})
	}
	for _, cached := range state.MatchCache {
		entry := discussionMatchSnapshot{Decision: string(cached.Match.Decision), StoryID: cached.Match.StoryID}
		for _, e := range cached.Match.Evidence {
			entry.Evidence = append(entry.Evidence, discussionMatchEvidenceSnapshot{Role: e.Role, HNField: e.HNField, HNQuote: e.HNQuote, ItemID: e.ItemID, EventField: e.EventField, EventQuote: e.EventQuote})
		}
		match.Cache = append(match.Cache, discussionCachedMatchSnapshot{Signature: cached.Signature, Match: entry})
	}
	encoded, err := json.Marshal(match)
	if err != nil {
		return hnPoolModel{}, err
	}
	return hnPoolModel{ThreadID: state.Input.Thread.ID, PublishedAt: state.Input.Thread.PublishedAt, Snapshot: record.Snapshot, Fingerprint: state.Fingerprint, ConfirmedAt: nullableDiscussionTime(state.ConfirmedAt), CheckedAt: nullableDiscussionTime(state.CheckedAt), NextRefreshAt: state.NextRefreshAt, LastError: state.LastError, MatchSignature: state.MatchSignature, MatchSnapshot: encoded, MatchNextAt: nullableDiscussionTime(state.MatchNextAt), MatchError: state.MatchError}, nil
}

func (row hnPoolModel) domain() (biz.HNPoolState, error) {
	discussion, err := (storyDiscussionModel{Snapshot: row.Snapshot}).domain()
	if err != nil {
		return biz.HNPoolState{}, err
	}
	state := biz.HNPoolState{Input: discussion.Input, Fingerprint: row.Fingerprint, NextRefreshAt: row.NextRefreshAt, LastError: row.LastError, MatchSignature: row.MatchSignature, MatchError: row.MatchError}
	if row.CheckedAt != nil {
		state.CheckedAt = *row.CheckedAt
	}
	if row.ConfirmedAt != nil {
		state.ConfirmedAt = *row.ConfirmedAt
	}
	if row.MatchNextAt != nil {
		state.MatchNextAt = *row.MatchNextAt
	}
	if len(row.MatchSnapshot) > 0 {
		var match discussionMatchSnapshot
		if err := json.Unmarshal(row.MatchSnapshot, &match); err != nil {
			return state, err
		}
		for _, cached := range match.Cache {
			entry := biz.DiscussionMatch{Decision: biz.DiscussionMatchDecision(cached.Match.Decision), StoryID: cached.Match.StoryID}
			for _, e := range cached.Match.Evidence {
				entry.Evidence = append(entry.Evidence, biz.DiscussionMatchEvidence{Role: e.Role, HNField: e.HNField, HNQuote: e.HNQuote, ItemID: e.ItemID, EventField: e.EventField, EventQuote: e.EventQuote})
			}
			state.MatchCache = append(state.MatchCache, biz.DiscussionCachedMatch{Signature: cached.Signature, Match: entry})
		}
		state.Match = biz.DiscussionMatch{Decision: biz.DiscussionMatchDecision(match.Decision), StoryID: match.StoryID}
		for _, e := range match.Evidence {
			state.Match.Evidence = append(state.Match.Evidence, biz.DiscussionMatchEvidence{Role: e.Role, HNField: e.HNField, HNQuote: e.HNQuote, ItemID: e.ItemID, EventField: e.EventField, EventQuote: e.EventQuote})
		}
	}
	return state, nil
}
