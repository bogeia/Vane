package data

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/bogeia/vane/internal/biz"
)

type collectionRepo struct{ data *Data }

type collectSourceRecord struct {
	ID, FeedURL, ETag, LastModified string
	IntervalMinutes                 int
	FirstCollected                  bool
	RetryNotBefore                  time.Time
}

func NewCollectionRepo(data *Data) biz.CollectionRepository {
	return &collectionRepo{data: data}
}

func (r *collectionRepo) DueSources(ctx context.Context, now time.Time) ([]biz.CollectSource, error) {
	var rows []collectSourceRecord
	var sources []biz.CollectSource
	err := r.data.ORM.WithContext(ctx).Model(&sourceModel{}).Select("id,feed_url,etag AS e_tag,last_modified,interval_minutes,first_collected,retry_not_before").Where("enabled AND next_collect_at <= ?", now).Order("id").Scan(&rows).Error
	for _, row := range rows {
		sources = append(sources, row.domain())
	}
	return sources, err
}

func (r *collectionRepo) SaveCollection(ctx context.Context, result biz.CollectionResult, items []biz.CollectedItem) error {
	return r.data.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var added int64
		for _, item := range items {
			values := map[string]any{"source_id": result.SourceID, "url": item.URL, "original_title": item.OriginalTitle, "body": item.Body, "image_url": item.ImageURL, "published_at": item.PublishedAt, "discovered_at": item.DiscoveredAt, "next_attempt_at": item.DiscoveredAt}
			result := tx.Model(&itemModel{}).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "url_hash"}}, DoNothing: true}).Create(values)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				if err := verifyURLHash(tx, &itemModel{}, item.URL); err != nil {
					return err
				}
			}
			added += result.RowsAffected
		}
		for _, blocked := range result.Blocked {
			insert := tx.Exec(`INSERT INTO blocked_contents (url, source_id, original_title, first_blocked_at)
 SELECT ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM items WHERE url_hash = ? AND url = ?)
 ON CONFLICT (url_hash) DO NOTHING`, blocked.URL, result.SourceID, blocked.OriginalTitle, blocked.FirstBlockedAt, urlDigest(blocked.URL), blocked.URL)
			if insert.Error != nil {
				return insert.Error
			}
			if insert.RowsAffected == 0 {
				if err := verifyURLHash(tx, &blockedContentModel{}, blocked.URL); err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&sourceModel{}).Where("id = ?", result.SourceID).Updates(map[string]any{"last_collected_at": result.FetchedAt, "next_collect_at": result.NextCollectAt, "last_error": result.Error, "etag": result.ETag, "last_modified": result.LastModified, "first_collected": result.FirstCollected, "retry_not_before": result.RetryNotBefore}).Error; err != nil {
			return err
		}
		return tx.Create(&fetchRecordModel{SourceID: result.SourceID, FetchedAt: result.FetchedAt, Success: result.Error == "", Error: result.Error, NewItems: added}).Error
	})
}

func (r *collectionRepo) ExistingURLs(ctx context.Context, urls []string) (map[string]bool, error) {
	result := make(map[string]bool)
	if len(urls) == 0 {
		return result, nil
	}
	requested := make(map[string]bool, len(urls))
	for _, url := range urls {
		requested[url] = true
	}
	hashes := make([][]byte, len(urls))
	hashURLs := make(map[string]string, len(urls))
	for i, url := range urls {
		hashes[i] = urlDigest(url)
		if previous, exists := hashURLs[string(hashes[i])]; exists && previous != url {
			return nil, errors.New("URL SHA-256 collision")
		}
		hashURLs[string(hashes[i])] = url
	}
	var existing []string
	if err := r.data.ORM.WithContext(ctx).Model(&itemModel{}).Where("url_hash IN ?", hashes).Pluck("url", &existing).Error; err != nil {
		return nil, err
	}
	for _, url := range existing {
		if !requested[url] {
			return nil, errors.New("URL SHA-256 collision")
		}
		result[url] = true
	}
	return result, nil
}

func (r collectSourceRecord) domain() biz.CollectSource {
	return biz.CollectSource{ID: r.ID, FeedURL: r.FeedURL, ETag: r.ETag, LastModified: r.LastModified, IntervalMinutes: r.IntervalMinutes, FirstCollected: r.FirstCollected, RetryNotBefore: r.RetryNotBefore}
}

// A digest collision must fail explicitly rather than silently discard a different URL.
func verifyURLHash(tx *gorm.DB, model any, url string) error {
	var count int64
	if err := tx.Model(model).Where("url_hash = ? AND url <> ?", urlDigest(url), url).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return errors.New("URL SHA-256 collision")
	}
	return nil
}

func urlDigest(url string) []byte { hash := sha256.Sum256([]byte(url)); return hash[:] }
