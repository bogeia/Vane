package data

import (
	"reflect"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

func TestDiscussionSnapshotRoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)
	state := biz.DiscussionState{URLs: []string{"https://example.com/"}, Input: biz.DiscussionInput{Thread: biz.HNThread{ID: 10, URL: "https://example.com/", Title: "Thread", PublishedAt: now}, Comments: []biz.HNComment{{ID: 11, ParentID: 10, Text: "public comment"}, {ID: 12, ParentID: 10, Text: "second comment"}}}, Fingerprint: "input-v1", Points: []biz.DiscussionPoint{{Text: "用户评论", CommentID: 11}}, UpdatedAt: now, ConfirmedAt: now, CheckedAt: now, NextRefreshAt: now.Add(time.Hour), LastError: "safe failure"}
	row, err := discussionRecord(20, state)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := row.domain()
	if err != nil || !reflect.DeepEqual(state, restored) {
		t.Fatalf("round trip: %+v %v", restored, err)
	}
	if row.StoryID != 20 {
		t.Fatal("story association lost")
	}
	row.Snapshot = []byte("invalid json")
	if _, err := row.domain(); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
}

func TestDiscussionEmptySnapshotTimes(t *testing.T) {
	row, err := discussionRecord(1, biz.DiscussionState{})
	if err != nil {
		t.Fatal(err)
	}
	if row.ConfirmedAt != nil || row.UpdatedAt != nil {
		t.Fatal("empty snapshot has successful timestamps")
	}
	restored, err := row.domain()
	if err != nil || !restored.ConfirmedAt.IsZero() || len(restored.Points) != 0 {
		t.Fatalf("empty snapshot: %+v %v", restored, err)
	}
}
