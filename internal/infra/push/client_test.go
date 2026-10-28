package push_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/push"
)

func TestPushRejectsRedirectAndNamesGroup(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = w.Write([]byte(`{"errcode":0}`)) }))
	defer target.Close()
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer hook.Close()
	client := push.New(config.Push{RequestTimeout: time.Second, Webhooks: []config.Webhook{{Name: "readers", Daily: true, Type: config.WebhookTypeWecom, URL: hook.URL + "/private-secret"}}}, config.Site{Name: "Vane", PublicURL: "https://example.test"})
	err := client.Push(t.Context(), biz.Report{Date: "2026-10-23", Headline: &biz.Item{ID: "1", Title: "标题"}})
	if err == nil || !strings.Contains(err.Error(), "readers wecom part 1: HTTP 302") {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("followed redirect")
	}
	if strings.Contains(err.Error(), hook.URL) || strings.Contains(err.Error(), "private-secret") {
		t.Fatal("leaked webhook")
	}
}

func TestPushOnlySendsDailyGroups(t *testing.T) {
	var daily, other atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/daily" {
			daily.Add(1)
		} else {
			other.Add(1)
		}
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer hook.Close()
	client := push.New(config.Push{RequestTimeout: time.Second, Webhooks: []config.Webhook{{Name: "daily", Daily: true, Type: "wecom", URL: hook.URL + "/daily"}, {Name: "ops", Type: "wecom", URL: hook.URL + "/ops"}}}, config.Site{})
	if err := client.Push(t.Context(), biz.Report{}); err != nil {
		t.Fatal(err)
	}
	if daily.Load() != 1 || other.Load() != 0 {
		t.Fatalf("calls daily=%d other=%d", daily.Load(), other.Load())
	}
}

func TestSendMessagesToNamedGroup(t *testing.T) {
	var contents []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ops" {
			t.Error("message reached another group")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		contents = append(contents, string(body))
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer hook.Close()
	client := push.New(config.Push{RequestTimeout: time.Second, Webhooks: []config.Webhook{{Name: "readers", Type: "wecom", URL: hook.URL + "/readers"}, {Name: "ops", Type: "wecom", URL: hook.URL + "/ops"}}}, config.Site{})
	if err := client.Send(t.Context(), "ops", []biz.PushMessage{{Title: "first", Content: "one"}, {Title: "second", Content: "two"}}); err != nil {
		t.Fatal(err)
	}
	if len(contents) != 2 || !strings.Contains(contents[0], "first") || !strings.Contains(contents[1], "second") {
		t.Fatalf("unexpected messages: %v", contents)
	}
}
