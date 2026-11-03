package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/bogeia/vane/internal/biz"
)

type storiesRepo struct {
	story biz.Story
	err   error
	calls int
}

func (r *storiesRepo) Story(context.Context, int64) (biz.Story, error) {
	r.calls++
	return r.story, r.err
}

func TestStoryDiscussionDTO(t *testing.T) {
	now := time.Now().UTC()
	summary := &biz.DiscussionSummary{Thread: biz.HNThread{ID: 123, URL: "https://evil.example/not-a-discussion"}, SampleSize: 2, UpdatedAt: now, Points: []biz.DiscussionPoint{{Text: "有评论认为，仍需检验。", CommentID: 456}}}
	dto := storyDiscussionDTO(summary)
	if dto == nil || dto.ThreadURL != "https://news.ycombinator.com/item?id=123" || dto.Points[0].CommentURL != "https://news.ycombinator.com/item?id=456" || dto.SampleSize != 2 || !dto.UpdatedAt.Equal(now) {
		t.Fatalf("DTO=%+v", dto)
	}
	body, err := json.Marshal(dto)
	if err != nil || strings.Contains(string(body), "evil.example") || strings.Contains(string(body), "comment_id") {
		t.Fatalf("body=%s err=%v", body, err)
	}
	if storyDiscussionDTO(nil) != nil || storyDiscussionDTO(&biz.DiscussionSummary{}) != nil {
		t.Fatal("empty summary visible")
	}
	summary.Points[0].CommentID = -1
	if storyDiscussionDTO(summary) != nil {
		t.Fatal("invalid reference visible")
	}
}

func TestStoryServiceErrorBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, id  string
		err       error
		want      int
		wantCalls int
	}{
		{"invalid", "oops", nil, 400, 0},
		{"not found", "1", biz.ErrStoryNotFound, 404, 1},
		{"internal", "1", errors.New("database credential secret"), 500, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &storiesRepo{err: tc.err}
			service := NewStoriesService(biz.NewStoriesUseCase(repo), slog.New(slog.NewTextHandler(io.Discard, nil)))
			_, err := service.Get(t.Context(), &StoryInput{ID: tc.id})
			var status huma.StatusError
			if !errors.As(err, &status) || status.GetStatus() != tc.want || repo.calls != tc.wantCalls {
				t.Fatalf("err=%v calls=%d", err, repo.calls)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("internal error exposed")
			}
		})
	}
}
