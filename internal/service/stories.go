package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/bogeia/vane/internal/biz"
)

type Story struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Items []Item `json:"items"`
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
	return &StoryOutput{Body: Story{ID: story.ID, Title: story.Title, Items: itemsDTO(story.Items)}}, nil
}
