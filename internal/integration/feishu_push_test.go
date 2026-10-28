package integration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/config"
)

type feishuCardRequest struct {
	MsgType   string `json:"msg_type"`
	Timestamp string `json:"timestamp"`
	Sign      string `json:"sign"`
	Card      struct {
		Schema string `json:"schema"`
		Header struct {
			Title struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
			} `json:"title"`
		} `json:"header"`
		Body struct {
			Elements []struct {
				Tag     string `json:"tag"`
				Content string `json:"content"`
			} `json:"elements"`
		} `json:"body"`
	} `json:"card"`
}

func TestReportSchedulerPushesFeishuCardsWithOptionalSignature(t *testing.T) {
	for _, test := range []struct{ name, secret string }{{name: "unsigned"}, {name: "signed", secret: "feishu-signing-secret"}} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			var cards []feishuCardRequest
			webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					http.Error(w, "bad request", 400)
					return
				}
				var card feishuCardRequest
				var fields map[string]json.RawMessage
				if json.Unmarshal(body, &card) != nil || json.Unmarshal(body, &fields) != nil || r.Method != http.MethodPost {
					t.Error("invalid card request")
					http.Error(w, "bad request", 400)
					return
				}
				if test.secret == "" {
					if _, ok := fields["timestamp"]; ok {
						t.Error("unsigned request contains timestamp")
					}
					if _, ok := fields["sign"]; ok {
						t.Error("unsigned request contains sign")
					}
				} else {
					seconds, err := strconv.ParseInt(card.Timestamp, 10, 64)
					if err != nil || time.Since(time.Unix(seconds, 0)) > time.Hour || time.Until(time.Unix(seconds, 0)) > time.Minute {
						t.Error("signature timestamp is not current")
					}
					digest := hmac.New(sha256.New, []byte(card.Timestamp+"\n"+test.secret))
					want := base64.StdEncoding.EncodeToString(digest.Sum(nil))
					if card.Sign != want {
						t.Error("signature does not match official empty-message HMAC")
					}
				}
				mu.Lock()
				cards = append(cards, card)
				mu.Unlock()
				_, _ = fmt.Fprint(w, `{"code":0}`)
			}))
			defer webhook.Close()
			databaseURL := testDatabaseURL(t)
			resetTestSchema(t, databaseURL)
			cfg := loadTestConfig(t, databaseURL)
			cfg.Sources = nil
			cfg.Site.PublicURL = "https://vane.test/" + strings.Repeat("r", 800)
			cfg.Push.Webhooks = []config.Webhook{{Name: "group-1", Daily: true, Type: "feishu", URL: webhook.URL + "/raw-hook-secret", Secret: test.secret}}
			app := assembleTestApp(t, cfg)
			ids := seedPushCandidates(t, app, time.Date(2026, time.October, 21, 23, 0, 0, 0, time.UTC), 7)
			for range 2 {
				if err := app.Reports.RunOnce(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(cards) != 1 {
				t.Fatalf("cards=%d, want one request below Feishu's limit", len(cards))
			}
			card := cards[0]
			if card.MsgType != "interactive" || card.Card.Schema != "2.0" || card.Card.Header.Title.Tag != "plain_text" || card.Card.Header.Title.Content != "Vane 日报 · 2026-10-22" {
				t.Fatalf("invalid card header: %+v", card.Card.Header)
			}
			if len(card.Card.Body.Elements) != 1 || card.Card.Body.Elements[0].Tag != "markdown" {
				t.Fatalf("invalid card body: %+v", card.Card.Body)
			}
			content := card.Card.Body.Elements[0].Content
			previous := -1
			for _, i := range []int{0, 1, 2, 3, 5, 6} {
				title := fmt.Sprintf("推送标题%d", i)
				index := strings.Index(content, title)
				if index <= previous || strings.Count(content, title) != 1 || !strings.Contains(content, cfg.Site.PublicURL+"/items/"+ids[i]) {
					t.Fatalf("item %d missing, duplicated or out of order", i)
				}
				previous = index
			}
			if strings.Contains(content, "推送标题4") || !strings.Contains(content, cfg.Site.PublicURL+"/reports/2026-10-22") {
				t.Fatal("card contains a brief or lacks the full-report link")
			}
		})
	}
}

func TestReportPushSplitsFeishuBySerializedRequestBytes(t *testing.T) {
	var cards []feishuCardRequest
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) > 20*1024 {
			t.Errorf("invalid request size: %d", len(body))
		}
		var card feishuCardRequest
		if err := json.Unmarshal(body, &card); err != nil {
			t.Error(err)
		}
		cards = append(cards, card)
		_, _ = fmt.Fprint(w, `{"code":0}`)
	}))
	defer webhook.Close()
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Site.PublicURL = "https://vane.test/" + strings.Repeat("a&b", 300)
	cfg.Push.Webhooks = []config.Webhook{{Name: "group-2", Daily: true, Type: "feishu", URL: webhook.URL, Secret: "split-secret"}}
	app := assembleTestApp(t, cfg)
	ids := seedPushCandidates(t, app, time.Date(2026, time.October, 21, 23, 0, 0, 0, time.UTC), 12)
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE items SET title='标题-'||lpad(split_part(source_id,'-',2),2,'0')||'-'||repeat('中',74), category=(ARRAY['模型','产品','行业'])[split_part(source_id,'-',2)::int%3+1]`); err != nil {
		t.Fatal(err)
	}
	if err := app.Reports.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(cards) < 2 {
		t.Fatalf("cards=%d, want splitting after JSON escaping", len(cards))
	}
	for i, card := range cards {
		if !strings.Contains(card.Card.Header.Title.Content, fmt.Sprintf("（%d/%d）", i+1, len(cards))) || card.Sign == "" {
			t.Fatal("missing sequence or signature")
		}
		if !strings.Contains(card.Card.Body.Elements[0].Content, cfg.Site.PublicURL+"/reports/2026-10-22") {
			t.Fatal("missing full-report link")
		}
	}
	for i, id := range ids {
		line := fmt.Sprintf("- [标题-%02d-%s](%s/items/%s)", i, strings.Repeat("中", 74), cfg.Site.PublicURL, id)
		count := 0
		for _, card := range cards {
			count += strings.Count(card.Card.Body.Elements[0].Content, line)
		}
		if count != 1 {
			t.Fatalf("item %d whole line occurs %d times", i, count)
		}
	}
}

func TestReportSchedulerRetriesMixedPlatformsAfterFeishuBusinessFailure(t *testing.T) {
	var feishuCalls, wecomCalls int
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/feishu-private" {
			feishuCalls++
			if feishuCalls == 1 {
				_, _ = fmt.Fprint(w, `{"code":19021,"msg":"remote-private-secret"}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"code":0}`)
		} else {
			wecomCalls++
			_, _ = fmt.Fprint(w, `{"errcode":0}`)
		}
	}))
	defer webhook.Close()
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Push.Webhooks = []config.Webhook{{Name: "group-3", Daily: true, Type: "feishu", URL: webhook.URL + "/feishu-private", Secret: "signing-private"}, {Name: "group-4", Daily: true, Type: "wecom", URL: webhook.URL + "/wecom-private"}}
	app := assembleTestApp(t, cfg)
	seedPushCandidates(t, app, time.Date(2026, time.October, 21, 23, 0, 0, 0, time.UTC), 1)
	err := app.Reports.RunOnce(t.Context())
	if err == nil || !strings.Contains(err.Error(), "19021") {
		t.Fatalf("expected Feishu business error, got %v", err)
	}
	for _, secret := range []string{"feishu-private", "signing-private", "remote-private-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("error leaks private data")
		}
	}
	report, err := app.Reports.Get(t.Context(), "2026-10-22")
	if err != nil || report.PushedAt != nil {
		t.Fatalf("failed mixed delivery marked pushed: %v", err)
	}
	if feishuCalls != 1 || wecomCalls != 1 {
		t.Fatalf("first delivery calls=%d/%d", feishuCalls, wecomCalls)
	}
	for range 2 {
		if err := app.Reports.RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if feishuCalls != 2 || wecomCalls != 2 {
		t.Fatalf("retry calls=%d/%d", feishuCalls, wecomCalls)
	}
	report, err = app.Reports.Get(t.Context(), "2026-10-22")
	if err != nil || report.PushedAt == nil {
		t.Fatalf("successful mixed delivery not marked pushed: %v", err)
	}
}
