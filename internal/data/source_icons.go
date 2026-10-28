package data

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/bogeia/vane/internal/biz"
)

type sourceIconsRepo struct{ data *Data }

func NewSourceIconsRepo(data *Data) biz.SourceIconsRepository { return &sourceIconsRepo{data: data} }
func (r *sourceIconsRepo) IconState(ctx context.Context, id string) (biz.SourceIconState, error) {
	var row sourceModel
	err := r.data.ORM.WithContext(ctx).Select("icon_input,icon_version").Where("id = ?", id).Take(&row).Error
	return biz.SourceIconState{Input: row.IconInput, Version: row.IconVersion}, err
}

func (r *sourceIconsRepo) SaveIcon(ctx context.Context, source biz.Source, input string, icon biz.SourceIcon) error {
	return r.data.ORM.WithContext(ctx).Model(&sourceModel{}).Where("id = ? AND feed_url = ?", source.ID, source.FeedURL).Updates(map[string]any{"icon_version": icon.Version(), "icon_input": input, "icon_content": icon.Content, "icon_content_type": icon.ContentType}).Error
}

func (r *sourceIconsRepo) Icon(ctx context.Context, id string) (biz.SourceIcon, error) {
	var row sourceModel
	err := r.data.ORM.WithContext(ctx).Select("icon_content,icon_content_type").Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && len(row.IconContent) == 0) {
		return biz.SourceIcon{}, biz.ErrSourceIconNotFound
	}
	return biz.SourceIcon{Content: row.IconContent, ContentType: row.IconContentType}, err
}
