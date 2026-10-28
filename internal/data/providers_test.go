package data_test

import (
	"testing"
	"time"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
)

func TestDailyPusherRequiresDailyGroup(t *testing.T) {
	cfg := config.Config{Push: config.Push{RequestTimeout: time.Second, Webhooks: []config.Webhook{{Name: "ops", Alerts: true}}}}
	if data.NewPusher(cfg, data.NewPushClient(cfg)) != nil {
		t.Fatal("scheduler enabled without a daily group")
	}
	cfg.Push.Webhooks[0].Daily = true
	if data.NewPusher(cfg, data.NewPushClient(cfg)) == nil {
		t.Fatal("daily group has no pusher")
	}
}
