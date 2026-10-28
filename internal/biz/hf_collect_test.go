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

type hfCollectionRepo struct {
	collectionFake
	source CollectSource
	result CollectionResult
}

func (r *hfCollectionRepo) DueSources(context.Context, time.Time) ([]CollectSource, error) {
	return []CollectSource{r.source}, nil
}

func (r *hfCollectionRepo) SaveCollection(_ context.Context, result CollectionResult, items []CollectedItem) error {
	r.saved = items
	r.result = result
	return nil
}

func TestHFCollectorCreatedAtWindowAndPublication(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	for _, firstCollected := range []bool{false, true} {
		t.Run(fmt.Sprint(firstCollected), func(t *testing.T) {
			repo := &hfCollectionRepo{source: CollectSource{ID: "s", FirstCollected: firstCollected}}
			f := &sourcePreviewFetcher{}
			createdByTitle := map[string]time.Time{}
			for _, age := range []time.Duration{time.Hour, 8 * 24 * time.Hour, 30 * 24 * time.Hour, 30*24*time.Hour + time.Second, -time.Hour} {
				created := now.Add(-age)
				createdByTitle[fmt.Sprint(age)] = created
				f.items = append(f.items, FeedItem{Title: fmt.Sprint(age), URL: fmt.Sprintf("https://huggingface.co/org/model-%d", age), Published: &created})
			}
			f.items = append(f.items, FeedItem{URL: "https://huggingface.co/org/no-date"})
			fetcher := &articleResultFetcher{sourcePreviewFetcher: f, body: "# Model card"}
			collector := NewCollector(repo, fetcher, CollectorOptions{Enabled: true, Kinds: map[string]string{"s": SourceKindHFModels}}, func() time.Time { return now })
			require.NoError(t, collector.RunOnce(t.Context()))
			require.Len(t, repo.saved, 3)
			for _, item := range repo.saved {
				want, ok := createdByTitle[item.OriginalTitle]
				require.True(t, ok, "unexpected model collected")
				if firstCollected {
					want = now
				}
				assert.Equal(t, want, item.PublishedAt)
				assert.Equal(t, now, item.DiscoveredAt)
			}
			assert.Equal(t, SourceKindHFModels, f.fetched.Kind)
			assert.Equal(t, SourceKindHFModels, f.article.Kind)
		})
	}
}

func TestHFCollectorBlocksFormatsBeforeInitialLimit(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	repo := &hfCollectionRepo{source: CollectSource{ID: "s"}}
	f := &sourcePreviewFetcher{}
	for _, suffix := range []string{"FP8", "bf16", "GGUF", "MLX", "hf", "Int4", "Int8", "FP4", "NVFP4", "AWQ", "GPTQ", "8B", "32B", "70B", "1B", "2B", "3B", "4B", "5B", "6B", "7B"} {
		f.items = append(f.items, FeedItem{Title: "org/Model-" + suffix, URL: "https://huggingface.co/org/Model-" + suffix, Published: &now})
	}
	collector := NewCollector(repo, &articleResultFetcher{sourcePreviewFetcher: f, body: "Model card"}, CollectorOptions{Enabled: true, Kinds: map[string]string{"s": SourceKindHFModels}, Block: map[string][][]string{"s": {{"/-(FP8|FP4|NVFP4|BF16|Int4|Int8|AWQ|GPTQ|GGUF|MLX|hf)$/"}}}}, func() time.Time { return now })
	require.NoError(t, collector.RunOnce(t.Context()))
	require.Len(t, repo.saved, 8)
	assert.Len(t, repo.result.Blocked, 11)
	assert.Equal(t, "org/Model-8B", repo.saved[0].OriginalTitle)
	assert.Equal(t, "org/Model-5B", repo.saved[7].OriginalTitle)
}

func TestHFCollectorSkipsUnavailableModelCards(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		body string
		err  error
	}{{"", nil}, {" \n", nil}, {"", errors.New("404")}} {
		repo := &hfCollectionRepo{source: CollectSource{ID: "s"}}
		f := &sourcePreviewFetcher{items: []FeedItem{{Title: "org/model", URL: "https://huggingface.co/org/model", Published: &now}}}
		collector := NewCollector(repo, &articleResultFetcher{sourcePreviewFetcher: f, body: tc.body, articleErr: tc.err}, CollectorOptions{Enabled: true, Kinds: map[string]string{"s": SourceKindHFModels}}, func() time.Time { return now })
		require.NoError(t, collector.RunOnce(t.Context()))
		assert.Empty(t, repo.saved)
	}
}

func TestSourcesPreviewKeepsUndatedTimeEmpty(t *testing.T) {
	f := &sourcePreviewFetcher{items: []FeedItem{{Title: "No date", URL: "https://example.com/no-date"}}}
	uc := NewSourcesUseCase(Sources{{ID: "s", Kind: SourceKindRSS, FeedURL: "https://example.com/rss"}}, f, func() time.Time { t.Fatal("preview must not invent a timestamp"); return time.Time{} })
	preview, err := uc.Preview(t.Context(), "s")
	require.NoError(t, err)
	require.Len(t, preview.Items, 1)
	assert.True(t, preview.Items[0].PublishedAt.IsZero())
}
