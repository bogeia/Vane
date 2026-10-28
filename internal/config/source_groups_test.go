package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSourceGroups(t *testing.T) {
	main, err := os.ReadFile("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	source := "    - id: fixture\n      kind: rss\n      name: secret-source\n      feed_url: https://example.test/feed\n      enabled: false\n      interval_minutes: 60\n      block: []\n"
	valid := "sources:\n  official: []\n  media:\n" + source
	for _, tt := range []struct{ name, content, path string }{
		{"group assigns tier", valid, ""},
		{"manual icon", valid + "      icon_url: https://example.test/icon.svg\n", ""},
		{"invalid manual icon", valid + "      icon_url: file:///secret\n", "sources.media[0].icon_url"},
		{"empty manual icon", valid + "      icon_url: \"\"\n", "sources.media[0].icon_url"},
		{"official assigns tier", "sources:\n  media: []\n  official:\n" + source, ""},
		{"empty groups", "sources:\n  official: []\n  media: []\n", ""},
		{"missing official", "sources:\n  media: []\n", "sources.official"},
		{"missing media", "sources:\n  official: []\n", "sources.media"},
		{"unknown source root field", valid + "secret-setting: secret-value\n", "sources"},
		{"unknown source field", valid + "      secret-setting: secret-value\n", "sources"},
		{"unknown source nested field", strings.Replace(valid, "kind: rss", "kind: web_list", 1) + "      web_list:\n        item: article\n        link: a\n        title: h2\n        date: time\n        secret-selector: secret-value\n", "sources"},
		{"unknown group", valid + "  secret-group: []\n", "sources"},
		{"old list", "sources:\n" + source, "sources"},
		{"tier forbidden", valid + "      tier: official\n", "sources.media[0].tier"},
		{"duplicate across groups", "sources:\n  official:\n" + source + "  media:\n" + source, "sources.media[0].id"},
		{"missing kind", strings.Replace(valid, "      kind: rss\n", "", 1), "sources.media[0].kind"},
		{"third media source missing block", "sources:\n  official: []\n  media:\n" + strings.Replace(source, "id: fixture", "id: first", 1) + strings.Replace(source, "id: fixture", "id: second", 1) + strings.Replace(source, "      block: []\n", "", 1), "sources.media[2].block"},
		{"media web selector required", strings.Replace(valid, "kind: rss", "kind: web_list", 1) + "      web_list:\n        item: article\n        link: a\n        title: h2\n", "sources.media[0].web_list.date"},
		{"media block words", strings.Replace(valid, "block: []", "block: [[\"!secret\"]]", 1), "sources.media[0].block"},
		{"missing block", strings.Replace(valid, "      block: []\n", "", 1), "sources.media[0].block"},
		{"invalid interval", strings.Replace(valid, "interval_minutes: 60", "interval_minutes: -1", 1), "sources.media[0].interval_minutes"},
		{"null group", strings.Replace(valid, "official: []", "official: null", 1), "sources.official"},
		{"web list required", strings.Replace(valid, "kind: rss", "kind: web_list", 1), "sources.media[0].web_list"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(strings.Replace(string(main), "sources.example.yaml", "sources.yaml", 1)), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "sources.yaml"), []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(filepath.Join(dir, "config.yaml"))
			if tt.path != "" {
				if err == nil || !strings.Contains(err.Error(), tt.path) || strings.Contains(err.Error(), "secret") {
					t.Fatalf("error = %v, want safe path %s", err, tt.path)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.name == "official assigns tier" && (len(cfg.Sources) != 1 || cfg.Sources[0].Tier != "official") {
				t.Fatalf("sources = %+v", cfg.Sources)
			}
			if tt.name == "group assigns tier" && (len(cfg.Sources) != 1 || cfg.Sources[0].Tier != "media") {
				t.Fatalf("sources = %+v", cfg.Sources)
			}
		})
	}
}

func TestUnknownConfigFieldsRejectEmptyValues(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(sample)
	for _, value := range []string{"{}", "null"} {
		for _, tc := range []struct{ name, content string }{
			{"main root", strings.Replace(valid, "server:\n", "secret-key: "+value+"\nserver:\n", 1)},
			{"main nested", strings.Replace(valid, "fetch:\n", "fetch:\n  secret-key: "+value+"\n", 1)},
			{"source root", valid + "secret-key: " + value + "\n"},
			{"source group", strings.Replace(valid, "  official:\n", "  secret-key: "+value+"\n  official:\n", 1)},
			{"source field", strings.Replace(valid, "      kind: rss\n", "      kind: rss\n      secret-key: "+value+"\n", 1)},
			{"source nested", strings.Replace(valid, "        item:", "        secret-key: "+value+"\n        item:", 1)},
		} {
			t.Run(tc.name+value, func(t *testing.T) {
				_, err := Load(writeTestConfig(t, tc.content))
				if err == nil || strings.Contains(err.Error(), "secret") {
					t.Fatalf("expected safe unknown-field rejection, got %v", err)
				}
			})
		}
	}
}
