package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/data"
)

func TestDatabaseURLDigestsAndStoryMembership(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	store, cleanup, err := data.Open(t.Context(), loadTestConfig(t, databaseURL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := data.Migrate(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	db := testDB(t, store)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES ('relations','Source','official','https://example.test/feed',true,60)`); err != nil {
		t.Fatal(err)
	}
	repo := data.NewCollectionRepo(store)
	now := time.Now().UTC()
	// Hash output is intentionally difficult to compress so the URLs exceed a B-tree key.
	var long strings.Builder
	for i := range 200 {
		hash := sha256.Sum256([]byte(string(rune(i))))
		long.WriteString(hex.EncodeToString(hash[:]))
	}
	urls := []string{"https://example.test/" + long.String(), "https://example.test/" + long.String() + "other", `https://example.test/中文\x41\path`}
	result := biz.CollectionResult{SourceID: "relations", FetchedAt: now, NextCollectAt: now.Add(time.Hour)}
	var items []biz.CollectedItem
	for _, url := range urls {
		items = append(items, biz.CollectedItem{URL: url, OriginalTitle: "Title", PublishedAt: now, DiscoveredAt: now})
	}
	for range 2 {
		if err := repo.SaveCollection(t.Context(), result, items); err != nil {
			t.Fatal(err)
		}
	}
	existing, err := repo.ExistingURLs(t.Context(), urls)
	if err != nil || len(existing) != len(urls) {
		t.Fatalf("existing URLs=%v err=%v", existing, err)
	}
	for _, url := range urls {
		var digest []byte
		if err := db.QueryRowContext(t.Context(), `SELECT url_hash FROM items WHERE url=$1`, url).Scan(&digest); err != nil {
			t.Fatal(err)
		}
		want := sha256.Sum256([]byte(url))
		if string(digest) != string(want[:]) {
			t.Fatalf("digest mismatch for URL")
		}
	}
	blockedURL := urls[0] + "blocked"
	result.Blocked = []biz.BlockedContent{{URL: blockedURL, OriginalTitle: "Blocked", FirstBlockedAt: now}, {URL: urls[0], OriginalTitle: "Already collected", FirstBlockedAt: now}}
	for range 2 {
		if err := repo.SaveCollection(t.Context(), result, nil); err != nil {
			t.Fatal(err)
		}
	}
	var blockedCount int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM blocked_contents`).Scan(&blockedCount); err != nil {
		t.Fatal(err)
	}
	if blockedCount != 1 {
		t.Fatalf("blocked rows=%d, want one", blockedCount)
	}

	if _, err := db.ExecContext(t.Context(), `INSERT INTO stories(title) VALUES ('First'),('Second'); INSERT INTO push_targets(name,first_seen_at) VALUES ('target',now())`); err != nil {
		t.Fatal(err)
	}
	var story1, story2, item int64
	if err := db.QueryRowContext(t.Context(), `SELECT min(id),max(id) FROM stories`).Scan(&story1, &story2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE stories SET representative_item_id=(SELECT id FROM items WHERE url=$1) WHERE id=$2`, urls[1], story2); err == nil {
		t.Fatal("ungrouped representative accepted")
	}
	if err := db.QueryRowContext(t.Context(), `UPDATE items SET story_id=$1 WHERE url=$2 RETURNING id`, story1, urls[0]).Scan(&item); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE stories SET representative_item_id=$1 WHERE id=$2`, item, story2); err == nil {
		t.Fatal("cross-story representative accepted")
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE stories SET representative_item_id=$1 WHERE id=$2`, item, story1); err != nil {
		t.Fatal(err)
	}
	push := `INSERT INTO push_deliveries(target_name,story_id,item_id,status,attempts,next_attempt_at,last_error,created_at,updated_at) VALUES ('target',$1,$2,'sent',1,now(),'',now(),now())`
	if _, err := db.ExecContext(t.Context(), push, story2, item); err == nil {
		t.Fatal("cross-story push accepted")
	}
	if _, err := db.ExecContext(t.Context(), push, story1, item); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE items SET story_id=$1 WHERE id=$2`, story2, item); err == nil {
		t.Fatal("referenced item moved to another story")
	}
	for name, columns := range map[string]string{"push_deliveries_story_idx": "(story_id)", "push_deliveries_item_story_idx": "(item_id, story_id)"} {
		var definition string
		if err := db.QueryRowContext(t.Context(), `SELECT indexdef FROM pg_indexes WHERE indexname=$1`, name).Scan(&definition); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(definition, columns) {
			t.Fatalf("index %s: %s", name, definition)
		}
	}
	if err := data.Migrate(t.Context(), store); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateReplacesObsoleteURLAndMembershipKeys(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	store, cleanup, err := data.Open(t.Context(), loadTestConfig(t, databaseURL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := data.Migrate(t.Context(), store); err != nil {
		t.Fatal(err)
	}
	// Recreate the old keys on populated tables, without maintaining a second schema.
	db := testDB(t, store)
	if _, err := db.ExecContext(t.Context(), `
 INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES ('upgrade','Source','official','https://example.test/feed',true,60);
 INSERT INTO items(source_id,url,original_title,published_at) VALUES ('upgrade','https://example.test/retained','Retained',now());
 INSERT INTO blocked_contents(url,source_id,original_title,first_blocked_at) VALUES ('https://example.test/blocked-retained','upgrade','Blocked',now());
 ALTER TABLE items ADD CONSTRAINT uni_items_url UNIQUE(url);
 ALTER TABLE blocked_contents DROP CONSTRAINT blocked_contents_pkey;
 ALTER TABLE blocked_contents ADD PRIMARY KEY(url);
 ALTER TABLE blocked_contents DROP COLUMN url_hash;
 ALTER TABLE items DROP COLUMN url_hash;
 ALTER TABLE stories DROP CONSTRAINT stories_representative_membership_fk;
 ALTER TABLE stories ADD CONSTRAINT stories_representative_item_fk FOREIGN KEY(representative_item_id) REFERENCES items(id);
 ALTER TABLE push_deliveries DROP CONSTRAINT push_deliveries_item_membership_fk;
 ALTER TABLE push_deliveries ADD CONSTRAINT push_deliveries_item_fk FOREIGN KEY(item_id) REFERENCES items(id);
 `); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := data.Migrate(t.Context(), store); err != nil {
			t.Fatal(err)
		}
	}
	var obsolete int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM pg_constraint WHERE conname IN ('uni_items_url','stories_representative_item_fk','push_deliveries_item_fk')`).Scan(&obsolete); err != nil {
		t.Fatal(err)
	}
	if obsolete != 0 {
		t.Fatalf("%d obsolete constraints remain", obsolete)
	}
	var retained int
	if err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM items WHERE url='https://example.test/retained' AND octet_length(url_hash)=32)+(SELECT count(*) FROM blocked_contents WHERE url='https://example.test/blocked-retained' AND octet_length(url_hash)=32)`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != 2 {
		t.Fatalf("retained rows=%d", retained)
	}
}
