package data

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/bogeia/vane/internal/biz"
)

type storiesRepo struct{ data *Data }

func NewStoriesRepo(data *Data) biz.StoriesRepository {
	return &storiesRepo{data: data}
}

func (r *storiesRepo) Story(ctx context.Context, id int64) (biz.Story, error) {
	var record struct {
		ID    string
		Title string
	}
	var story biz.Story
	err := r.data.ORM.WithContext(ctx).Model(&storyModel{}).Select("id::text,title").Where("id = ? AND EXISTS(SELECT 1 FROM items WHERE story_id=? AND stage=?)", id, id, biz.StageDone).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return biz.Story{}, biz.ErrStoryNotFound
	}
	if err != nil {
		return biz.Story{}, err
	}
	story.ID, story.Title = record.ID, record.Title
	story.Items, err = scanPublicItems(publicItems(r.data.ORM.WithContext(ctx)).Where("i.story_id = ?", id).Order("i.published_at,i.id"))
	return story, err
}
