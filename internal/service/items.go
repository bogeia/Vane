package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/biz/taxonomy"
)

type ItemSource struct {
	IconURL string `json:"icon_url"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Tier    string `json:"tier"`
}

type Item struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	OriginalTitle string     `json:"original_title"`
	Summary       string     `json:"summary"`
	Reason        string     `json:"reason"`
	URL           string     `json:"url"`
	ImageURL      string     `json:"image_url"`
	Source        ItemSource `json:"source"`
	PublishedAt   time.Time  `json:"published_at"`
	Score         int        `json:"score"`
	Selected      bool       `json:"selected"`
	Category      string     `json:"category"`
	Tags          []string   `json:"tags"`
	StoryID       string     `json:"story_id"`
	StoryOthers   int        `json:"story_others"`
	StoryItemIDs  []string   `json:"story_item_ids"`
}

type ItemPage struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type ItemDetail struct {
	Item
	RelatedItems []Item `json:"related_items"`
}

type Topic struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Group         string `json:"group"`
	Description   string `json:"description"`
	SelectedCount int64  `json:"selected_count"`
	AllCount      int64  `json:"all_count"`
}

type ItemsService struct {
	items *biz.ItemsUseCase
	log   *slog.Logger
}

func NewItemsService(items *biz.ItemsUseCase, log *slog.Logger) *ItemsService {
	return &ItemsService{items, log}
}

type ListItemsInput struct {
	Topic    string `query:"topic"`
	View     string `query:"view" default:"all"`
	Limit    int    `query:"limit" default:"20"`
	Cursor   string `query:"cursor"`
	Category string `query:"category"`
	Q        string `query:"q"`
}
type ListItemsOutput struct{ Body ItemPage }

func (i *ItemsService) List(ctx context.Context, input *ListItemsInput) (*ListItemsOutput, error) {
	if input.View != biz.ViewAll && input.View != biz.ViewSelected {
		return nil, huma.Error400BadRequest("view must be all or selected")
	}
	if input.Limit < 1 || input.Limit > 100 {
		return nil, huma.Error400BadRequest("limit must be between 1 and 100")
	}
	if input.Category != "" && !slices.Contains(biz.Categories, input.Category) {
		return nil, huma.Error400BadRequest("invalid category")
	}
	if len([]rune(input.Q)) > 200 || strings.ContainsRune(input.Q, '\x00') {
		return nil, huma.Error400BadRequest("invalid search query")
	}
	if input.Topic != "" && !taxonomy.ValidTopic(input.Topic) {
		return nil, huma.Error400BadRequest("invalid topic")
	}
	cursor, err := parseCursor(input.Cursor)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid cursor")
	}
	page, err := i.items.List(ctx, cursor, input.Limit, biz.ItemFilter{View: input.View, Category: input.Category, Q: input.Q, Topic: input.Topic})
	if err != nil {
		i.log.ErrorContext(ctx, "list items", "error", err)
		return nil, huma.Error500InternalServerError("internal server error")
	}
	return &ListItemsOutput{Body: ItemPage{Items: itemsDTO(page.Items), NextCursor: encodeCursor(page.NextCursor)}}, nil
}

type ItemInput struct {
	ID string `path:"id"`
}
type ItemOutput struct{ Body ItemDetail }

func (i *ItemsService) Detail(ctx context.Context, input *ItemInput) (*ItemOutput, error) {
	id, err := strconv.ParseInt(input.ID, 10, 64)
	if err != nil || id <= 0 {
		return nil, huma.Error400BadRequest("invalid item id")
	}
	detail, err := i.items.Detail(ctx, id)
	if errors.Is(err, biz.ErrItemNotFound) {
		return nil, huma.Error404NotFound("item not found")
	}
	if err != nil {
		i.log.ErrorContext(ctx, "get item", "error", err)
		return nil, huma.Error500InternalServerError("internal server error")
	}
	return &ItemOutput{Body: ItemDetail{Item: itemDTO(detail.Item), RelatedItems: itemsDTO(detail.RelatedItems)}}, nil
}

type TopicsOutput struct {
	Body struct {
		Topics []Topic `json:"topics"`
	}
}

func (i *ItemsService) Topics(ctx context.Context, _ *struct{}) (*TopicsOutput, error) {
	topics, err := i.items.Topics(ctx)
	if err != nil {
		i.log.ErrorContext(ctx, "list topics", "error", err)
		return nil, huma.Error500InternalServerError("internal server error")
	}
	result := &TopicsOutput{}
	if topics != nil {
		result.Body.Topics = make([]Topic, len(topics))
		for index, topic := range topics {
			result.Body.Topics[index] = Topic{Slug: topic.Slug, Name: topic.Name, Group: topic.Group, Description: topic.Description, SelectedCount: topic.SelectedCount, AllCount: topic.AllCount}
		}
	}
	return result, nil
}

func itemSourceDTO(in biz.ItemSource) ItemSource {
	return ItemSource{ID: in.ID, Name: in.Name, Tier: in.Tier, IconURL: sourceIconURL(in.ID, in.IconVersion)}
}

func itemDTO(in biz.Item) Item {
	return Item{
		ID: in.ID, Title: in.Title, OriginalTitle: in.OriginalTitle,
		Summary: in.Summary, Reason: in.Reason, URL: in.URL, ImageURL: in.ImageURL,
		Source: itemSourceDTO(in.Source), PublishedAt: in.PublishedAt, Score: in.Score,
		Selected: in.Selected, Category: in.Category, Tags: in.Tags, StoryID: in.StoryID,
		StoryOthers: in.StoryOthers, StoryItemIDs: in.StoryItemIDs,
	}
}

func itemsDTO(in []biz.Item) []Item {
	if in == nil {
		return nil
	}
	out := make([]Item, len(in))
	for i, item := range in {
		out[i] = itemDTO(item)
	}
	return out
}
