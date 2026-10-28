package biz

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bogeia/vane/internal/biz/watchwords"
	"github.com/bogeia/vane/internal/infra/retry"
)

type CollectSource struct {
	ID, FeedURL, ETag, LastModified, Kind string
	IntervalMinutes                       int
	FirstCollected                        bool
	RetryNotBefore                        time.Time
}

type FeedItem struct {
	URL, Title, Body, ImageURL string
	Published, Updated         *time.Time
}

type Feed struct {
	URL                string
	Items              []FeedItem
	ETag, LastModified string
	NotModified        bool
}

type CollectedItem struct {
	URL, OriginalTitle, Body, ImageURL string
	PublishedAt, DiscoveredAt          time.Time
}

type CollectionRepository interface {
	DueSources(context.Context, time.Time) ([]CollectSource, error)
	ExistingURLs(context.Context, []string) (map[string]bool, error)
	SaveCollection(context.Context, CollectionResult, []CollectedItem) error
}

type FeedFetcher interface {
	Fetch(context.Context, CollectSource) (Feed, error)
	Article(context.Context, CollectSource, string) (string, error)
}

type Collector struct {
	repo        CollectionRepository
	fetcher     FeedFetcher
	enabled     bool
	block       map[string][][]string
	kinds       map[string]string
	now         func() time.Time
	revalidated sync.Map
}

func NewCollector(repo CollectionRepository, fetcher FeedFetcher, options CollectorOptions, now func() time.Time) *Collector {
	return &Collector{repo: repo, fetcher: fetcher, enabled: options.Enabled, block: options.Block, kinds: options.Kinds, now: now}
}

func (c *Collector) RunOnce(ctx context.Context) error {
	if !c.enabled {
		return nil
	}
	now := c.now()
	sources, err := c.repo.DueSources(ctx, now)
	if err != nil {
		return err
	}
	var failures error
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return errors.Join(failures, err)
		}
		source.Kind = c.kinds[source.ID]
		request := source
		if _, ok := c.revalidated.Load(source.ID); !ok {
			// Reapply this instance's block rules even when the feed has not changed.
			request.ETag, request.LastModified = "", ""
		}
		feed, fetchErr := c.fetcher.Fetch(ctx, request)
		source.RetryNotBefore = time.Time{}
		var items []CollectedItem
		var blocked []BlockedContent
		if fetchErr == nil && !feed.NotModified {
			slices.SortStableFunc(feed.Items, func(a, b FeedItem) int { return publicationTime(b, now).Compare(publicationTime(a, now)) })
			seen := make(map[string]bool)
			var candidates []FeedItem
			var urls []string
			for _, item := range feed.Items {
				normalized, err := normalizeURL(item.URL)
				if err != nil || seen[normalized] {
					continue
				}
				seen[normalized] = true
				item.URL = normalized
				item.Title = html.UnescapeString(item.Title)
				if matched, _ := watchwords.MatchWords(c.block[source.ID], item.Title, ""); matched {
					blocked = append(blocked, BlockedContent{SourceID: source.ID, URL: normalized, OriginalTitle: item.Title, FirstBlockedAt: now})
					continue
				}
				item.ImageURL, _ = normalizeURL(item.ImageURL)
				if source.Kind == SourceKindHFModels {
					if item.Published == nil || item.Published.Before(now.Add(-30*24*time.Hour)) || item.Published.After(now) {
						continue
					}
				} else if source.FirstCollected && publicationTime(item, now).Before(now.Add(-7*24*time.Hour)) {
					continue
				}
				if !source.FirstCollected && len(candidates) == 8 {
					continue
				}
				candidates = append(candidates, item)
				urls = append(urls, normalized)
			}
			existing, err := c.repo.ExistingURLs(ctx, urls)
			if err != nil {
				fetchErr = err
			} else {
				for _, item := range candidates {
					if existing[item.URL] {
						continue
					}
					if utf8.RuneCountInString(item.Body) < 400 {
						article, err := c.fetcher.Article(ctx, source, item.URL)
						if (source.Kind == SourceKindWebList || source.Kind == SourceKindHFModels) && (err != nil || strings.TrimSpace(article) == "") {
							continue
						}
						if err == nil && utf8.RuneCountInString(article) > utf8.RuneCountInString(item.Body) {
							item.Body = article
						}
					}
					published := publicationTime(item, now)
					if source.Kind == SourceKindHFModels && source.FirstCollected {
						published = now
					}
					items = append(items, CollectedItem{URL: item.URL, OriginalTitle: item.Title, Body: item.Body, ImageURL: item.ImageURL, PublishedAt: published, DiscoveredAt: now})
				}
			}
		}
		message := ""
		if fetchErr != nil {
			message = fetchErr.Error()
			source.RetryNotBefore = retry.NotBefore(fetchErr)
			failures = errors.Join(failures, fmt.Errorf("source %s: %w", source.ID, fetchErr))
		}
		result := NewCollectionResult(source, feed, now, message)
		if fetchErr == nil {
			result.Blocked = blocked
		}
		if err := c.repo.SaveCollection(ctx, result, items); err != nil {
			failures = errors.Join(failures, fmt.Errorf("save source %s: %w", source.ID, err))
			// The failed batch rolled back source state; record its failure in a separate transaction.
			if recordErr := c.repo.SaveCollection(ctx, NewCollectionResult(source, Feed{}, now, err.Error()), nil); recordErr != nil {
				failures = errors.Join(failures, fmt.Errorf("record source %s failure: %w", source.ID, recordErr))
			}
		} else if fetchErr == nil && !feed.NotModified {
			c.revalidated.Store(source.ID, true)
		}
	}
	return failures
}

func normalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid item URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return "", fmt.Errorf("item URL must be absolute HTTP(S) without credentials")
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", fmt.Errorf("invalid item URL query")
	}
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" || lower == "msclkid" || lower == "mc_cid" || lower == "mc_eid" {
			query.Del(key)
		}
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment, parsed.RawFragment = "", ""
	parsed.RawQuery = query.Encode()
	parsed.ForceQuery = false
	return parsed.String(), nil
}

func publicationTime(item FeedItem, now time.Time) time.Time {
	published := now
	if item.Published != nil {
		published = *item.Published
	} else if item.Updated != nil {
		published = *item.Updated
	}
	if published.After(now) {
		return now
	}
	return published
}

type CollectionResult struct {
	Blocked            []BlockedContent
	SourceID           string
	FirstCollected     bool
	ETag, LastModified string
	NextCollectAt      time.Time
	RetryNotBefore     *time.Time
	FetchedAt          time.Time
	Error              string
}

func NewCollectionResult(source CollectSource, feed Feed, now time.Time, message string) CollectionResult {
	firstCollected := source.FirstCollected || (message == "" && !feed.NotModified)
	etag, lastModified := source.ETag, source.LastModified
	if message == "" {
		if !feed.NotModified || feed.ETag != "" {
			etag = feed.ETag
		}
		if !feed.NotModified || feed.LastModified != "" {
			lastModified = feed.LastModified
		}
	}
	next := now.Add(time.Duration(source.IntervalMinutes) * time.Minute)
	if source.RetryNotBefore.After(next) {
		next = source.RetryNotBefore
	}
	var retryAt *time.Time
	if !source.RetryNotBefore.IsZero() {
		value := source.RetryNotBefore
		retryAt = &value
	}
	return CollectionResult{SourceID: source.ID, FirstCollected: firstCollected, ETag: etag, LastModified: lastModified, NextCollectAt: next, RetryNotBefore: retryAt, FetchedAt: now, Error: message}
}
