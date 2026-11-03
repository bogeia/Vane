package integration

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/data"
)

type readingQueryCounter struct {
	logger.Interface
	count int
}

func (c *readingQueryCounter) Trace(context.Context, time.Time, func() (string, int64), error) {
	c.count++
}

func TestItemReadingFlagsShareDetailPolicyAndBatchQueries(t *testing.T) {
	store, query, _ := discussionStore(t)
	db := testDB(t, store)
	for index := range 7 {
		var id int64
		if err := db.QueryRowContext(t.Context(), "INSERT INTO stories(title) VALUES('Reading availability') RETURNING id").Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,title,published_at,stage,selected,story_id,body) VALUES('group',$1,'Reading availability','Reading availability',$2,'done',true,$3,$4)`, fmt.Sprintf("https://example.test/reading-%d", index), query.Now, id, "Saved body"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE items SET body=E' \n\t ' WHERE url='https://example.test/1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,published_at,stage,body) VALUES('group','https://example.test/hidden','Hidden',$1,'analyzed','Hidden body')`, query.Now); err != nil {
		t.Fatal(err)
	}
	if err := data.NewDiscussionsRepo(store).RefreshDue(t.Context(), query, func(_ context.Context, target biz.DiscussionTarget, _ biz.DiscussionState) (biz.DiscussionState, error) {
		return discussionState(target, query.Now), nil
	}); err != nil {
		t.Fatal(err)
	}
	counter := &readingQueryCounter{Interface: logger.Discard}
	store.ORM = store.ORM.Session(&gorm.Session{Logger: counter})
	items := biz.NewItemsUseCase(data.NewItemsRepo(store), func() time.Time { return query.Now })
	page, err := items.List(t.Context(), nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 8 || counter.count != 4 {
		t.Fatalf("public list should use four queries for eight stories: items=%d queries=%d", len(page.Items), counter.count)
	}
	for _, item := range page.Items {
		wantBody := item.URL != "https://example.test/1"
		if item.HasBody != wantBody || !item.HasDiscussion {
			t.Fatalf("incorrect reading flags: %+v", item)
		}
		id, err := strconv.ParseInt(item.ID, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		detail, err := items.Detail(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		storyID, err := strconv.ParseInt(item.StoryID, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		story, err := biz.NewStoriesUseCase(data.NewStoriesRepo(store)).Get(t.Context(), storyID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.HasBody != item.HasBody || detail.HasDiscussion != item.HasDiscussion || (story.Discussion != nil) != item.HasDiscussion {
			t.Fatalf("list/detail/story disagree for item %s", item.ID)
		}
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE story_discussions SET confirmed_at=$1", query.Now.Add(-biz.DiscussionFreshness)); err != nil {
		t.Fatal(err)
	}
	page, err = items.List(t.Context(), nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.HasDiscussion {
			t.Fatalf("expired discussion marked readable: %s", item.ID)
		}
	}
}
