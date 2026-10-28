package integration

import (
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

func TestTopicsRebuildCommandOnlyChangesTopicsAndIsIdempotent(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	app := assembleTestApp(t, cfg)
	db := testDB(t, app.Data)
	processed := time.Now().UTC().Truncate(time.Microsecond)
	// More than one batch; titles mention a company but legacy subjects are empty.
	if _, err := db.ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,title,published_at,processed_at,stage,selected,category,tags) SELECT $1,'https://example.test/legacy/'||n,'OpenAI Agent','OpenAI Agent',now(),$2,'done',n%2=0,'产品',ARRAY['Agent'] FROM generate_series(1,205) n`, cfg.Sources[0].ID, processed); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,published_at,stage,category,tags) VALUES($1,'https://example.test/pending','pending',now(),'new','产品',ARRAY['Agent'])`, cfg.Sources[0].ID); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.QueryRowContext(t.Context(), `SELECT jsonb_agg(to_jsonb(i)-'topics' ORDER BY id)::text FROM items i`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	configPath := writeSubprocessConfig(t, databaseURL, "127.0.0.1:8080", cfg.Web.Dir)
	binary := buildVane(t)
	for _, wantUpdated := range []int{205, 0} {
		output, err := exec.CommandContext(t.Context(), binary, "topics", "rebuild", "--conf", configPath).Output()
		if err != nil {
			t.Fatal("topics rebuild failed (output suppressed to protect configuration)")
		}
		var result struct {
			Checked int `json:"checked"`
			Updated int `json:"updated"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatal(err)
		}
		if result.Checked != 205 || result.Updated != wantUpdated {
			t.Fatalf("result=%+v, want checked=205 updated=%d", result, wantUpdated)
		}
	}
	var after string
	if err := db.QueryRowContext(t.Context(), `SELECT jsonb_agg(to_jsonb(i)-'topics' ORDER BY id)::text FROM items i`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("rebuild changed fields other than topics")
	}
	var invalid int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM items WHERE (stage='done' AND topics != ARRAY['openai','agent','product']) OR (stage='new' AND cardinality(topics) != 0)`).Scan(&invalid); err != nil {
		t.Fatal(err)
	}
	if invalid != 0 {
		t.Fatalf("%d rows have incorrect topics", invalid)
	}
}
