package biz

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type collectionFake struct {
	existing map[string]bool
	saved    []CollectedItem
}

func (f *collectionFake) DueSources(context.Context, time.Time) ([]CollectSource, error) {
	return []CollectSource{{ID: "s"}}, nil
}

func (f *collectionFake) ExistingURLs(context.Context, []string) (map[string]bool, error) {
	return f.existing, nil
}

func (f *collectionFake) SaveCollection(_ context.Context, _ CollectionResult, items []CollectedItem) error {
	f.saved = items
	return nil
}

type feedFake struct {
	items    []FeedItem
	articles []string
}

func (f *feedFake) Fetch(context.Context, CollectSource) (Feed, error) {
	return Feed{Items: f.items}, nil
}

func (f *feedFake) Article(_ context.Context, _ CollectSource, url string) (string, error) {
	f.articles = append(f.articles, url)
	return "", nil
}

func TestCollectorInitialLimitBeforeExistingFilter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &collectionFake{existing: map[string]bool{"https://example.com/0": true}}
	feed := &feedFake{}
	for i := range 10 {
		published := now.Add(-time.Duration(i) * time.Minute)
		feed.items = append(feed.items, FeedItem{URL: fmt.Sprintf("https://example.com/%d?utm_source=x", i), Published: &published})
	}
	if err := NewCollector(repo, feed, CollectorOptions{Enabled: true, Kinds: map[string]string{"s": SourceKindRSS}}, func() time.Time { return now }).RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(repo.saved) != 7 || len(feed.articles) != 7 {
		t.Fatalf("saved=%d articles=%d; want 7", len(repo.saved), len(feed.articles))
	}
	for _, url := range feed.articles {
		if url == "https://example.com/0" || url == "https://example.com/8" || url == "https://example.com/9" {
			t.Fatalf("unexpected article fetch %s", url)
		}
	}
	repo.existing = map[string]bool{}
	for _, item := range repo.saved {
		repo.existing[item.URL] = true
	}
	repo.existing["https://example.com/0"] = true
	feed.articles = nil
	if err := NewCollector(repo, feed, CollectorOptions{Enabled: true, Kinds: map[string]string{"s": SourceKindRSS}}, func() time.Time { return now }).RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(feed.articles) != 0 || len(repo.saved) != 0 {
		t.Fatal("duplicate feed fetched articles or saved items")
	}
}
