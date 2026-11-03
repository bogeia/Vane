package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/bogeia/vane/internal/biz"
)

type Story struct {
	ID         string           `json:"id"`
	Title      string           `json:"title"`
	Items      []Item           `json:"items"`
	Discussion *StoryDiscussion `json:"discussion"`
}

type StoriesService struct {
	stories *biz.StoriesUseCase
	log     *slog.Logger
}

func NewStoriesService(stories *biz.StoriesUseCase, log *slog.Logger) *StoriesService {
	return &StoriesService{stories, log}
}

type StoryInput struct {
	ID string `path:"id"`
}
type StoryOutput struct{ Body Story }

func (s *StoriesService) Get(ctx context.Context, input *StoryInput) (*StoryOutput, error) {
	id, err := strconv.ParseInt(input.ID, 10, 64)
	if err != nil || id <= 0 {
		return nil, huma.Error400BadRequest("invalid story id")
	}
	story, err := s.stories.Get(ctx, id)
	if errors.Is(err, biz.ErrStoryNotFound) {
		return nil, huma.Error404NotFound("story not found")
	}
	if err != nil {
		s.log.ErrorContext(ctx, "get story", "error", err)
		return nil, huma.Error500InternalServerError("internal server error")
	}
	return &StoryOutput{Body: Story{ID: story.ID, Title: story.Title, Items: itemsDTO(story.Items), Discussion: storyDiscussionDTO(story.Discussion)}}, nil
}

// StoryDiscussion is a cached sample of community opinions, separate from event facts.
type StoryDiscussion struct {
	ThreadURL  string                 `json:"thread_url"`
	SampleSize int                    `json:"sample_size"`
	UpdatedAt  time.Time              `json:"updated_at"`
	Points     []StoryDiscussionPoint `json:"points"`
}

type StoryDiscussionPoint struct {
	Text       string `json:"text"`
	CommentURL string `json:"comment_url"`
}

func storyDiscussionDTO(summary *biz.DiscussionSummary) *StoryDiscussion {
	if summary == nil || len(summary.Points) == 0 || summary.Thread.ID <= 0 {
		return nil
	}
	points := make([]StoryDiscussionPoint, 0, len(summary.Points))
	for _, point := range summary.Points {
		if point.CommentID <= 0 {
			return nil
		}
		points = append(points, StoryDiscussionPoint{Text: point.Text, CommentURL: hnItemURL(point.CommentID)})
	}
	return &StoryDiscussion{ThreadURL: hnItemURL(summary.Thread.ID), SampleSize: summary.SampleSize, UpdatedAt: summary.UpdatedAt, Points: points}
}

func hnItemURL(id int64) string {
	return "https://news.ycombinator.com/item?id=" + strconv.FormatInt(id, 10)
}
