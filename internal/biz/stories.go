package biz

import (
	"context"
	"errors"
)

var ErrStoryNotFound = errors.New("story not found")

type Story struct {
	ID    string
	Title string
	Items []Item
}
type StoriesRepository interface {
	Story(context.Context, int64) (Story, error)
}

type StoriesUseCase struct{ repo StoriesRepository }

func NewStoriesUseCase(repo StoriesRepository) *StoriesUseCase { return &StoriesUseCase{repo: repo} }
func (u *StoriesUseCase) Get(ctx context.Context, id int64) (Story, error) {
	story, err := u.repo.Story(ctx, id)
	for index := range story.Items {
		story.Items[index] = CompleteItem(story.Items[index])
	}
	return story, err
}
