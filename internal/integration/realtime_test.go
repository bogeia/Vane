package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/biz/taxonomy"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
)

func realtimeApp(t *testing.T, hooks []config.Webhook) (*testApp, *time.Time) {
	t.Helper()
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Collect.Enabled = false
	cfg.LLM.Enabled = false
	cfg.Push.Webhooks = hooks
	now := time.Now().Add(time.Minute).Truncate(time.Second)
	app, cleanup, err := newTestApp(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now })
	if err != nil {
		t.Fatal("assemble realtime app:", err)
	}
	t.Cleanup(cleanup)
	_, err = testDB(t, app.Data).ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('realtime','测试信源','media','https://example.test/feed',false,60)`)
	if err != nil {
		t.Fatal(err)
	}
	return app, &now
}

func seedRealtime(t *testing.T, app *testApp, now time.Time, title string, selected bool, storyID int64, processed, published time.Time) int64 {
	t.Helper()
	db := testDB(t, app.Data)
	if storyID == 0 {
		if err := db.QueryRowContext(t.Context(), `INSERT INTO stories(title,created_at) VALUES($1,$2) RETURNING id`, title, now).Scan(&storyID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,published_at,processed_at,stage,title,summary,reason,category,selected,story_id) VALUES('realtime',$1,$2,$3,$4,'done',$2,'摘要 [坏链接](https://bad.test) *粗体*','推荐理由 <at id=all>','模型',$5,$6)`, fmt.Sprintf("https://example.test/%s/%d", title, processed.UnixNano()), title, published, processed, selected, storyID); err != nil {
		t.Fatal(err)
	}
	return storyID
}

func realtimeHook(name, platform, url string, enabled bool) config.Webhook {
	return config.Webhook{Name: name, Type: platform, URL: url, Realtime: config.Realtime{Enabled: enabled, Scope: "selected"}}
}

func TestRealtimeSelectedEligibilityAndTargetIdentity(t *testing.T) {
	var received []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		received = append(received, payload.Markdown.Content)
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer hook.Close()
	app, now := realtimeApp(t, []config.Webhook{realtimeHook("readers", "wecom", hook.URL, true), realtimeHook("off", "wecom", hook.URL, false)})
	repo := data.NewRealtimeRepo(app.Data)
	first, err := repo.Target(t.Context(), "readers")
	if err != nil {
		t.Fatal(err)
	}
	seedRealtime(t, app, *now, "旧资料", true, 0, first.FirstSeenAt.Add(-time.Second), now.Add(-time.Hour))
	seedRealtime(t, app, *now, "过期", true, 0, *now, now.Add(-13*time.Hour))
	seedRealtime(t, app, *now, "未精选", false, 0, *now, now.Add(-time.Hour))
	story := seedRealtime(t, app, *now, "精选 *标题*", true, 0, *now, now.Add(-time.Hour))
	if err := app.Realtime.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	seedRealtime(t, app, *now, "同事件第二条", true, story, now.Add(time.Second), now.Add(-time.Hour))
	if err := app.Realtime.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(received) != 1 {
		t.Fatalf("requests=%d, want one selected event", len(received))
	}
	for _, want := range []string{`精选 \*标题\*`, `摘要`, `推荐理由`, `/items/`, `https://example.test/`, `精选`, `\[坏链接\]`, `&lt;at id=all&gt;`} {
		if !strings.Contains(received[0], want) {
			t.Errorf("payload missing %q: %s", want, received[0])
		}
	}
	// Re-syncing the same name models a URL edit; renaming creates a fresh baseline.
	if err := repo.SyncTargets(t.Context(), []string{"readers", "renamed"}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	same, _ := repo.Target(t.Context(), "readers")
	renamed, _ := repo.Target(t.Context(), "renamed")
	if !same.FirstSeenAt.Equal(first.FirstSeenAt) || !renamed.FirstSeenAt.Equal(now.Add(time.Hour)) {
		t.Fatal("target identity baseline changed incorrectly")
	}
	options := biz.RealtimeOptions{Groups: []biz.RealtimeGroup{{Name: "renamed", Enabled: true, Scope: "selected"}}}
	cfg := config.Config{Push: config.Push{RequestTimeout: time.Second, Webhooks: []config.Webhook{realtimeHook("renamed", "wecom", hook.URL, true)}}}
	renamedUseCase := biz.NewRealtimeUseCase(repo, data.NewPushClient(cfg), options, func() time.Time { return now.Add(time.Hour) }, slog.Default())
	if err := renamedUseCase.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(received) != 1 {
		t.Fatal("renamed target received backlog")
	}
	// Durable event identity survives reconstructing the use case.
	fresh := biz.NewRealtimeUseCase(repo, data.NewPushClient(config.Config{Push: config.Push{RequestTimeout: time.Second, Webhooks: []config.Webhook{realtimeHook("readers", "wecom", hook.URL, true)}}}), biz.RealtimeOptions{Groups: []biz.RealtimeGroup{{Name: "readers", Enabled: true, Scope: "selected"}}}, func() time.Time { return *now }, slog.Default())
	if err := fresh.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(received) != 1 {
		t.Fatal("restart repeated sent event")
	}
}

func TestRealtimeSevenItemsUseFiveCardsAndOneList(t *testing.T) {
	var received []map[string]any
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		received = append(received, payload)
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer hook.Close()
	app, now := realtimeApp(t, []config.Webhook{realtimeHook("cards", "feishu", hook.URL, true)})
	for i := range 7 {
		seedRealtime(t, app, *now, fmt.Sprintf("标题%d", i), true, 0, now.Add(time.Duration(i)*time.Second), now.Add(-time.Hour))
	}
	if err := app.Realtime.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(received) != 6 {
		t.Fatalf("requests=%d want 6", len(received))
	}
	for index, payload := range received {
		if payload["msg_type"] != "interactive" {
			t.Fatal("Feishu did not receive card")
		}
		encoded, _ := json.Marshal(payload)
		if index < 5 {
			for _, want := range []string{fmt.Sprintf("标题%d", index), "摘要", "推荐理由", "本站资料", "原文", "精选"} {
				if !strings.Contains(string(encoded), want) {
					t.Errorf("card missing %q: %s", want, encoded)
				}
			}
		} else {
			if !strings.Contains(string(encoded), "标题5") || !strings.Contains(string(encoded), "标题6") || strings.Contains(string(encoded), "摘要") {
				t.Errorf("list=%s", encoded)
			}
		}
	}
	var count int
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), `SELECT count(*) FROM push_deliveries WHERE status='sent' AND attempts=1`).Scan(&count); err != nil || count != 7 {
		t.Fatalf("sent=%d err=%v", count, err)
	}
}

func TestRealtimeOnlyFailedGroupRetriesThreeTimesThenAbandons(t *testing.T) {
	successes, failures := 0, 0
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { successes++; _, _ = io.WriteString(w, `{"errcode":0}`) }))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { failures++; _, _ = io.WriteString(w, `{"code":1}`) }))
	defer bad.Close()
	app, now := realtimeApp(t, []config.Webhook{realtimeHook("good", "wecom", good.URL, true), realtimeHook("bad", "feishu", bad.URL, true)})
	seedRealtime(t, app, *now, "实时资料", true, 0, *now, now.Add(-time.Hour))
	for range 6 {
		err := app.Realtime.RunOnce(t.Context())
		if failures < 4 && err == nil {
			t.Fatal("failure hidden")
		}
		*now = now.Add(time.Minute)
	}
	if successes != 1 || failures != 4 {
		t.Fatalf("good=%d bad=%d want 1/4", successes, failures)
	}
	var status, lastError string
	var attempts int
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), `SELECT status,attempts,last_error FROM push_deliveries WHERE target_name='bad'`).Scan(&status, &attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != biz.DeliveryAbandoned || attempts != 4 || lastError != "push request failed" {
		t.Fatalf("delivery=%s/%d/%s", status, attempts, lastError)
	}
	for range 2 {
		if err := data.Migrate(t.Context(), app.Data); err != nil {
			t.Fatal("repeat migration:", err)
		}
	}
	for _, statement := range []string{`UPDATE push_deliveries SET status='invalid' WHERE target_name='bad'`, `UPDATE push_deliveries SET attempts=5 WHERE target_name='bad'`, `UPDATE push_deliveries SET target_name='missing' WHERE target_name='bad'`, `UPDATE push_deliveries SET item_id=99999999 WHERE target_name='bad'`, `UPDATE push_deliveries SET story_id=99999999 WHERE target_name='bad'`} {
		if _, err := testDB(t, app.Data).ExecContext(t.Context(), statement); err == nil {
			t.Fatalf("constraint accepted %s", statement)
		}
	}
	// Composite uniqueness is enforced by PostgreSQL, including after a failed delivery.
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), `INSERT INTO push_deliveries SELECT * FROM push_deliveries WHERE target_name='bad'`); err == nil {
		t.Fatal("duplicate delivery accepted")
	}
}

func TestRealtimeCancellationDoesNotConsumeAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	requests := 0
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			cancel()
		}
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer hook.Close()
	app, now := realtimeApp(t, []config.Webhook{realtimeHook("cancel", "wecom", hook.URL, true)})
	seedRealtime(t, app, *now, "取消恢复", true, 0, *now, now.Add(-time.Hour))
	if err := app.Realtime.RunOnce(ctx); err == nil {
		t.Fatal("cancellation ignored")
	}
	var count int
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), `SELECT count(*) FROM push_deliveries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cancel consumed attempt: %d / %v", count, err)
	}
	if err := app.Realtime.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), `SELECT attempts FROM push_deliveries WHERE target_name='cancel'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("restored attempt: %d / %v", count, err)
	}
}

func TestRealtimeListPartialFailureDoesNotRepeatSuccessfulParts(t *testing.T) {
	requests := 0
	received := map[string]int{}
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var payload struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if requests == 7 {
			_, _ = io.WriteString(w, `{"errcode":1}`)
			return
		}
		for _, line := range strings.Split(payload.Markdown.Content, "\n") {
			if strings.HasPrefix(line, "- [") {
				received[line]++
			}
		}
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer hook.Close()
	app, now := realtimeApp(t, []config.Webhook{realtimeHook("split", "wecom", hook.URL, true)})
	for index := range 25 {
		seedRealtime(t, app, *now, fmt.Sprintf("%02d%s", index, strings.Repeat("长标题", 80)), true, 0, now.Add(time.Duration(index)*time.Second), now.Add(-time.Hour))
	}
	if err := app.Realtime.RunOnce(t.Context()); err == nil {
		t.Fatal("partial failure ignored")
	}
	*now = now.Add(time.Minute)
	if err := app.Realtime.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	for title, count := range received {
		if count != 1 {
			t.Fatalf("successful list line repeated %d times: %s", count, title)
		}
	}
	var sent int
	if err := testDB(t, app.Data).QueryRowContext(t.Context(), `SELECT count(*) FROM push_deliveries WHERE status='sent'`).Scan(&sent); err != nil || sent != 25 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
}

func TestRealtimeTopicAndWordFilters(t *testing.T) {
	var received []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = append(received, string(body))
		_, _ = io.WriteString(w, `{"errcode":0}`)
	}))
	defer hook.Close()
	for _, tt := range []struct {
		name, scope string
		topics      []string
		words       [][]string
		selected    bool
		want        int
		reason      string
	}{
		{"all words includes unselected", "all", nil, [][]string{{"Claude"}}, false, 2, "关注词：Claude"},
		{"selected topic", "selected", []string{"anthropic"}, nil, true, 2, "主题：Anthropic / Claude"},
		{"intersection", "selected", []string{"anthropic"}, [][]string{{"Claude"}}, true, 1, "主题：Anthropic / Claude；关注词：Claude"},
		{"all topics", "all", []string{"anthropic"}, nil, false, 2, "主题：Anthropic / Claude"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			received = nil
			group := realtimeHook("filters", "wecom", hook.URL, true)
			group.Realtime.Scope = tt.scope
			group.Realtime.Topics = tt.topics
			group.Realtime.Words = tt.words
			app, now := realtimeApp(t, []config.Webhook{group})
			for _, entry := range []struct{ title, topic string }{{"Claude匹配", "anthropic"}, {"Claude其他主题", "openai"}, {"其他标题", "anthropic"}, {"完全不匹配", "openai"}} {
				story := seedRealtime(t, app, *now, entry.title, tt.selected, 0, *now, now.Add(-time.Hour))
				if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE items SET topics=ARRAY[$1]::text[] WHERE story_id=$2`, entry.topic, story); err != nil {
					t.Fatal(err)
				}
			}
			if err := app.Realtime.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(received) != tt.want {
				t.Fatalf("requests=%d want %d: %v", len(received), tt.want, received)
			}
			for _, body := range received {
				var payload struct {
					Markdown struct {
						Content string `json:"content"`
					} `json:"markdown"`
				}
				if err := json.Unmarshal([]byte(body), &payload); err != nil {
					t.Fatal(err)
				}
				if !strings.HasSuffix(payload.Markdown.Content, tt.reason) {
					t.Fatalf("wrong reason: %s", payload.Markdown.Content)
				}
			}
			if err := app.Realtime.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(received) != tt.want {
				t.Fatal("filtered event repeated")
			}
		})
	}
}

func TestRealtimeRetriesRetireExpiredOrFilteredItems(t *testing.T) {
	for _, change := range []string{"expired", "filters", "roundup", "roundup all"} {
		t.Run(change, func(t *testing.T) {
			requests := 0
			hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; _, _ = io.WriteString(w, `{"errcode":1}`) }))
			defer hook.Close()
			group := realtimeHook("retry", "wecom", hook.URL, true)
			if change == "roundup all" {
				group.Realtime.Scope = taxonomy.ScopeAll
				group.Realtime.Words = [][]string{{"Claude"}}
			}
			app, now := realtimeApp(t, []config.Webhook{group})
			published := now.Add(-time.Hour)
			if change == "expired" {
				published = now.Add(-12*time.Hour + 30*time.Second)
			}
			story := seedRealtime(t, app, *now, "Claude发布", true, 0, *now, published)
			if err := app.Realtime.RunOnce(t.Context()); err == nil {
				t.Fatal("failure hidden")
			}
			if change == "roundup" || change == "roundup all" {
				if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE items SET roundup=true WHERE story_id=$1`, story); err != nil {
					t.Fatal(err)
				}
			}
			*now = now.Add(time.Minute)
			options := biz.RealtimeOptions{Groups: []biz.RealtimeGroup{{Name: "retry", Enabled: true, Scope: group.Realtime.Scope, Words: group.Realtime.Words}}}
			if change == "filters" {
				options.Groups[0].Words = [][]string{{"GPT"}}
			}

			repo := data.NewRealtimeRepo(app.Data)
			cfg := config.Config{Push: config.Push{RequestTimeout: time.Second, Webhooks: []config.Webhook{group}}}
			retry := biz.NewRealtimeUseCase(repo, data.NewPushClient(cfg), options, func() time.Time { return *now }, slog.Default())
			for range 2 {
				if err := retry.RunOnce(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if requests != 1 {
				t.Fatalf("ineligible retry sent: requests=%d", requests)
			}
			var status, lastError string
			var attempts int
			if err := testDB(t, app.Data).QueryRowContext(t.Context(), `SELECT status,attempts,last_error FROM push_deliveries WHERE target_name='retry'`).Scan(&status, &attempts, &lastError); err != nil {
				t.Fatal(err)
			}
			if status != biz.DeliveryAbandoned || attempts != 1 || lastError != "push no longer eligible" {
				t.Fatalf("retired delivery=%s/%d/%s", status, attempts, lastError)
			}
		})
	}
}

func TestRealtimeRoundupsNeverCreateDeliveries(t *testing.T) {
	for _, scope := range []string{"all", "selected"} {
		t.Run(scope, func(t *testing.T) {
			requests := 0
			hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; _, _ = io.WriteString(w, `{"errcode":0}`) }))
			defer hook.Close()
			group := realtimeHook("readers", "wecom", hook.URL, true)
			group.Realtime.Scope = scope
			group.Realtime.Words = [][]string{{"Claude"}}
			app, now := realtimeApp(t, []config.Webhook{group})
			story := seedRealtime(t, app, *now, "Claude综合早报", false, 0, *now, now.Add(-time.Hour))
			if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE items SET roundup=true WHERE story_id=$1`, story); err != nil {
				t.Fatal(err)
			}
			seedRealtime(t, app, *now, "Claude发布", true, 0, *now, now.Add(-time.Hour))
			if err := app.Realtime.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("requests=%d want one ordinary selected item", requests)
			}
			deliveries, err := data.NewRealtimeRepo(app.Data).Deliveries(t.Context(), "readers")
			if err != nil {
				t.Fatal(err)
			}
			if len(deliveries) != 1 || deliveries[0].Status != biz.DeliverySent {
				t.Fatalf("deliveries=%+v", deliveries)
			}
			if deliveries[0].StoryID == fmt.Sprint(story) {
				t.Fatal("roundup created delivery")
			}
			// Successful deliveries stay terminal even if the item is later identified as a roundup.
			if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE items SET roundup=true`); err != nil {
				t.Fatal(err)
			}
			if err := app.Realtime.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			after, err := data.NewRealtimeRepo(app.Data).Deliveries(t.Context(), "readers")
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 || len(after) != 1 || after[0].Status != biz.DeliverySent || after[0].Attempts != 1 || !after[0].UpdatedAt.Equal(deliveries[0].UpdatedAt) {
				t.Fatalf("sent delivery changed: %+v", after)
			}
		})
	}
}
