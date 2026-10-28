package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
)

func TestSourceAlertsThresholdRecoveryDisabledGroupsAndRetry(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	var mu sync.Mutex
	var bodies []string
	fail := false
	const webhookSecret = "alert-webhook-secret-sentinel"
	const remoteSensitiveError = "remote-sensitive-error-sentinel"
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if fail {
			w.WriteHeader(503)
			_, _ = io.WriteString(w, remoteSensitiveError)
			return
		}
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer webhook.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = []config.Source{{Kind: biz.SourceKindRSS, ID: "broken", Name: "失败信源", Tier: "official", FeedURL: "https://example.test/rss", Enabled: true, IntervalMinutes: 60}, {Kind: biz.SourceKindRSS, ID: "disabled", Name: "停用信源", Tier: "official", FeedURL: "https://example.test/disabled", Enabled: false, IntervalMinutes: 60}}
	cfg.Push.Webhooks = []config.Webhook{{Name: "ops", Type: config.WebhookTypeWecom, URL: webhook.URL + "/robot?key=" + webhookSecret, Secret: webhookSecret, Alerts: true}, {Name: "readers", Type: config.WebhookTypeWecom, URL: webhook.URL, Alerts: false}}
	app := assembleTestApp(t, cfg)
	uc := biz.NewSourceAlertsUseCase(data.NewSourceAlertsRepo(app.Data), data.NewPushClient(cfg), biz.NewSourceAlertOptions(cfg))
	db := testDB(t, app.Data)
	now := time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)
	insert := func(id string, success bool) {
		t.Helper()
		now = now.Add(time.Minute)
		message := "request failed"
		if success {
			message = ""
		}
		if _, err := db.ExecContext(t.Context(), `INSERT INTO fetch_records(source_id,fetched_at,success,error,new_items) VALUES($1,$2,$3,$4,0)`, id, now, success, message); err != nil {
			t.Fatal(err)
		}
	}
	run := func() {
		t.Helper()
		if err := uc.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(bodies) }
	for range 3 {
		insert("disabled", false)
	}
	insert("broken", false)
	insert("broken", false)
	run()
	if count() != 0 {
		t.Fatal("early or disabled alert")
	}
	insert("broken", true)
	insert("broken", false)
	insert("broken", false)
	run()
	if count() != 0 {
		t.Fatal("success did not reset failures")
	}
	insert("broken", false)
	mu.Lock()
	fail = true
	mu.Unlock()
	sendError := uc.RunOnce(t.Context())
	if sendError == nil {
		t.Fatal("send failure ignored")
	}
	assertSanitized := func(content string) {
		t.Helper()
		for _, sensitive := range []string{webhook.URL, cfg.Push.Webhooks[0].URL, webhookSecret, remoteSensitiveError} {
			if strings.Contains(content, sensitive) {
				t.Fatal("alert exposed sensitive webhook data")
			}
		}
	}
	assertSanitized(sendError.Error())
	mu.Lock()
	failedBody := bodies[len(bodies)-1]
	mu.Unlock()
	assertSanitized(failedBody)
	var open bool
	if err := db.QueryRowContext(t.Context(), `SELECT alert_started_at IS NOT NULL FROM sources WHERE id='broken'`).Scan(&open); err != nil || open {
		t.Fatal("failed alert saved")
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	run()
	if count() != 2 {
		t.Fatalf("requests=%d, want failed attempt and retry only to ops", count())
	}
	mu.Lock()
	body := bodies[len(bodies)-1]
	mu.Unlock()
	assertSanitized(body)
	for _, want := range []string{"失败信源", "broken", "连续失败：3", "首次失败：2026-10-23 16:07", "request failed"} {
		if !strings.Contains(body, want) {
			t.Fatalf("message missing %s", want)
		}
	}
	insert("broken", false)
	run()
	if count() != 2 {
		t.Fatal("open alert repeated")
	}
	insert("broken", true)
	run()
	if count() != 3 {
		t.Fatal("recovery not sent")
	}
	mu.Lock()
	body = bodies[len(bodies)-1]
	mu.Unlock()
	if !strings.Contains(body, "信源恢复") || !strings.Contains(body, "中断时长：4 分钟") {
		t.Fatal(body)
	}
	for range 3 {
		insert("broken", false)
	}
	run()
	if count() != 4 {
		t.Fatal("new incident not alerted")
	}
}
