package biz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

type ItemSource struct {
	IconVersion string
	ID          string
	Name        string
	Tier        string
}
type Item struct {
	Score1, Score2 *int
	Roundup        bool
	ID             string
	Title          string
	OriginalTitle  string
	Summary        string
	Reason         string
	URL            string
	ImageURL       string
	Source         ItemSource
	PublishedAt    time.Time
	Score          int
	Selected       bool
	Category       string
	Topics         []string
	Tags           []string
	StoryID        string
	StoryOthers    int
	StoryItemIDs   []string
}
type Cursor struct {
	PublishedAt time.Time
	ID          int64
}
type ItemPage struct {
	Items      []Item
	NextCursor *Cursor
}
type ItemFilter struct {
	View, Category, Q, Topic string
	Since                    time.Time
}

var ErrItemNotFound = errors.New("item not found")

type ItemDetail struct {
	Item
	RelatedItems []Item
}

type ItemsRepository interface {
	SitemapReports(context.Context, int) ([]SitemapResource, error)
	SitemapItems(context.Context, SitemapQuery) ([]SitemapResource, error)
	Topics(context.Context) ([]taxonomy.Topic, error)
	Detail(context.Context, int64) (ItemDetail, error)
	ListItems(context.Context, *Cursor, int, ItemFilter) ([]Item, error)
}
type ItemsUseCase struct {
	repo ItemsRepository
	now  func() time.Time
}

func NewItemsUseCase(repo ItemsRepository, now func() time.Time) *ItemsUseCase {
	return &ItemsUseCase{repo: repo, now: now}
}

func (i *ItemsUseCase) Detail(ctx context.Context, id int64) (ItemDetail, error) {
	detail, err := i.repo.Detail(ctx, id)
	detail.Item = CompleteItem(detail.Item)
	for index := range detail.RelatedItems {
		detail.RelatedItems[index] = CompleteItem(detail.RelatedItems[index])
	}
	return detail, err
}

func (i *ItemsUseCase) List(ctx context.Context, cursor *Cursor, limit int, filters ...ItemFilter) (ItemPage, error) {
	filter := ItemFilter{}
	if len(filters) > 0 {
		filter = filters[0]
	}
	items, err := i.repo.ListItems(ctx, cursor, limit+1, filter)
	if err != nil {
		return ItemPage{}, err
	}
	for index := range items {
		items[index] = CompleteItem(items[index])
	}
	page := ItemPage{Items: items}
	if page.Items == nil {
		page.Items = []Item{}
	}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		id, err := strconv.ParseInt(last.ID, 10, 64)
		if err != nil {
			return ItemPage{}, fmt.Errorf("invalid item cursor ID: %w", err)
		}
		page.NextCursor = &Cursor{PublishedAt: last.PublishedAt, ID: id}
	}
	return page, nil
}

func (i *ItemsUseCase) Topics(ctx context.Context) ([]taxonomy.Topic, error) {
	counts, err := i.repo.Topics(ctx)
	if err != nil {
		return nil, err
	}
	bySlug := make(map[string]taxonomy.Topic, len(counts))
	for _, row := range counts {
		bySlug[row.Slug] = row
	}
	topics := slices.Clone(taxonomy.Topics)
	for index := range topics {
		count := bySlug[topics[index].Slug]
		topics[index].SelectedCount = count.SelectedCount
		topics[index].AllCount = count.AllCount
	}
	return topics, nil
}

// CompleteItem applies the shared business score and title fallback.
func CompleteItem(item Item) Item {
	if item.Title == "" {
		item.Title = item.OriginalTitle
	}
	if item.Score1 != nil || item.Score2 != nil {
		first, second := 0, 0
		if item.Score1 != nil {
			first = *item.Score1
		}
		if item.Score2 != nil {
			second = *item.Score2
		}
		item.Score = AverageScore(first, second)
	}
	return item
}
