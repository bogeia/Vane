package data

import (
	"context"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/bogeia/vane/internal/biz"
)

type publicItemRow struct {
	Item              itemModel `gorm:"embedded"`
	SourceName        string
	SourceTier        string
	SourceIconVersion string
	StoryOthers       int64
	StoryItemIDs      textArray `gorm:"type:text[]"`
}

type itemsRepo struct{ data *Data }

func NewItemsRepo(data *Data) biz.ItemsRepository {
	return &itemsRepo{data: data}
}

func (row publicItemRow) item() biz.Item {
	stored := row.Item
	storyID := ""
	if stored.StoryID != nil {
		storyID = strconv.FormatInt(*stored.StoryID, 10)
	}
	return biz.Item{Roundup: stored.Roundup, ID: strconv.FormatInt(stored.ID, 10), Title: stored.Title, OriginalTitle: stored.OriginalTitle, Summary: stored.Summary, Reason: stored.Reason, URL: stored.URL, ImageURL: stored.ImageURL, Source: biz.ItemSource{ID: stored.SourceID, Name: row.SourceName, Tier: row.SourceTier, IconVersion: row.SourceIconVersion}, PublishedAt: stored.PublishedAt, Score1: stored.Score1, Score2: stored.Score2, Selected: stored.Selected, Category: stored.Category, Topics: append([]string{}, stored.Topics...), Tags: append([]string{}, stored.Tags...), StoryID: storyID, StoryOthers: int(row.StoryOthers), StoryItemIDs: append([]string{}, row.StoryItemIDs...)}
}

func publicItems(db *gorm.DB) *gorm.DB {
	siblings := db.Session(&gorm.Session{NewDB: true}).Model(&itemModel{}).Select("count(*)").Where("story_id = i.story_id AND stage = ? AND id <> i.id", biz.StageDone)
	members := db.Session(&gorm.Session{NewDB: true}).Model(&itemModel{}).Select("array_agg(id::text ORDER BY id)").Where("(story_id = i.story_id OR id = i.id) AND stage = ?", biz.StageDone)
	return db.Model(&itemModel{}).Table("items i").Select("i.id,i.source_id,i.url,i.original_title,i.image_url,i.published_at,i.discovered_at,i.processed_at,i.score1,i.score2,i.title,i.summary,i.reason,i.category,i.tags,i.topics,i.roundup,i.selected,i.story_id,s.name AS source_name,s.tier AS source_tier,s.icon_version AS source_icon_version,(?) AS story_others,(?) AS story_item_ids", siblings, members).Joins("JOIN sources s ON s.id=i.source_id").Where("i.stage = ?", biz.StageDone)
}

func scanPublicItems(query *gorm.DB) ([]biz.Item, error) {
	var records []publicItemRow
	if err := query.Scan(&records).Error; err != nil {
		return nil, err
	}
	items := make([]biz.Item, 0, len(records))
	for _, record := range records {
		items = append(items, record.item())
	}
	return items, nil
}

func (r *itemsRepo) ListItems(ctx context.Context, cursor *biz.Cursor, limit int, filter biz.ItemFilter) ([]biz.Item, error) {
	db := r.data.ORM.WithContext(ctx)
	query := publicItems(db)
	if !filter.Since.IsZero() {
		query = query.Where("i.published_at >= ?", filter.Since)
	}
	if cursor != nil {
		query = query.Where("(i.published_at,i.id) < (?,?)", cursor.PublishedAt, cursor.ID)
	}
	if filter.View == biz.ViewSelected {
		members := db.Model(&itemModel{}).Select("1").Where("story_id = st.id AND stage = ? AND selected AND NOT roundup", biz.StageDone)
		stories := db.Model(&storyModel{}).Table("stories st").Select("1").Where("st.id = i.story_id AND st.representative_item_id = i.id").Where("EXISTS (?)", members)
		query = query.Where("NOT i.roundup").Where("EXISTS (?)", stories)
	}
	if filter.Category != "" {
		query = query.Where("i.category = ?", filter.Category)
	}
	if filter.Topic != "" {
		query = query.Where("i.topics @> ?::text[]", textArray{filter.Topic})
	}
	if filter.Q != "" {
		escaped := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(filter.Q)
		pattern := "%" + escaped + "%"
		query = query.Where("(i.title ILIKE ? OR i.original_title ILIKE ? OR i.summary ILIKE ?)", pattern, pattern, pattern)
	}
	return scanPublicItems(query.Order("i.published_at DESC,i.id DESC").Limit(limit))
}

func (r *itemsRepo) Detail(ctx context.Context, id int64) (biz.ItemDetail, error) {
	items, err := scanPublicItems(publicItems(r.data.ORM.WithContext(ctx)).Where("i.id = ?", id))
	if err != nil {
		return biz.ItemDetail{}, err
	}
	if len(items) == 0 {
		return biz.ItemDetail{}, biz.ErrItemNotFound
	}
	detail := biz.ItemDetail{Item: items[0], RelatedItems: []biz.Item{}}
	if detail.StoryID == "" {
		return detail, nil
	}
	detail.RelatedItems, err = scanPublicItems(publicItems(r.data.ORM.WithContext(ctx)).Where("i.story_id = ? AND i.id <> ?", detail.StoryID, id).Order("i.published_at,i.id"))
	return detail, err
}
