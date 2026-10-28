package biz

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

func TestNormalizeSourceURL(t *testing.T) {
	for _, raw := range []string{
		"HTTP://EXAMPLE.COM:80/feed/?utm_source=x&keep=yes#fragment",
		"http://example.com/feed?UTM_CAMPAIGN=x&fbclid=x&gclid=x&msclkid=x&mc_cid=x&mc_eid=x&keep=yes", "https://example.com:443/feed?keep=yes",
	} {
		got, err := NormalizeSourceURL(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, "example.com/feed?keep=yes", got, raw)
	}
	_, err := NormalizeSourceURL("file:///etc/passwd")
	assert.Error(t, err, "accepted file URL")
}

func TestSourceID(t *testing.T) {
	used := map[string]bool{"rss-example-com-feed": true}
	assert.Equal(t, "rss-example-com-feed-xml", SourceID("https://www.Example.com/feed.xml", used))
	assert.Equal(t, "rss-example-com-feed-2", SourceID("https://example.com/feed", used))
	assert.Equal(t, "rss-example-com-feed-3", SourceID("https://example.com/feed", used))
}

func TestNormalizeSourceURLPreservesEscapedPath(t *testing.T) {
	got, err := NormalizeSourceURL("https://example.com/a%2Fb/")
	require.NoError(t, err)
	assert.Equal(t, "example.com/a%2Fb", got)
}

func TestSourceIDRemovesWWWCaseInsensitively(t *testing.T) {
	assert.Equal(t, "rss-example-com-feed", SourceID("https://WWW.Example.com/feed", map[string]bool{}))
}

func TestNormalizeSourceURLPreservesEncodedTrailingSlash(t *testing.T) {
	got, err := NormalizeSourceURL("https://example.com/feed%2F")
	require.NoError(t, err)
	assert.Equal(t, "example.com/feed%2F", got)
}

type redirectProbe struct {
	failDirect bool
	now        time.Time
}

func (f redirectProbe) Fetch(_ context.Context, source CollectSource) (Feed, error) {
	if source.FeedURL == "https://example.com/b" && f.failDirect {
		return Feed{}, fmt.Errorf("temporary failure")
	}
	return Feed{URL: "https://example.com/b", Items: []FeedItem{{Published: &f.now}}}, nil
}
func (redirectProbe) Article(context.Context, CollectSource, string) (string, error) { return "", nil }
func TestSourcesImportDeduplicatesSuccessfulFinalURLs(t *testing.T) {
	for _, failDirect := range []bool{true, false} {
		t.Run(fmt.Sprint(failDirect), func(t *testing.T) {
			now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
			u := NewSourcesUseCase(nil, redirectProbe{failDirect: failDirect, now: now}, func() time.Time { return now })
			result, err := u.Import(t.Context(), []SourceCandidate{{Name: "redirect", URL: "https://example.com/a"}, {Name: "direct", URL: "https://example.com/b"}}, taxonomy.TierOfficial, 60)
			require.NoError(t, err)
			want := SkipSourceDuplicate
			if failDirect {
				want = SkipSourceUnavailable
			}
			wantResult := SourceImport{
				Sources: []Source{{Kind: SourceKindRSS, ID: "rss-example-com-b", Name: "redirect", Tier: taxonomy.TierOfficial, FeedURL: "https://example.com/b", Enabled: true, IntervalMinutes: 60}},
				Skipped: []SourceSkip{{URL: "https://example.com/b", Reason: want}},
			}
			if diff := cmp.Diff(wantResult, result); diff != "" {
				t.Errorf("import mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
