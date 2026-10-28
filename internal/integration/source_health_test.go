package integration

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/infra/fetch"
)

func TestCollectorWritesOneFetchRecordPerAttempt(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		title   string
		success bool
		added   int
	}{
		{"success", http.StatusOK, "AI news", true, 1},
		{"not modified", http.StatusNotModified, "", true, 0},
		{"fetch failure", http.StatusNotFound, "", false, 0},
		{"save failure rolls back", http.StatusOK, "Invalid\x00title", false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			databaseURL := testDatabaseURL(t)
			resetTestSchema(t, databaseURL)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/feed+json")
				w.WriteHeader(test.status)
				if test.status == http.StatusOK {
					items := []map[string]any{{"id": "fixture", "url": "https://example.test/health", "title": test.title, "content_text": fixtureBody, "date_published": "2026-10-21T11:00:00Z"}}
					if test.name == "save failure rolls back" {
						items = append(items, map[string]any{"id": "valid", "url": "https://example.test/valid", "title": "Valid item", "content_text": fixtureBody, "date_published": "2026-10-22T12:00:00Z"})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"version": "https://jsonfeed.org/version/1.1", "title": "Fixture", "items": items})
				}
			}))
			defer server.Close()
			cfg := loadTestConfig(t, databaseURL)
			cfg.Collect.Enabled = true
			cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "fixture", Name: "Fixture", Tier: "official", FeedURL: server.URL, Enabled: true, IntervalMinutes: 60}}
			now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
			app, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, cfg.Fetch), nil))
			if err != nil {
				t.Fatal("assemble application failed")
			}
			t.Cleanup(cleanup)
			err = app.Collector.RunOnce(t.Context())
			if (err == nil) != test.success {
				t.Fatalf("success=%v, error=%v", test.success, err)
			}
			if test.name == "save failure rolls back" && !strings.Contains(err.Error(), "save source fixture") {
				t.Fatalf("expected persistence failure, got %v", err)
			}
			// A source that is not due must not acquire another record.
			if err := app.Collector.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			var count, added, items int
			var success bool
			var message string
			var fetched time.Time
			err = testDB(t, app.Data).QueryRowContext(t.Context(), `SELECT count(*),bool_and(success),min(error),sum(new_items),min(fetched_at) FROM fetch_records WHERE source_id='fixture'`).Scan(&count, &success, &message, &added, &fetched)
			if err != nil {
				t.Fatal(err)
			}
			if count != 1 || success != test.success || added != test.added || !fetched.Equal(now) || (message == "") != test.success {
				t.Fatalf("record count=%d, success=%v, added=%d, fetched=%s, error=%q", count, success, added, fetched, message)
			}
			if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&items); err != nil {
				t.Fatal(err)
			}
			if items != test.added {
				t.Fatalf("persisted items=%d, want %d", items, test.added)
			}
		})
	}
}

func TestStatusCommandReportsSourceHealthWithinRequestedWindow(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = []config.Source{
		{Kind: biz.SourceKindRSS, ID: "health", Name: "Health fixture", Tier: "media", FeedURL: "https://example.test/health-feed", Enabled: false, IntervalMinutes: 60},
		{Kind: biz.SourceKindRSS, ID: "empty", Name: "Empty fixture", Tier: "official", FeedURL: "https://example.test/empty-feed", Enabled: false, IntervalMinutes: 60},
	}
	app := assembleTestApp(t, cfg)
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), `INSERT INTO fetch_records(source_id,fetched_at,success,error,new_items) VALUES
 ('health',now()-interval '12 hours',true,'',1),
 ('health',now()-interval '36 hours',true,'',0),
 ('health',now()-interval '2 days',false,'fixture failure',0),
 ('health',now()-interval '8 days',false,'old failure',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,published_at,discovered_at,processed_at,stage,prefilter,score1,score2,selected)
 SELECT 'health','https://example.test/'||n,'Fixture',now()-interval '30 days',now()-age::interval,now(),stage,prefilter,score1,score2,selected
 FROM (VALUES
 ('1','12 hours','done','PASS',80,91,true),
 ('2','36 hours','done','UNKNOWN',60,80,false),
 ('3','36 hours','blocked','BLOCK',NULL,NULL,false),
 ('4','36 hours','prefiltered','PASS',100,NULL,false),
 ('5','36 hours','new',NULL,NULL,NULL,false),
 ('6','3 days','done','PASS',50,50,false),
 ('7','2 days','failed','PASS',70,90,false),
 ('8','8 days','done','PASS',100,100,true)
 ) AS fixture(n,age,stage,prefilter,score1,score2,selected)`); err != nil {
		t.Fatal(err)
	}
	configPath := writeSubprocessConfig(t, databaseURL, "127.0.0.1:8080", cfg.Web.Dir)
	binaryPath := buildVane(t)
	type health struct {
		Days             int      `json:"days"`
		Fetches          int64    `json:"fetches"`
		FetchSuccessRate *float64 `json:"fetch_success_rate"`
		Prefiltered      int64    `json:"prefiltered"`
		AIRelatedRate    *float64 `json:"ai_related_rate"`
		Done             int64    `json:"done"`
		SelectionRate    *float64 `json:"selection_rate"`
		Scored           int64    `json:"scored"`
		AverageScore     *float64 `json:"average_score"`
	}
	number := func(v float64) *float64 { return &v }
	sevenDays := health{7, 3, number(0.667), 6, number(0.833), 3, number(0.333), 4, number(71.375)}
	for _, test := range []struct {
		name string
		args []string
		want health
	}{
		{"default seven days", nil, sevenDays},
		{"explicit seven days", []string{"--days", "7"}, sevenDays},
		{"one day", []string{"--days", "1"}, health{1, 1, number(1), 1, number(1), 1, number(1), 1, number(85.5)}},
		{"ninety days", []string{"--days", "90"}, health{90, 4, number(0.5), 7, number(0.857), 4, number(0.5), 5, number(77.1)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"status", "--conf", configPath}, test.args...)
			output, err := exec.CommandContext(t.Context(), binaryPath, args...).Output()
			if err != nil {
				t.Fatal("status command failed (output suppressed to protect credentials)")
			}
			var status struct {
				Sources []struct {
					ID        string  `json:"id"`
					Name      string  `json:"name"`
					Tier      string  `json:"tier"`
					Enabled   bool    `json:"enabled"`
					ItemCount int64   `json:"item_count"`
					Health    *health `json:"health"`
				} `json:"sources"`
			}
			if err := json.Unmarshal(output, &status); err != nil {
				t.Fatal(err)
			}
			if len(status.Sources) != 2 {
				t.Fatalf("source count=%d, want 2", len(status.Sources))
			}
			for _, source := range status.Sources {
				if source.Health == nil {
					t.Fatalf("source %s missing health", source.ID)
				}
				want := test.want
				if source.ID == "empty" {
					want = health{Days: test.want.Days}
				} else if source.ID != "health" || source.Name != "Health fixture" || source.Tier != "media" || source.Enabled || source.ItemCount != 8 {
					t.Fatalf("existing fields changed: %+v", source)
				}
				if !reflect.DeepEqual(*source.Health, want) {
					t.Fatalf("source %s health=%+v, want %+v", source.ID, *source.Health, want)
				}
			}
		})
	}
	for _, days := range []string{"0", "91"} {
		if err := exec.CommandContext(t.Context(), binaryPath, "status", "--conf", configPath, "--days", days).Run(); err == nil {
			t.Fatalf("status accepted days=%s", days)
		}
	}
}
