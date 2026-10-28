package biz

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sourcePreviewFetcher struct {
	items            []FeedItem
	fetched, article CollectSource
	body             string
	err              error
}

func (f *sourcePreviewFetcher) Fetch(_ context.Context, s CollectSource) (Feed, error) {
	f.fetched = s
	return Feed{Items: f.items}, f.err
}

func (f *sourcePreviewFetcher) Article(_ context.Context, s CollectSource, _ string) (string, error) {
	f.article = s
	return f.body, f.err
}

func TestSourcesPreviewDisabledSortedLimitedAndUnfiltered(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	for _, kind := range []string{SourceKindRSS, SourceKindWebList, SourceKindHFModels} {
		t.Run(kind, func(t *testing.T) {
			f := &sourcePreviewFetcher{}
			for i := range 25 {
				published := now.Add(time.Duration(i) * time.Hour)
				f.items = append(f.items, FeedItem{Title: fmt.Sprint(i), URL: fmt.Sprintf("https://example.com/%d", i), Published: &published})
			}
			uc := NewSourcesUseCase(Sources{{ID: "s", Kind: kind, FeedURL: "https://example.com/list", Enabled: false}}, f, func() time.Time { return now })
			preview, err := uc.Preview(t.Context(), "s")
			require.NoError(t, err)
			assert.Equal(t, "s", preview.ID)
			assert.Equal(t, kind, preview.Kind)
			require.Len(t, preview.Items, 20)
			assert.Equal(t, "24", preview.Items[0].Title)
			assert.Equal(t, now.Add(24*time.Hour), preview.Items[0].PublishedAt)
			assert.Equal(t, "5", preview.Items[19].Title)
			assert.Equal(t, kind, f.fetched.Kind)
			assert.Equal(t, "https://example.com/list", f.fetched.FeedURL)
			assert.Empty(t, f.article.ID)
			_, err = uc.Preview(t.Context(), "unknown")
			assert.EqualError(t, err, "source not found")
			f.err = errors.New("web list: no items matched")
			_, err = uc.Preview(t.Context(), "s")
			assert.EqualError(t, err, "web list: no items matched")
		})
	}
}

func TestCollectorWebListRequiresArticleAndPropagatesKind(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, body string
		err        error
		want       int
	}{
		{name: "failed", err: errors.New("article failed")}, {name: "empty"}, {name: "whitespace", body: "  \n"}, {name: "success", body: "Article body", want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &collectionFake{}
			f := &sourcePreviewFetcher{items: []FeedItem{{URL: "https://example.com/article", Title: "Title", Published: &now}}}
			// Fetch succeeds; Article can fail independently.
			fetcher := &articleResultFetcher{sourcePreviewFetcher: f, body: tc.body, articleErr: tc.err}
			collector := NewCollector(repo, fetcher, CollectorOptions{Enabled: true, Kinds: map[string]string{"s": SourceKindWebList}}, func() time.Time { return now })
			require.NoError(t, collector.RunOnce(t.Context()))
			require.Len(t, repo.saved, tc.want)
			assert.Equal(t, SourceKindWebList, f.fetched.Kind)
			assert.Equal(t, SourceKindWebList, f.article.Kind)
		})
	}
}

type articleResultFetcher struct {
	*sourcePreviewFetcher
	body       string
	articleErr error
}

func (f *articleResultFetcher) Article(_ context.Context, s CollectSource, _ string) (string, error) {
	f.article = s
	return f.body, f.articleErr
}
