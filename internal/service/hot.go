package service

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"

	"github.com/bogeia/vane/internal/biz"
)

type HotStory struct {
	StoryID string       `json:"story_id"`
	Title   string       `json:"title"`
	Heat    int          `json:"heat"`
	Trend   string       `json:"trend"`
	Sources []ItemSource `json:"sources"`
}

type HotService struct {
	hot *biz.HotUseCase
	log *slog.Logger
}

func NewHotService(hot *biz.HotUseCase, log *slog.Logger) *HotService { return &HotService{hot, log} }

type HotOutput struct {
	Body struct {
		Items []HotStory `json:"items"`
	}
}

func (h *HotService) List(ctx context.Context, _ *struct{}) (*HotOutput, error) {
	items, err := h.hot.List(ctx)
	if err != nil {
		h.log.ErrorContext(ctx, "get hot ranking", "error", err)
		return nil, huma.Error500InternalServerError("internal server error")
	}
	output := &HotOutput{}
	if items != nil {
		output.Body.Items = make([]HotStory, len(items))
		for index, item := range items {
			story := HotStory{StoryID: item.StoryID, Title: item.Title, Heat: item.Heat, Trend: item.Trend}
			if item.Sources != nil {
				story.Sources = make([]ItemSource, len(item.Sources))
				for sourceIndex, source := range item.Sources {
					story.Sources[sourceIndex] = itemSourceDTO(source)
				}
			}
			output.Body.Items[index] = story
		}
	}
	return output, nil
}
