package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/spf13/viper"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
)

func TestReportSchedulerPushesWecomOnceWithBodyLinks(t *testing.T) {
	var mu sync.Mutex
	var contents []string
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/disabled" {
			t.Error("daily-disabled group received report")
		}
		var message struct {
			Msgtype  string `json:"msgtype"`
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil || message.Msgtype != "markdown" || r.Method != http.MethodPost {
			t.Error("unexpected Wecom request")
			http.Error(w, "bad request", 400)
			return
		}
		mu.Lock()
		contents = append(contents, message.Markdown.Content)
		mu.Unlock()
		_, _ = fmt.Fprint(w, `{"errcode":0}`)
	}))
	defer webhook.Close()
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Site.PublicURL = "https://vane.test"
	cfg.Push.Webhooks = []config.Webhook{{Name: "group-1", Daily: true, Type: "wecom", URL: webhook.URL + "/raw-webhook-secret", Secret: ""}, {Name: "operators", Daily: false, Type: "wecom", URL: webhook.URL + "/disabled"}}
	app := assembleTestApp(t, cfg)
	// assembleTestApp uses noon UTC; every fixture is inside that day's report window.
	processed := time.Date(2026, time.October, 21, 23, 0, 0, 0, time.UTC)
	ids := seedPushCandidates(t, app, processed, 7)
	for range 3 {
		if err := app.Reports.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(contents) != 1 {
		t.Fatalf("push requests=%d, want one", len(contents))
	}
	content := contents[0]
	if !strings.Contains(content, "Vane 日报 · 2026-10-22") || !strings.Contains(content, "https://vane.test/reports/2026-10-22") {
		t.Fatalf("missing report heading/link: %s", content)
	}
	previous := -1
	for _, i := range []int{0, 1, 2, 3, 5, 6} {
		title := fmt.Sprintf("推送标题%d", i)
		index := strings.Index(content, title)
		if index <= previous || strings.Count(content, title) != 1 || !strings.Contains(content, "https://vane.test/items/"+ids[i]) {
			t.Fatalf("body item %d missing, duplicated or out of order: %s", i, content)
		}
		previous = index
	}
	if strings.Contains(content, "推送标题4") {
		t.Fatalf("brief leaked into push: %s", content)
	}
	var pushedAt *time.Time
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT pushed_at FROM reports WHERE date='2026-10-22'").Scan(&pushedAt); err != nil {
		t.Fatal(err)
	}
	if pushedAt == nil {
		t.Fatal("successful push did not set pushed_at")
	}
}

func seedPushCandidates(t *testing.T, app *testApp, processed time.Time, count int) []string {
	t.Helper()
	ids := make([]string, 0, count)
	for i := range count {
		source := fmt.Sprintf("push-%d", i)
		if _, err := testDB(t, app.Data).ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES($1,$1,'official','https://example.test/feed',false,60)`, source); err != nil {
			t.Fatal(err)
		}
		var storyID int64
		if err := testDB(t, app.Data).QueryRowContext(t.Context(), `INSERT INTO stories(title,created_at) VALUES($1,$2) RETURNING id`, fmt.Sprintf("事件%d", i), processed).Scan(&storyID); err != nil {
			t.Fatal(err)
		}
		category := "模型"
		if i >= 5 {
			category = "产品"
		}
		var id string
		if err := testDB(t, app.Data).QueryRowContext(t.Context(), `INSERT INTO items(source_id,url,original_title,published_at,discovered_at,processed_at,stage,score1,score2,title,category,selected,story_id) VALUES($1,$2,$3,$4,$4,$4,'done',$5,$5,$3,$6,true,$7) RETURNING id::text`, source, "https://example.test/"+source, fmt.Sprintf("推送标题%d", i), processed, 100-i, category, storyID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE stories SET representative_item_id=$1 WHERE id=$2`, id, storyID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestReportPushSplitsWecomAtUTF8ByteLimitWithoutSplittingItems(t *testing.T) {
	var mu sync.Mutex
	var contents []string
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		mu.Lock()
		contents = append(contents, payload.Markdown.Content)
		mu.Unlock()
		_, _ = fmt.Fprint(w, `{"errcode":0}`)
	}))
	defer webhook.Close()
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Site.PublicURL = "https://vane.test/" + strings.Repeat("route", 60)
	cfg.Push.Webhooks = []config.Webhook{{Name: "group-2", Daily: true, Type: "wecom", URL: webhook.URL}}
	app := assembleTestApp(t, cfg)
	ids := seedPushCandidates(t, app, time.Date(2026, time.October, 21, 23, 0, 0, 0, time.UTC), 12)
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE items SET title='标题-'||lpad(split_part(source_id,'-',2),2,'0')||'-'||repeat('中',74), category=(ARRAY['模型','产品','行业'])[split_part(source_id,'-',2)::int%3+1]`); err != nil {
		t.Fatal(err)
	}
	if err := app.Reports.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	messages := slices.Clone(contents)
	mu.Unlock()
	if len(messages) < 2 {
		t.Fatalf("messages=%d, want multiple", len(messages))
	}
	for i, content := range messages {
		if len(content) > 4096 || !utf8.ValidString(content) {
			t.Fatalf("message %d has invalid UTF-8 or %d bytes", i, len(content))
		}
		if !strings.Contains(content, fmt.Sprintf("（%d/%d）", i+1, len(messages))) {
			t.Fatalf("message %d missing part number", i)
		}
		if !strings.Contains(content, cfg.Site.PublicURL+"/reports/2026-10-22") {
			t.Fatalf("message %d missing full-report link", i)
		}
	}
	for i, id := range ids {
		line := fmt.Sprintf("- [标题-%02d-%s](%s/items/%s)", i, strings.Repeat("中", 74), cfg.Site.PublicURL, id)
		count := 0
		for _, content := range messages {
			count += strings.Count(content, line)
		}
		if count != 1 {
			t.Fatalf("item %d whole line occurs %d times, want once", i, count)
		}
	}
	report, err := app.Reports.Get(t.Context(), "2026-10-22")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Site.PublicURL += strings.Repeat("x", 5000)
	if err := data.NewPusher(cfg, data.NewPushClient(cfg)).Push(t.Context(), report); err == nil {
		t.Fatal("oversized individual item was truncated or accepted")
	}
	mu.Lock()
	requests := len(contents)
	mu.Unlock()
	if requests != len(messages) {
		t.Fatal("oversized individual item reached webhook")
	}
}

func TestReportSchedulerRetriesFailedWecomAndManualReportKeepsPushState(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		timeout    bool
	}{
		{name: "HTTP failure", status: 503, body: `{"errcode":0}`},
		{name: "business failure", status: 200, body: `{"errcode":9001,"errmsg":"remote-error-secret"}`},
		{name: "missing business code", status: 200, body: `{}`},
		{name: "invalid response", status: 200, body: `broken JSON`},
		{name: "request timeout", timeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls, secondary atomic.Int32
			webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/secondary-hook" {
					secondary.Add(1)
					_, _ = fmt.Fprint(w, `{"errcode":0}`)
					return
				}
				if calls.Add(1) == 1 {
					if test.timeout {
						_, _ = io.Copy(io.Discard, r.Body)
						<-r.Context().Done()
						return
					}
					w.WriteHeader(test.status)
					_, _ = fmt.Fprint(w, test.body)
					return
				}
				_, _ = fmt.Fprint(w, `{"errcode":0}`)
			}))
			defer webhook.Close()
			databaseURL := testDatabaseURL(t)
			resetTestSchema(t, databaseURL)
			cfg := loadTestConfig(t, databaseURL)
			cfg.Sources = nil
			cfg.Push.RequestTimeout = 100 * time.Millisecond
			cfg.Push.Webhooks = []config.Webhook{{Name: "group-3", Daily: true, Type: "wecom", URL: webhook.URL + "/raw-webhook-secret"}, {Name: "group-4", Daily: true, Type: "wecom", URL: webhook.URL + "/secondary-hook"}}
			app := assembleTestApp(t, cfg)
			seedPushCandidates(t, app, time.Date(2026, time.October, 21, 23, 0, 0, 0, time.UTC), 1)
			err := app.Reports.RunOnce(t.Context())
			if err == nil {
				t.Fatal("failed push was accepted")
			}
			for _, value := range []string{webhook.URL, "raw-webhook-secret", "remote-error-secret"} {
				if strings.Contains(err.Error(), value) {
					t.Fatalf("push error leaked webhook details: %v", err)
				}
			}
			var pushedAt *time.Time
			if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT pushed_at FROM reports WHERE date='2026-10-22'").Scan(&pushedAt); err != nil {
				t.Fatal(err)
			}
			if pushedAt != nil || calls.Load() != 1 || secondary.Load() != 1 {
				t.Fatalf("failed attempt pushed_at=%v requests=%d, want nil and one request", pushedAt, calls.Load())
			}
			if err := app.Reports.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT pushed_at FROM reports WHERE date='2026-10-22'").Scan(&pushedAt); err != nil {
				t.Fatal(err)
			}
			if pushedAt == nil {
				t.Fatal("successful retry did not mark pushed")
			}
			if test.name == "business failure" {
				path := writeSubprocessConfig(t, databaseURL, "127.0.0.1:8080", cfg.Web.Dir)
				settings := viper.New()
				settings.SetConfigFile(path)
				if err := settings.ReadInConfig(); err != nil {
					t.Fatal("read temporary configuration")
				}
				// The subprocess loads the strict schema, unlike the in-process fixture.
				for i := range cfg.Push.Webhooks {
					cfg.Push.Webhooks[i].Realtime = config.Realtime{Scope: "selected", Topics: []string{}, Words: [][]string{}}
				}
				settings.Set("push.webhooks", cfg.Push.Webhooks)
				if err := settings.WriteConfig(); err != nil {
					t.Fatal("write temporary configuration")
				}
				if _, err := config.Load(path); err != nil {
					t.Fatalf("invalid report subprocess configuration: %v", err)
				}
				if _, err := exec.CommandContext(t.Context(), buildVane(t), "report", "--conf", path, "--date", "2026-10-22").Output(); err != nil {
					t.Fatal("report command failed (output suppressed)")
				}
			} else if _, err := app.Reports.Generate(t.Context(), "2026-10-22"); err != nil {
				t.Fatal(err)
			}
			var after *time.Time
			if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT pushed_at FROM reports WHERE date='2026-10-22'").Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after == nil || !after.Equal(*pushedAt) {
				t.Fatalf("manual regeneration changed pushed_at from %v to %v", pushedAt, after)
			}
			if err := app.Reports.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 || secondary.Load() != 2 {
				t.Fatalf("requests=%d, want failed attempt plus retry only", calls.Load())
			}
		})
	}
}

func TestReportSchedulerSkipsEmptyReportsAndUnconfiguredPush(t *testing.T) {
	for _, test := range []struct {
		name           string
		empty, noHooks bool
		platform       string
	}{
		{name: "empty report", empty: true, platform: "wecom"},
		{name: "no webhooks", noHooks: true, platform: "wecom"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = fmt.Fprint(w, `{"errcode":0}`) }))
			defer webhook.Close()
			databaseURL := testDatabaseURL(t)
			resetTestSchema(t, databaseURL)
			cfg := loadTestConfig(t, databaseURL)
			cfg.Sources = nil
			if !test.noHooks {
				cfg.Push.Webhooks = []config.Webhook{{Name: "group-5", Daily: true, Type: test.platform, URL: webhook.URL}}
			}
			app := assembleTestApp(t, cfg)
			if !test.empty {
				seedPushCandidates(t, app, time.Date(2026, time.October, 21, 23, 0, 0, 0, time.UTC), 1)
			}
			err := app.Reports.RunOnce(t.Context())
			if err != nil {
				t.Fatalf("unexpected result: %v", err)
			}
			if calls.Load() != 0 {
				t.Fatal("unexpected webhook request")
			}
			var pushedAt *time.Time
			if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT pushed_at FROM reports WHERE date='2026-10-22'").Scan(&pushedAt); err != nil {
				t.Fatal(err)
			}
			if pushedAt != nil {
				t.Fatal("report was marked pushed without sending")
			}
		})
	}
}

func TestReportPushSubprocessWebhookConfiguration(t *testing.T) {
	path := writeSubprocessConfig(t, "postgres://localhost:5432/unavailable?sslmode=disable", "127.0.0.1:8080", t.TempDir())
	settings := viper.New()
	settings.SetConfigFile(path)
	if err := settings.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	hooks := []config.Webhook{{Name: "readers", Daily: true, Type: config.WebhookTypeWecom, URL: "https://example.test/webhook", Realtime: config.Realtime{Scope: "selected", Topics: []string{}, Words: [][]string{}}}}
	settings.Set("push.webhooks", hooks)
	if err := settings.WriteConfig(); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("subprocess webhook configuration rejected: %v", err)
	}
}
