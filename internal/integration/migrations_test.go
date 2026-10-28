package integration

import (
	"strings"
	"sync"
	"testing"

	"github.com/bogeia/vane/internal/data"
)

func TestMigrateCreatesConstrainedSchema(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	store, cleanup, err := data.Open(t.Context(), loadTestConfig(t, databaseURL))
	if err != nil {
		t.Fatalf("open migration test database: %s", redactMigrationError(databaseURL, err))
	}
	t.Cleanup(cleanup)
	if err := data.Migrate(t.Context(), store); err != nil {
		t.Fatalf("migrate empty database: %s", redactMigrationError(databaseURL, err))
	}

	for _, table := range []string{"sources", "items", "stories", "reports", "fetch_records", "blocked_contents"} {
		var exists bool
		if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("migration did not create table %s", table)
		}
	}

	for _, table := range []string{"items", "stories", "fetch_records"} {
		var identity, generation string
		if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT is_identity, identity_generation FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name='id'`, table).Scan(&identity, &generation); err != nil {
			t.Fatalf("inspect %s.id identity: %v", table, err)
		}
		if identity != "YES" || generation != "ALWAYS" {
			t.Errorf("%s.id identity = %s/%s, want YES/ALWAYS", table, identity, generation)
		}
	}

	var pendingIndex, publicIndex bool
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT COALESCE((SELECT indpred IS NOT NULL FROM pg_index WHERE indexrelid='items_pending_idx'::regclass), false), COALESCE((SELECT indpred IS NOT NULL FROM pg_index WHERE indexrelid='items_public_idx'::regclass), false)`).Scan(&pendingIndex, &publicIndex); err != nil {
		t.Fatalf("inspect partial indexes: %v", err)
	}
	if !pendingIndex || !publicIndex {
		t.Fatalf("partial indexes pending/public = %v/%v, want true/true", pendingIndex, publicIndex)
	}

	var fetchIndex string
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT indexdef FROM pg_indexes WHERE schemaname='public' AND indexname='fetch_records_source_time_idx'`).Scan(&fetchIndex); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fetchIndex, "(source_id, fetched_at)") {
		t.Fatalf("fetch record index=%s, want source and fetch time", fetchIndex)
	}

	var blockedPrimaryKey string
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='blocked_contents'::regclass AND contype='p'`).Scan(&blockedPrimaryKey); err != nil {
		t.Fatal(err)
	}
	if blockedPrimaryKey != "PRIMARY KEY (url_hash)" {
		t.Fatalf("blocked primary key=%s", blockedPrimaryKey)
	}
	for name, columns := range map[string]string{"blocked_contents_source_time_idx": "(source_id, first_blocked_at)", "blocked_contents_time_idx": "(first_blocked_at)"} {
		var definition string
		if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT indexdef FROM pg_indexes WHERE schemaname='public' AND indexname=$1`, name).Scan(&definition); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(definition, columns) {
			t.Fatalf("%s=%s, want %s", name, definition, columns)
		}
	}

	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES ('migration-source','Source','official','https://example.test/feed',true,60)`); err != nil {
		t.Fatalf("insert source using defaults: %v", err)
	}
	var firstCollected, dueIsInfinite bool
	var etag, lastModified, lastError string
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT first_collected, etag, last_modified, last_error, next_collect_at='-infinity'::timestamptz FROM sources WHERE id='migration-source'`).Scan(&firstCollected, &etag, &lastModified, &lastError, &dueIsInfinite); err != nil {
		t.Fatalf("read source defaults: %v", err)
	}
	if firstCollected || etag != "" || lastModified != "" || lastError != "" || !dueIsInfinite {
		t.Errorf("source defaults = firstCollected:%v etag:%q lastModified:%q lastError:%q dueIsInfinite:%v", firstCollected, etag, lastModified, lastError, dueIsInfinite)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,published_at) VALUES ('migration-source','https://example.test/defaults','Defaults',now())`); err != nil {
		t.Fatalf("insert item using defaults: %v", err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO blocked_contents(url,source_id,original_title,first_blocked_at) VALUES ('https://example.test/blocked','migration-source','First',now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO blocked_contents(url,source_id,original_title,first_blocked_at) VALUES ('https://example.test/blocked','migration-source','Second',now())`); err == nil {
		t.Fatal("duplicate blocked URL was accepted")
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES ('blocked-only-source','Blocked source','official','https://example.test/blocked-feed',true,60)`); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO blocked_contents(url,source_id,original_title,first_blocked_at) VALUES ('https://example.test/blocked-only','blocked-only-source','Blocked',now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `DELETE FROM sources WHERE id='blocked-only-source'`); err == nil {
		t.Fatal("source referenced by blocked content was deleted")
	}
	var stage string
	var attempts int
	var selected, roundup, emptyTitle, emptyTags, emptySubjects, nullFact bool
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT stage,attempts,selected,roundup,title='',array_length(tags,1) IS NULL,cardinality(subjects)=0,fact IS NULL FROM items WHERE url='https://example.test/defaults'`).Scan(&stage, &attempts, &selected, &roundup, &emptyTitle, &emptyTags, &emptySubjects, &nullFact); err != nil {
		t.Fatalf("read item defaults: %v", err)
	}
	if stage != "new" || attempts != 0 || selected || roundup || !emptyTitle || !emptyTags || !emptySubjects || !nullFact {
		t.Errorf("item defaults = stage:%q attempts:%d selected:%v roundup:%v emptyTitle:%v emptyTags:%v", stage, attempts, selected, roundup, emptyTitle, emptyTags)
	}

	for _, test := range []struct {
		name string
		sql  string
	}{
		{"blocked source foreign key", `INSERT INTO blocked_contents(url,source_id,original_title,first_blocked_at) VALUES ('https://example.test/blocked-missing-source','missing-source','Title',now())`},
		{"blocked URL not null", `INSERT INTO blocked_contents(url,source_id,original_title,first_blocked_at) VALUES (NULL,'migration-source','Title',now())`},
		{"blocked source not null", `INSERT INTO blocked_contents(url,source_id,original_title,first_blocked_at) VALUES ('https://example.test/blocked-null-source',NULL,'Title',now())`},
		{"blocked title not null", `INSERT INTO blocked_contents(url,source_id,original_title,first_blocked_at) VALUES ('https://example.test/blocked-null-title','migration-source',NULL,now())`},
		{"blocked time not null", `INSERT INTO blocked_contents(url,source_id,original_title,first_blocked_at) VALUES ('https://example.test/blocked-null-time','migration-source','Title',NULL)`},
		{"fetch source foreign key", `INSERT INTO fetch_records(source_id,fetched_at,success,error,new_items) VALUES ('missing-source',now(),true,'',0)`},
		{"nonnegative new items", `INSERT INTO fetch_records(source_id,fetched_at,success,error,new_items) VALUES ('migration-source',now(),true,'',-1)`},
		{"reject old tier T1", `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES ('old-T1','Source','T1','https://example.test/feed',true,60)`},
		{"reject old tier T1_5", `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES ('old-T1_5','Source','T1_5','https://example.test/feed',true,60)`},
		{"reject old tier T2", `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES ('old-T2','Source','T2','https://example.test/feed',true,60)`},
		{"tier check", `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES ('bad-tier','Source','bad','https://example.test/feed',true,60)`},
		{"positive interval check", `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES ('bad-interval','Source','official','https://example.test/feed',true,0)`},
		{"item source foreign key", `INSERT INTO items(source_id,url,original_title,published_at) VALUES ('missing-source','https://example.test/1','Title',now())`},
		{"item stage check", `INSERT INTO items(source_id,url,original_title,published_at,stage) VALUES ('migration-source','https://example.test/bad-stage','Title',now(),'unknown')`},
		{"nonnegative attempts check", `INSERT INTO items(source_id,url,original_title,published_at,attempts) VALUES ('migration-source','https://example.test/bad-attempts','Title',now(),-1)`},
		{"score range check", `INSERT INTO items(source_id,url,original_title,published_at,score1) VALUES ('migration-source','https://example.test/bad-score','Title',now(),101)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := testDB(t, store).ExecContext(t.Context(), test.sql); err == nil {
				t.Fatal("invalid row was accepted")
			}
		})
	}

	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,published_at) VALUES ('migration-source','https://example.test/duplicate','First',now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,published_at) VALUES ('migration-source','https://example.test/duplicate','Second',now())`); err == nil {
		t.Fatal("duplicate item URL was accepted")
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO items(id,source_id,url,original_title,published_at) VALUES (100,'migration-source','https://example.test/identity','Explicit',now())`); err == nil {
		t.Fatal("explicit item identity was accepted without OVERRIDING SYSTEM VALUE")
	}
}

func TestMigrateDoesNotChangeBusinessStateWhenRepeated(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	store, cleanup, err := data.Open(t.Context(), loadTestConfig(t, databaseURL))
	if err != nil {
		t.Fatalf("open migration test database: %s", redactMigrationError(databaseURL, err))
	}
	t.Cleanup(cleanup)
	if err := data.Migrate(t.Context(), store); err != nil {
		t.Fatalf("migrate empty database: %s", redactMigrationError(databaseURL, err))
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES ('current-source','Current','official','https://current.test/feed',true,90)`); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO blocked_contents(url,source_id,original_title,first_blocked_at) VALUES ('https://current.test/blocked','current-source','Blocked title','2026-10-21T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	var storyID, itemID int64
	if err := testDB(t, store).QueryRowContext(t.Context(), `INSERT INTO stories(title) VALUES ('Current story') RETURNING id`).Scan(&storyID); err != nil {
		t.Fatalf("seed story: %v", err)
	}
	if err := testDB(t, store).QueryRowContext(t.Context(), `INSERT INTO items(source_id,story_id,url,original_title,body,published_at,stage,attempts,score1,score2,title,summary,reason,category,tags,selected) VALUES ('current-source',$1,'https://current.test/item','Original','Original body',now(),'done',2,70,80,'Written title','Written summary','Reason','AIHOT',ARRAY['AI','RAG'],true) RETURNING id`, storyID).Scan(&itemID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE stories SET representative_item_id=$1 WHERE id=$2`, itemID, storyID); err != nil {
		t.Fatalf("set story representative: %v", err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `INSERT INTO reports(date,content) VALUES ('2026-10-21','{"headline":{"title":"Snapshot"}}')`); err != nil {
		t.Fatalf("seed report snapshot: %v", err)
	}
	const updatedDue = "2099-01-01T00:00:00Z"
	if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE sources SET next_collect_at=$1,last_error='newer source state' WHERE id='current-source'`, updatedDue); err != nil {
		t.Fatalf("update source business state: %v", err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET body='Updated body',summary='Updated summary',attempts=3,roundup=true WHERE id=$1`, itemID); err != nil {
		t.Fatalf("update item business state: %v", err)
	}
	if err := data.Migrate(t.Context(), store); err != nil {
		t.Fatalf("repeat migration: %s", redactMigrationError(databaseURL, err))
	}
	var blockedPreserved bool
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT source_id='current-source' AND original_title='Blocked title' AND first_blocked_at='2026-10-21T12:00:00Z'::timestamptz FROM blocked_contents WHERE url='https://current.test/blocked'`).Scan(&blockedPreserved); err != nil {
		t.Fatal(err)
	}
	if !blockedPreserved {
		t.Fatal("repeat migration changed blocked content")
	}
	var due, sourceError, body, summary, stage string
	var attempts int
	var roundup bool
	var storedReport string
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT s.next_collect_at::text,s.last_error,i.body,i.summary,i.stage,i.attempts,i.roundup,r.content::text FROM sources s JOIN items i ON i.source_id=s.id CROSS JOIN reports r WHERE s.id='current-source' AND i.id=$1 AND r.date='2026-10-21'`, itemID).Scan(&due, &sourceError, &body, &summary, &stage, &attempts, &roundup, &storedReport); err != nil {
		t.Fatalf("read business state after repeated migration: %v", err)
	}
	if !strings.Contains(due, "2099") || sourceError != "newer source state" || body != "Updated body" || summary != "Updated summary" || stage != "done" || attempts != 3 || !roundup || !strings.Contains(storedReport, "Snapshot") {
		t.Fatalf("repeated migration changed business state: due=%q sourceError=%q body=%q summary=%q stage=%q attempts=%d roundup:%v report=%q", due, sourceError, body, summary, stage, attempts, roundup, storedReport)
	}
}

func TestMigrateIsSafeWhenCalledConcurrently(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	store, cleanup, err := data.Open(t.Context(), loadTestConfig(t, databaseURL))
	if err != nil {
		t.Fatalf("open migration test database: %s", redactMigrationError(databaseURL, err))
	}
	t.Cleanup(cleanup)

	const callers = 4
	var wg sync.WaitGroup
	errCh := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			errCh <- data.Migrate(t.Context(), store)
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent migration failed: %s", redactMigrationError(databaseURL, err))
		}
	}
	var tableCount int
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('sources','items','stories','reports','fetch_records')`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 5 {
		t.Fatalf("concurrent migration created %d application tables, want 5", tableCount)
	}
}

func redactMigrationError(databaseURL string, err error) string {
	return strings.ReplaceAll(err.Error(), databaseURL, "[redacted database URL]")
}
