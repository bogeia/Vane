package biz

import (
	"context"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

type TopicItem struct {
	Title, OriginalTitle, Category string
	Subjects, Tags                 []string
}

type TopicsRebuildResult struct{ Checked, Updated int64 }

type TopicsRepository interface {
	Rebuild(context.Context, func(TopicItem) []string) (TopicsRebuildResult, error)
}

type TopicsUseCase struct{ repo TopicsRepository }

func NewTopicsUseCase(repo TopicsRepository) *TopicsUseCase { return &TopicsUseCase{repo: repo} }

func (u *TopicsUseCase) Rebuild(ctx context.Context) (TopicsRebuildResult, error) {
	return u.repo.Rebuild(ctx, func(item TopicItem) []string {
		return taxonomy.AssignTopics(item.Title, item.OriginalTitle, item.Subjects, item.Tags, item.Category)
	})
}
