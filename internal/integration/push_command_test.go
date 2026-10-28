package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/bogeia/vane/internal/biz"
)

func TestPushCommandSendsExistingReportAgainAndMarksPushedAt(t *testing.T) {
	var calls atomic.Int32
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/disabled") {
			t.Error("manual push reached daily-disabled group")
		}
		var message struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Error(err)
		}
		if !strings.Contains(message.Markdown.Content, "手动推送正文") || !strings.Contains(message.Markdown.Content, "/reports/2026-10-21") {
			t.Error("manual push did not use stored report")
		}
		calls.Add(1)
		_, _ = fmt.Fprint(w, `{"errcode":0}`)
	}))
	defer webhook.Close()
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	app := assembleTestApp(t, cfg)
	seedCommandReport(t, app, "2026-10-21", true)
	configPath := writePushCommandConfig(t, databaseURL, webhook.URL+"/private-token")
	binary := buildVane(t)
	var previous time.Time
	for attempt := range 2 {
		output, err := exec.CommandContext(t.Context(), binary, "push", "--conf", configPath, "--date", "2026-10-21").CombinedOutput()
		if err != nil {
			t.Fatal("manual push command failed (output suppressed to protect configuration)")
		}
		if len(output) != 0 {
			t.Fatalf("successful push emitted %d bytes", len(output))
		}
		var pushedAt *time.Time
		if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT pushed_at FROM reports WHERE date='2026-10-21'").Scan(&pushedAt); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != int32(attempt+1) || pushedAt == nil || !pushedAt.After(previous) {
			t.Fatalf("push attempt=%d calls=%d timestamp=%v", attempt+1, calls.Load(), pushedAt)
		}
		previous = *pushedAt
	}
}

func seedCommandReport(t *testing.T, app *testApp, date string, body bool) {
	t.Helper()
	report := biz.Report{Date: date, Highlights: []biz.Item{}, Sections: []biz.ReportSection{}, Briefs: []biz.Item{}}
	if body {
		report.Headline = &biz.Item{ID: "1", Title: "手动推送正文"}
	}
	content, err := reportFixtureJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), "INSERT INTO reports(date,content) VALUES($1,$2)", date, string(content)); err != nil {
		t.Fatal(err)
	}
}

func writePushCommandConfig(t *testing.T, databaseURL, webhookURL string) string {
	t.Helper()
	path := writeSubprocessConfig(t, databaseURL, "127.0.0.1:8080", t.TempDir())
	settings := viper.New()
	settings.SetConfigFile(path)
	if err := settings.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	hooks := make([]map[string]any, 0)
	if webhookURL != "" {
		hooks = append(hooks, map[string]any{"name": "manual", "type": "wecom", "url": webhookURL, "secret": "", "daily": true, "alerts": false, "realtime": map[string]any{"enabled": false, "scope": "selected", "topics": []string{}, "words": [][]string{}}})
		hooks = append(hooks, map[string]any{"name": "disabled", "type": "wecom", "url": webhookURL + "/disabled", "secret": "", "daily": false, "alerts": false, "realtime": map[string]any{"enabled": false, "scope": "selected", "topics": []string{}, "words": [][]string{}}})
	}
	settings.Set("push.webhooks", hooks)
	if err := settings.WriteConfig(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPushCommandReportsErrorsWithoutLeakingWebhook(t *testing.T) {
	var calls atomic.Int32
	var businessCode atomic.Int32
	var httpStatus atomic.Int32
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		status := int(httpStatus.Load())
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = fmt.Fprintf(w, `{"errcode":%d,"errmsg":"raw-webhook-secret"}`, businessCode.Load())
	}))
	defer webhook.Close()
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	app := assembleTestApp(t, cfg)
	seedCommandReport(t, app, "2026-10-21", true)
	seedCommandReport(t, app, "2026-10-22", false)
	previous := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), "UPDATE reports SET pushed_at=$1 WHERE date='2026-10-21'", previous); err != nil {
		t.Fatal(err)
	}
	binary := buildVane(t)
	for _, tc := range []struct {
		name, date, want        string
		noHooks, noDaily        bool
		code, status, wantCalls int32
	}{
		{name: "report missing", date: "2026-10-20", want: "report not found"},
		{name: "empty body", date: "2026-10-22", want: "report body is empty"},
		{name: "no webhook", date: "2026-10-21", want: "no webhooks configured", noHooks: true},
		{name: "no daily group", date: "2026-10-21", want: "no webhooks configured", noDaily: true},
		{name: "business failure", date: "2026-10-21", want: "wecom part 1: business code 9", code: 9, wantCalls: 1},
		{name: "HTTP failure", date: "2026-10-21", want: "wecom part 1: HTTP 500", status: 500, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls.Store(0)
			businessCode.Store(tc.code)
			httpStatus.Store(tc.status)
			url := webhook.URL + "/raw-webhook-secret"
			if tc.noHooks {
				url = ""
			}
			configPath := writePushCommandConfig(t, databaseURL, url)
			if tc.noDaily {
				settings := viper.New()
				settings.SetConfigFile(configPath)
				if err := settings.ReadInConfig(); err != nil {
					t.Fatal(err)
				}
				hooks := settings.Get("push.webhooks").([]any)
				hooks[0].(map[string]any)["daily"] = false
				settings.Set("push.webhooks", hooks)
				if err := settings.WriteConfig(); err != nil {
					t.Fatal(err)
				}
			}
			output, err := exec.CommandContext(t.Context(), binary, "push", "--conf", configPath, "--date", tc.date).CombinedOutput()
			if err == nil || !strings.Contains(string(output), tc.want) {
				t.Fatalf("error=%v; expected readable %q error", err, tc.want)
			}
			for _, secret := range []string{webhook.URL, "raw-webhook-secret"} {
				if strings.Contains(string(output), secret) {
					t.Fatal("push error leaked webhook URL or secret")
				}
			}
			if calls.Load() != tc.wantCalls {
				t.Fatalf("webhook calls=%d want %d", calls.Load(), tc.wantCalls)
			}
			var pushedAt *time.Time
			if err := testDB(t, app.Data).QueryRowContext(t.Context(), "SELECT pushed_at FROM reports WHERE date='2026-10-21'").Scan(&pushedAt); err != nil {
				t.Fatal(err)
			}
			if pushedAt == nil || !pushedAt.Equal(previous) {
				t.Fatal("failed push changed existing pushed_at")
			}
		})
	}
}

func TestPushCommandValidatesDateBeforeOpeningDatabase(t *testing.T) {
	configPath := writePushCommandConfig(t, "postgres://127.0.0.1:1/unavailable?sslmode=disable", "")
	binary := buildVane(t)
	for _, date := range []string{"not-a-date", "2026-02-30", "2026-1-01"} {
		output, err := exec.CommandContext(t.Context(), binary, "push", "--conf", configPath, "--date", date).CombinedOutput()
		if err == nil || !strings.Contains(string(output), "invalid report date") {
			t.Fatalf("date %q did not fail before database connection", date)
		}
	}
	for _, args := range [][]string{
		{"push", "--date", "2026-10-21"},
		{"push", "--conf", configPath},
		{"push", "--conf", configPath, "--date", "2026-10-21", "extra"},
	} {
		if err := exec.CommandContext(t.Context(), binary, args...).Run(); err == nil {
			t.Fatalf("accepted invalid push args %v", args)
		}
	}
}
