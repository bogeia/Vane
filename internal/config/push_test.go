package config

import (
	"strings"
	"testing"
)

func TestLoadPushConfiguration(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	const block = "push:\n  request_timeout: 10s\n  webhooks: []\n"
	valid := string(sample)
	const webhook = "  webhooks:\n    - name: main\n      type: wecom\n      url: https://example.test/bot?key=raw-webhook-secret\n      secret: \"\"\n      daily: true\n      alerts: false\n      realtime:\n        enabled: false\n        scope: selected\n        topics: []\n        words: []"
	withWebhook := strings.Replace(valid, "  webhooks: []", webhook, 1)
	for _, field := range []string{"name", "daily", "alerts", "realtime", "enabled", "scope", "topics", "words"} {
		t.Run("missing "+field, func(t *testing.T) {
			lines := strings.Split(withWebhook, "\n")
			for i, line := range lines {
				if strings.HasPrefix(strings.TrimSpace(line), field+":") || strings.HasPrefix(strings.TrimSpace(line), "- "+field+":") {
					lines[i] = ""
					break
				}
			}
			if _, err := Load(writeTestConfig(t, strings.Join(lines, "\n"))); err == nil {
				t.Fatal("missing field accepted")
			}
		})
	}
	for _, test := range []struct {
		name, content string
		valid         bool
	}{
		{"empty webhooks", valid, true},
		{"blank name", strings.Replace(withWebhook, "name: main", "name: \"\"", 1), false},
		{"duplicate name", strings.Replace(valid, "  webhooks: []", webhook+"\n"+strings.SplitN(webhook, "  webhooks:\n", 2)[1], 1), false},
		{"invalid scope", strings.Replace(withWebhook, "scope: selected", "scope: bad", 1), false},
		{"wecom", withWebhook, true},
		{"feishu", strings.Replace(strings.Replace(withWebhook, "type: wecom", "type: feishu", 1), "secret: \"\"", "secret: signing-secret", 1), true},
		{"missing push", strings.Replace(valid, block, "", 1), false},
		{"missing timeout", strings.Replace(valid, "  request_timeout: 10s\n", "", 1), false},
		{"missing webhooks", strings.Replace(valid, "  webhooks: []\n", "", 1), false},
		{"null push", strings.Replace(valid, block, "push: null\n", 1), false},
		{"null webhooks", strings.Replace(valid, "webhooks: []", "webhooks: null", 1), false},
		{"zero timeout", strings.Replace(valid, "request_timeout: 10s", "request_timeout: 0s", 1), false},
		{"negative timeout", strings.Replace(valid, "request_timeout: 10s", "request_timeout: -1s", 1), false},
		{"invalid type", strings.Replace(withWebhook, "type: wecom", "type: raw-webhook-secret", 1), false},
		{"invalid URL", strings.Replace(withWebhook, "https://example.test/bot", "ftp://example.test/bot", 1), false},
		{"wecom secret", strings.Replace(withWebhook, "secret: \"\"", "secret: signing-secret", 1), false},
		{"missing type", strings.Replace(withWebhook, "type: wecom", "", 1), false},
		{"missing URL", strings.Replace(withWebhook, "      url: https://example.test/bot?key=raw-webhook-secret\n", "", 1), false},
		{"missing secret", strings.Replace(withWebhook, "      secret: \"\"", "", 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeTestConfig(t, test.content)
			for _, load := range []func(string) (Config, error){Load, LoadEvaluation} {
				_, err := load(path)
				if (err == nil) != test.valid {
					t.Fatalf("valid=%v, error=%v", test.valid, err)
				}
				if err != nil {
					for _, secret := range []string{"raw-webhook-secret", "signing-secret", "https://example.test/bot"} {
						if strings.Contains(err.Error(), secret) {
							t.Fatalf("configuration error leaked webhook value: %v", err)
						}
					}
				}
			}
		})
	}
}
