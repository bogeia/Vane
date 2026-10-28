package integration

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
)

func TestBlockedContentStatusWindowAndRollback(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: "https://example.test/feed", Enabled: true, IntervalMinutes: 1}}
	now := time.Now().UTC().Truncate(time.Second)
	app, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	repo := data.NewCollectionRepo(app.Data)
	for i := range 22 {
		at := now.Add(-time.Duration(i+1) * time.Hour)
		if i == 21 {
			at = now.Add(-8 * 24 * time.Hour)
		}
		result := biz.NewCollectionResult(biz.CollectSource{ID: "fixture", IntervalMinutes: 1}, biz.Feed{}, at, "")
		result.Blocked = []biz.BlockedContent{{URL: fmt.Sprintf("https://example.test/blocked-%02d", i), OriginalTitle: fmt.Sprintf("blocked %02d", i), FirstBlockedAt: at}}
		if err := repo.SaveCollection(t.Context(), result, nil); err != nil {
			t.Fatal(err)
		}
	}
	itemResult := biz.NewCollectionResult(biz.CollectSource{ID: "fixture", IntervalMinutes: 1}, biz.Feed{}, now, "")
	if err := repo.SaveCollection(t.Context(), itemResult, []biz.CollectedItem{{URL: "https://example.test/ai", OriginalTitle: "AI", PublishedAt: now, DiscoveredAt: now}}); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), "UPDATE items SET prefilter = 'PASS'"); err != nil {
		t.Fatal(err)
	}
	status, err := biz.NewOperationsUseCase(data.NewOperationsRepo(app.Data)).Status(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if status.Sources[0].Health.NewBlocked != 21 || status.Sources[0].Health.Prefiltered != 1 || status.Sources[0].Health.AIRelatedRate == nil || *status.Sources[0].Health.AIRelatedRate != 1 {
		t.Fatalf("health=%+v", status.Sources[0].Health)
	}
	if len(status.RecentBlocked) != 20 {
		t.Fatalf("recent blocked=%d", len(status.RecentBlocked))
	}
	for i, row := range status.RecentBlocked {
		if row.OriginalTitle != fmt.Sprintf("blocked %02d", i) {
			t.Fatalf("recent blocked[%d]=%+v", i, row)
		}
	}
	// Reject the final fetch record, after items, blocked contents and source state were written.
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), `ALTER TABLE fetch_records ADD CONSTRAINT fixture_reject CHECK (error <> 'reject')`); err != nil {
		t.Fatal(err)
	}
	var sourceBefore string
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), `SELECT row_to_json(s)::text FROM sources s WHERE id='fixture'`).Scan(&sourceBefore); err != nil {
		t.Fatal(err)
	}
	var fetchesBefore int
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), `SELECT count(*) FROM fetch_records`).Scan(&fetchesBefore); err != nil {
		t.Fatal(err)
	}
	result := biz.NewCollectionResult(biz.CollectSource{ID: "fixture", IntervalMinutes: 1}, biz.Feed{ETag: "rollback-etag"}, now.Add(time.Hour), "reject")
	result.Blocked = []biz.BlockedContent{{URL: "https://example.test/rolled-back", OriginalTitle: "rolled back", FirstBlockedAt: now}}
	err = repo.SaveCollection(t.Context(), result, []biz.CollectedItem{{URL: "https://example.test/rejected", OriginalTitle: "reject", PublishedAt: now, DiscoveredAt: now}})
	if err == nil {
		t.Fatal("expected collection failure")
	}
	var sourceAfter string
	var fetchesAfter, leakedItems, leakedBlocked int
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), `SELECT row_to_json(s)::text, (SELECT count(*) FROM fetch_records), (SELECT count(*) FROM items WHERE url='https://example.test/rejected'), (SELECT count(*) FROM blocked_contents WHERE url='https://example.test/rolled-back') FROM sources s WHERE id='fixture'`).Scan(&sourceAfter, &fetchesAfter, &leakedItems, &leakedBlocked); err != nil {
		t.Fatal(err)
	}
	if sourceAfter != sourceBefore || fetchesAfter != fetchesBefore || leakedItems != 0 || leakedBlocked != 0 {
		t.Fatalf("collection rollback failed: sourceChanged=%v fetches=%d/%d items=%d blocked=%d", sourceAfter != sourceBefore, fetchesAfter, fetchesBefore, leakedItems, leakedBlocked)
	}
	status, err = biz.NewOperationsUseCase(data.NewOperationsRepo(app.Data)).Status(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if status.Sources[0].Health.NewBlocked != 21 || status.RecentBlocked[0].OriginalTitle != "blocked 00" {
		t.Fatalf("failed collection leaked: %+v", status)
	}
}
