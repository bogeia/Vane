package integration

import (
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
)

func TestSourceSyncKeepsRetryDeadlineAndResetsChangedFeed(t *testing.T) {
	store, now := groupStore(t)
	source := config.Source{Kind: biz.SourceKindRSS, ID: "group", Name: "Group", Tier: "official", FeedURL: "https://example.test/feed", Enabled: true, IntervalMinutes: 60}
	until := now.Add(2 * time.Hour)
	err := data.NewCollectionRepo(store).SaveCollection(t.Context(), biz.NewCollectionResult(biz.CollectSource{ID: source.ID, IntervalMinutes: 60, FirstCollected: true, RetryNotBefore: until, ETag: "old", LastModified: "old"}, biz.Feed{}, now, "busy"), []biz.CollectedItem{{URL: "https://example.test/history", OriginalTitle: "History", PublishedAt: now, DiscoveredAt: now}})
	if err != nil {
		t.Fatal(err)
	}
	source.IntervalMinutes = 1
	if err := data.SyncSources(t.Context(), store, []config.Source{source}); err != nil {
		t.Fatal(err)
	}
	var next, retryAt time.Time
	if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT next_collect_at,retry_not_before FROM sources WHERE id='group'").Scan(&next, &retryAt); err != nil {
		t.Fatal(err)
	}
	if !next.Equal(until) || !retryAt.Equal(until) {
		t.Fatalf("next=%v retry=%v; want %v", next, retryAt, until)
	}
	source.FeedURL = "https://example.test/new-feed"
	if err := data.SyncSources(t.Context(), store, []config.Source{source}); err != nil {
		t.Fatal(err)
	}
	var reset bool
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT etag='' AND last_modified='' AND last_error='' AND NOT first_collected AND last_collected_at IS NULL AND retry_not_before IS NULL AND next_collect_at <= now() FROM sources WHERE id='group'`).Scan(&reset); err != nil {
		t.Fatal(err)
	}
	if !reset {
		t.Fatal("changed feed retained collection state")
	}
	var items int
	if err := testDB(t, store).QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&items); err != nil {
		t.Fatal(err)
	}
	if items != 1 {
		t.Fatalf("historical items=%d", items)
	}
}
