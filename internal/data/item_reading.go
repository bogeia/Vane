package data

import (
	"log/slog"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/bogeia/vane/internal/biz"
)

// Read each story's bounded cached evidence once, shared by all page members.
func loadItemDiscussions(db *gorm.DB, items []biz.Item) (err error) {
	defer func() {
		if err == nil {
			return
		}
		if cause := db.Statement.Context.Err(); cause != nil {
			err = cause
			return
		}
		slog.ErrorContext(db.Statement.Context, "read story discussion cache", "error", err)
		err = nil
	}()

	ids := map[string]bool{}
	for _, item := range items {
		if item.StoryID != "" {
			ids[item.StoryID] = true
		}
	}
	if len(ids) == 0 {
		return nil
	}
	storyIDs := make([]string, 0, len(ids))
	for id := range ids {
		storyIDs = append(storyIDs, id)
	}
	now := time.Now()
	eligible := discussionEligible(db.Session(&gorm.Session{NewDB: true}), biz.DiscussionQueryAt(now)).Select("s.id").Where("s.id IN ?", storyIDs)
	var caches []storyDiscussionModel
	if err := db.Where("story_id IN (?)", eligible).Find(&caches).Error; err != nil {
		return err
	}
	if len(caches) == 0 {
		return nil
	}
	reads := map[int64]*biz.DiscussionRead{}
	threadIDs := []int64{}
	cachedStoryIDs := []int64{}
	for _, cache := range caches {
		state, err := cache.domain()
		if err != nil {
			slog.ErrorContext(db.Statement.Context, "decode story discussion cache", "story_id", cache.StoryID, "error", err)
			continue
		}
		reads[cache.StoryID] = &biz.DiscussionRead{State: state, ReadAt: now}
		cachedStoryIDs = append(cachedStoryIDs, cache.StoryID)
		threadIDs = append(threadIDs, state.Input.Thread.ID)
	}
	if len(reads) == 0 {
		return nil
	}
	var rows []itemModel
	if err := db.Select("id,story_id,url,original_title,summary,published_at,fact,selected,roundup").Where("story_id IN ? AND stage=?", cachedStoryIDs, biz.StageDone).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	members := map[int64][]itemModel{}
	for _, row := range rows {
		members[*row.StoryID] = append(members[*row.StoryID], row)
	}
	for id, read := range reads {
		events, err := discussionEventRows(members[id])
		if err != nil {
			delete(reads, id)
			continue
		}
		read.Target = biz.DiscussionTarget{StoryID: id, Signature: biz.DiscussionTargetSignature(id, events)}
		for _, event := range events {
			read.Target.URLs = append(read.Target.URLs, event.URL)
		}
	}
	var pools []hnPoolModel
	if err := db.Where("thread_id IN ?", threadIDs).Find(&pools).Error; err != nil {
		return err
	}
	byThread := map[int64]biz.HNPoolState{}
	for _, row := range pools {
		pool, err := row.domain()
		if err == nil {
			byThread[row.ThreadID] = pool
		}
	}
	for index := range items {
		id, err := strconv.ParseInt(items[index].StoryID, 10, 64)
		if err != nil {
			continue
		}
		if read := reads[id]; read != nil {
			read.Pool = byThread[read.State.Input.Thread.ID]
			items[index].DiscussionRead = read
		}
	}
	return nil
}
