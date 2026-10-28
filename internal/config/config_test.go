package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(sample)
	t.Setenv("SERVER_ADDRESS", "0.0.0.0:9999")
	for _, test := range []struct {
		name, content string
		valid         bool
	}{
		{"valid", valid, true},
		{"request deadline equals write timeout", strings.Replace(valid, "request_timeout: 5s", "request_timeout: 10s", 1), false},
		{"missing request deadline", strings.Replace(valid, "  request_timeout: 5s\n", "", 1), false},
		{"zero max open connections", strings.Replace(valid, "max_open_conns: 20", "max_open_conns: 0", 1), false},
		{"idle exceeds open connections", strings.Replace(valid, "max_idle_conns: 10", "max_idle_conns: 21", 1), false},
		{"zero connection lifetime", strings.Replace(valid, "conn_max_lifetime: 30m", "conn_max_lifetime: 0s", 1), false},
		{"missing max open connections", strings.Replace(valid, "  max_open_conns: 20\n", "", 1), false},
		{"missing max idle connections", strings.Replace(valid, "  max_idle_conns: 10\n", "", 1), false},
		{"missing connection lifetime", strings.Replace(valid, "  conn_max_lifetime: 30m\n", "", 1), false},

		{"LLM disabled with empty API key", valid, true},
		{"LLM enabled with empty API key", strings.ReplaceAll(valid, "enabled: false\n  base_url: https://api.deepseek.com", "enabled: true\n  base_url: https://api.deepseek.com"), false},
		{"all interfaces", strings.ReplaceAll(valid, "127.0.0.1:8080", ":8080"), true},
		{"hostname", strings.ReplaceAll(valid, "127.0.0.1:8080", "localhost:8080"), true},
		{"IPv6", strings.ReplaceAll(valid, "127.0.0.1:8080", "'[::1]:8080'"), true},
		{"explicit zero", strings.ReplaceAll(valid, "max_backups: 7", "max_backups: 0"), true},
		{"console disabled", strings.ReplaceAll(valid, "console: true", "console: false"), true},
		{"explicit false", strings.ReplaceAll(valid, "compress: true", "compress: false"), true},
		{"missing console", strings.ReplaceAll(valid, "  console: true\n", ""), false},
		{"missing compress", strings.ReplaceAll(valid, "  compress: true\n", ""), false},
		{"null compress", strings.ReplaceAll(valid, "compress: true", "compress: null"), false},
		{"missing section", strings.Split(valid, "\nlog:")[0], false},
		{"unknown main field", strings.Replace(valid, "server:\n", "secret-setting: secret-value\nserver:\n", 1), false},
		{"unknown", valid + "unknown: true\n", false},
		{"unknown nested", strings.Replace(valid, "  level: info\n", "  level: info\n  secret-format: secret-value\n", 1), false},
		{"wrong bool", strings.ReplaceAll(valid, "compress: true", "compress: invalid"), false},
		{"quoted integer", strings.ReplaceAll(valid, "max_size: 500", "max_size: \"500\""), false},
		{"fractional integer", strings.ReplaceAll(valid, "max_size: 500", "max_size: 1.5"), false},
		{"duration without units", strings.ReplaceAll(valid, "read_timeout: 10s", "read_timeout: 10"), false},
		{"bad duration", strings.ReplaceAll(valid, "read_timeout: 10s", "read_timeout: nonsense"), false},
		{"zero duration", strings.ReplaceAll(valid, "read_timeout: 10s", "read_timeout: 0s"), false},
		{"bad level", strings.ReplaceAll(valid, "level: info", "level: invalid"), false},
		{"numeric level", strings.ReplaceAll(valid, "level: info", "level: 0"), false},
		{"empty filename", strings.ReplaceAll(valid, "filename: logs/app.log", "filename: ''"), false},
		{"zero size", strings.ReplaceAll(valid, "max_size: 500", "max_size: 0"), false},
		{"bad port", strings.ReplaceAll(valid, "127.0.0.1:8080", "127.0.0.1:0"), false},
		{"missing port", strings.ReplaceAll(valid, "127.0.0.1:8080", "localhost"), false},
		{"broken yaml", "server: [", false},
		{"empty", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeTestConfig(t, test.content)
			config, err := Load(path)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, got error %v", test.valid, err)
			}
			if test.valid && !strings.Contains(test.content, config.Server.Address) {
				t.Fatalf("environment overrode configuration: %s", config.Server.Address)
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err := Load("config.json"); err == nil {
		t.Fatal("non-YAML file accepted")
	}
}

func TestLoadRejectsServerAndLogOnlyConfig(t *testing.T) {
	const legacyConfig = `server:
  address: 127.0.0.1:8080
  read_timeout: 10s
  write_timeout: 10s
  idle_timeout: 60s
  shutdown_timeout: 10s

log:
  level: info
  console: false
  filename: logs/app.log
  max_size: 500
  max_backups: 7
  max_age: 30
  compress: true
`
	path := filepath.Join(t.TempDir(), "legacy.yaml")
	if err := os.WriteFile(path, []byte(legacyConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("server/log-only configuration accepted; new required fields are missing")
	}
}

func TestLoadRejectsMissingNewConfigSectionsAndFields(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(sample)
	var cases []struct{ name, content string }
	for _, section := range []string{"server", "log", "database", "site", "llm", "collect", "worker", "web", "sources"} {
		cases = append(cases, struct{ name, content string }{"missing " + section + " section", withoutTopLevelSection(valid, section)})
	}
	for _, field := range []struct{ name, old, replacement string }{
		{"site.name", "  name: Vane\n", ""},
		{"site.public_url", "  public_url: http://localhost:8080\n", ""},
		{"llm.enabled", "llm:\n  enabled: false\n", "llm:\n"},
		{"llm.base_url", "  base_url: https://api.deepseek.com\n", ""},
		{"llm.api_key", "  api_key: \"\"\n", ""},
		{"llm.model", "  model: deepseek-chat\n", ""},
		{"collect.enabled", "collect:\n  enabled: false\n", "collect:\n"},
		{"worker.count", "  count: 2\n", ""},
		{"web.dir", "  dir: web/dist\n", ""},
		{"sources.official[0].id", "    - id: rss-openai-news\n", "    -\n"},
		{"sources.official[0].name", "      name: \"OpenAI News\"\n", ""},
		{"sources.official[0].feed_url", "      feed_url: https://openai.com/news/rss.xml\n", ""},
		{"sources.official[0].enabled", "      enabled: true\n", ""},
		{"sources.official[0].interval_minutes", "      interval_minutes: 120\n", ""},
	} {
		cases = append(cases, struct{ name, content string }{"missing " + field.name, replaceOnce(t, valid, field.old, field.replacement)})
	}
	cases = append(cases, struct{ name, content string }{"missing database.url", replaceDatabaseURL(t, valid, "")})
	for _, field := range []struct{ name, old, null string }{
		{"site.name", "  name: Vane\n", "  name: null\n"},
		{"site.public_url", "  public_url: http://localhost:8080\n", "  public_url: null\n"},
		{"llm.enabled", "llm:\n  enabled: false\n", "llm:\n  enabled: null\n"},
		{"llm.base_url", "  base_url: https://api.deepseek.com\n", "  base_url: null\n"},
		{"llm.api_key", "  api_key: \"\"\n", "  api_key: null\n"},
		{"llm.model", "  model: deepseek-chat\n", "  model: null\n"},
		{"collect.enabled", "collect:\n  enabled: false\n", "collect:\n  enabled: null\n"},
		{"worker.count", "  count: 2\n", "  count: null\n"},
		{"web.dir", "  dir: web/dist\n", "  dir: null\n"},
		{"sources.official[0].id", "    - id: rss-openai-news\n", "    - id: null\n"},
		{"sources.official[0].name", "      name: \"OpenAI News\"\n", "      name: null\n"},
		{"sources.official[0].feed_url", "      feed_url: https://openai.com/news/rss.xml\n", "      feed_url: null\n"},
		{"sources.official[0].enabled", "      enabled: true\n", "      enabled: null\n"},
		{"sources.official[0].interval_minutes", "      interval_minutes: 120\n", "      interval_minutes: null\n"},
	} {
		cases = append(cases, struct{ name, content string }{"null " + field.name, replaceOnce(t, valid, field.old, field.null)})
	}
	cases = append(cases, struct{ name, content string }{"null database.url", replaceDatabaseURL(t, valid, "null")})
	firstSource := strings.Index(valid, "    - id: rss-openai-news\n")
	nextSource := strings.Index(valid[firstSource+1:], "    - id: rss-google-deepmind\n") + firstSource + 1
	cases = append(cases,
		struct{ name, content string }{"null sources list", withoutTopLevelSection(valid, "sources") + "sources: null\n"},
		struct{ name, content string }{"null source item", valid[:firstSource] + "    - null\n" + valid[nextSource:]},
	)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := writeTestConfig(t, test.content)
			if _, err := Load(path); err == nil {
				t.Fatal("configuration accepted")
			}
		})
	}
}

func TestLoadAllowsDisabledLLMWithEmptyOptionalFields(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	content := string(sample)
	for _, field := range []struct{ old, replacement string }{
		{"base_url: https://api.deepseek.com", "base_url: ''"},
		{"api_key: \"\"", "api_key: ''"},
		{"model: deepseek-chat", "model: ''"},
	} {
		content = replaceOnce(t, content, field.old, field.replacement)
	}
	path := writeTestConfig(t, content)
	if _, err := Load(path); err != nil {
		t.Fatalf("disabled LLM with empty optional fields rejected: %v", err)
	}
}

func TestLoadAllowsDisabledLLMWithInvalidOptionalURL(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	content := replaceOnce(t, string(sample), "base_url: https://api.deepseek.com", "base_url: not a URL")
	path := writeTestConfig(t, content)
	if _, err := Load(path); err != nil {
		t.Fatalf("disabled LLM with unused invalid URL rejected: %v", err)
	}
}

func TestLoadRejectsWhitespaceOnlyRequiredValuesWithoutLeakingSecrets(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(sample)
	databaseSecret := "postgres://user:db-secret@localhost/vane_test"
	apiKeySecret := "api-secret-value"
	content := replaceDatabaseURL(t, valid, databaseSecret)
	content = replaceOnce(t, content, "  enabled: false\n", "  enabled: true\n")
	content = replaceOnce(t, content, "  api_key: \"\"\n", "  api_key: "+apiKeySecret+"\n")
	content = replaceOnce(t, content, "name: Vane\n", "name: '   '\n")
	path := writeTestConfig(t, content)
	_, err = Load(path)
	if err == nil {
		t.Fatal("whitespace-only required value accepted")
	}
	for _, secret := range []string{databaseSecret, "db-secret", apiKeySecret} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("configuration error leaked a secret: %q", secret)
		}
	}
}

func TestLoadAcceptsMaximumSourceInterval(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	maxMinutes := int64((1<<63 - 1) / time.Minute)
	content := replaceOnce(t, string(sample), "interval_minutes: 120", "interval_minutes: "+strconv.FormatInt(maxMinutes, 10))
	path := writeTestConfig(t, content)
	if _, err := Load(path); err != nil {
		t.Fatalf("maximum representable source interval rejected: %v", err)
	}
}

func TestLoadRejectsInvalidNewConfigValues(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(sample)
	llmEnabled := replaceOnce(t, valid, "  enabled: false\n", "  enabled: true\n")
	llmEnabled = replaceOnce(t, llmEnabled, "  api_key: \"\"\n", "  api_key: test-key\n")
	cases := []struct{ name, content string }{
		{"empty database URL", replaceDatabaseURL(t, valid, "''")},
		{"invalid database URL", replaceDatabaseURL(t, valid, "postgres://localhost")},
		{"empty site name", replaceOnce(t, valid, "name: Vane\n", "name: ''\n")},
		{"invalid site URL", replaceOnce(t, valid, "http://localhost:8080", "ftp://localhost:8080")},
		{"invalid enabled LLM URL", replaceOnce(t, llmEnabled, "https://api.deepseek.com", "ftp://api.deepseek.com")},
		{"worker exhausts connection pool", replaceOnce(t, llmEnabled, "count: 2", "count: 20")},
		{"zero worker count", replaceOnce(t, valid, "count: 2", "count: 0")},
		{"empty web directory", replaceOnce(t, valid, "dir: web/dist", "dir: ''")},
		{"empty source id", replaceOnce(t, valid, "id: rss-openai-news", "id: ''")},
		{"empty source name", replaceOnce(t, valid, "name: \"OpenAI News\"", "name: ''")},
		{"empty source URL", replaceOnce(t, valid, "feed_url: https://openai.com/news/rss.xml", "feed_url: ''")},
		{"invalid source URL", replaceOnce(t, valid, "https://openai.com/news/rss.xml", "ftp://openai.com/news/rss.xml")},
		{"zero source interval", replaceOnce(t, valid, "interval_minutes: 120", "interval_minutes: 0")},
		{"source interval duration overflow", replaceOnce(t, valid, "interval_minutes: 120", "interval_minutes: "+strconv.FormatInt((1<<63-1)/int64(time.Minute)+1, 10))},
		{"duplicate source id", replaceOnce(t, valid, "    - id: rss-google-deepmind", "    - id: rss-openai-news")},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := writeTestConfig(t, test.content)
			if _, err := Load(path); err == nil {
				t.Fatal("configuration accepted")
			}
		})
	}
}

func withoutTopLevelSection(content, section string) string {
	var result strings.Builder
	skipping := false
	for line := range strings.SplitAfterSeq(content, "\n") {
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(strings.TrimSpace(line), ":") {
			skipping = strings.TrimSuffix(strings.TrimSpace(line), ":") == section
		}
		if !skipping {
			result.WriteString(line)
		}
	}
	return result.String()
}

func replaceOnce(t *testing.T, content, old, replacement string) string {
	t.Helper()
	if !strings.Contains(content, old) {
		t.Fatalf("sample config does not contain %q", old)
	}
	return strings.Replace(content, old, replacement, 1)
}

func replaceDatabaseURL(t *testing.T, content, value string) string {
	t.Helper()
	lines := strings.SplitAfter(content, "\n")
	inDatabase := false
	found := false
	for i, line := range lines {
		if strings.TrimSpace(line) == "database:" {
			inDatabase = true
			continue
		}
		if inDatabase && strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ") {
			break
		}
		if inDatabase && strings.HasPrefix(strings.TrimSpace(line), "url:") {
			if value == "" {
				lines[i] = ""
			} else {
				lines[i] = "  url: " + value + "\n"
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("sample config is missing database.url")
	}
	return strings.Join(lines, "")
}

// Source mutations are written to their own file, never into the main YAML.
func readConfigSample(t *testing.T) ([]byte, error) {
	t.Helper()
	main, err := os.ReadFile("../../configs/config.example.yaml")
	if err != nil {
		return nil, err
	}
	sources, err := os.ReadFile("../../configs/sources.example.yaml")
	if err != nil {
		return nil, err
	}
	return append(main, sources...), nil
}

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	main, sources, found := strings.Cut(content, "sources:")
	if found {
		sources = "sources:" + sources
	}
	for name, data := range map[string]string{"config.yaml": main, "sources.yaml": sources} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "config.yaml")
}

func TestSourcesFile(t *testing.T) {
	sample, err := os.ReadFile("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sourceSample, err := os.ReadFile("../../configs/sources.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, main, sources string
		missing, valid      bool
	}{
		{name: "relative path from another directory", valid: true},
		{name: "absolute path", valid: true},
		{name: "missing file", missing: true},
		{name: "malformed YAML", sources: "sources: ["},
		{name: "missing sources list", sources: "unknown: true"},
		{name: "empty sources list", sources: "sources:\n  official: []\n  media: []", valid: true},
		{name: "missing sources_file", main: strings.Replace(string(sample), "sources_file: sources.yaml", "", 1)},
		{name: "null sources_file", main: strings.Replace(string(sample), "sources_file: sources.yaml", "sources_file: null", 1)},
		{name: "blank sources_file", main: strings.Replace(string(sample), "sources_file: sources.yaml", "sources_file: ' '", 1)},
		{name: "wrong sources_file type", main: strings.Replace(string(sample), "sources_file: sources.yaml", "sources_file: true", 1)},
		{name: "inline conflict", main: string(sample) + "sources: []\n"},
		{name: "inline null conflict", main: string(sample) + "sources: null\n"},
		{name: "source field wrong type", sources: strings.Replace(string(sourceSample), "enabled: true", "enabled: invalid", 1)},
		{name: "source quoted integer", sources: strings.Replace(string(sourceSample), "interval_minutes: 120", "interval_minutes: '120'", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			main := string(sample)
			if test.main != "" {
				main = test.main
			}
			sources := string(sourceSample)
			if test.sources != "" {
				sources = test.sources
			}
			if test.name == "absolute path" {
				main = strings.Replace(main, "sources_file: sources.yaml", "sources_file: "+filepath.Join(dir, "sources.yaml"), 1)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(main), 0o600); err != nil {
				t.Fatal(err)
			}
			if !test.missing {
				if err := os.WriteFile(filepath.Join(dir, "sources.yaml"), []byte(sources), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load(filepath.Join(dir, "config.yaml"))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
			if strings.HasPrefix(test.name, "inline") && (err == nil || !strings.Contains(err.Error(), "inline sources")) {
				t.Fatalf("expected explicit inline sources rejection, got %v", err)
			}
			if test.valid {
				want := 28
				if test.name == "empty sources list" {
					want = 0
				}
				if len(cfg.Sources) != want {
					t.Fatalf("sources count = %d, want %d", len(cfg.Sources), want)
				}
			}
		})
	}
}

func TestLoadSelectionThresholds(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(sample)
	for _, test := range []struct {
		name, old, replacement string
		valid                  bool
	}{
		{"reject old threshold T1", "official: 60", "T1: 60", false},
		{"reject old threshold T1_5", "official: 60", "T1_5: 60", false},
		{"reject old threshold T2", "official: 60", "T2: 60", false},
		{"all thresholds", "", "", true},
		{"zero allowed", "official: 60", "official: 0", true},
		{"hundred allowed", "media: 76", "media: 100", true},
		{"missing selection", "selection:\n  thresholds:\n    official: 60\n    media: 76\n", "", false},
		{"missing thresholds", "  thresholds:\n    official: 60\n    media: 76", "  unknown: true", false},
		{"missing official", "    official: 60\n", "", false},
		{"missing media", "    media: 76\n", "", false},
		{"negative official", "official: 60", "official: -1", false},
		{"too high media", "media: 76", "media: 987654321", false},
		{"fractional media", "media: 76", "media: 60.5", false},
		{"quoted media", "media: 76", "media: '60'", false},
		{"null media", "media: 76", "media: null", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := valid
			if test.old != "" {
				content = replaceOnce(t, content, test.old, test.replacement)
			}
			_, err := Load(writeTestConfig(t, content))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
			if err != nil && strings.Contains(err.Error(), "987654321") {
				t.Fatalf("configuration value leaked: %v", err)
			}
		})
	}
}

func TestLoadRealtimeFilterValidation(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, realtime, want string }{
		{"unknown topic", `{enabled: true, scope: selected, topics: [secret-topic], words: []}`, "known_topic"},
		{"invalid regexp", `{enabled: true, scope: selected, topics: [], words: [["/[secret/"]]}`, "valid_regexp"},
		{"required only", `{enabled: true, scope: selected, topics: [], words: [["+secret"]]}`, "ordinary_word"},
		{"excluded only", `{enabled: true, scope: selected, topics: [], words: [["!secret"]]}`, "ordinary_word"},
		{"empty group", `{enabled: true, scope: selected, topics: [], words: [[]]}`, "ordinary_word"},
		{"empty term", `{enabled: true, scope: selected, topics: [], words: [[""]]}`, "nonempty_word"},
		{"all unfiltered", `{enabled: true, scope: all, topics: [], words: []}`, "requires_filter"},
		{"valid", `{enabled: true, scope: all, topics: [anthropic], words: [[Claude, '+发布', '!招聘']]}`, ""},
		{"disabled all", `{enabled: false, scope: all, topics: [], words: []}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hook := `webhooks:
    - name: readers
      type: wecom
      url: https://example.test/secret-url
      secret: ""
      daily: false
      alerts: false
      realtime: ` + tt.realtime
			content := strings.Replace(string(sample), "webhooks: []", hook, 1)
			_, err := Load(writeTestConfig(t, content))
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("validation=%v want sanitized %s", err, tt.want)
			}
		})
	}
}

func TestSourceBlockValidation(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	base := string(sample)
	for _, tt := range []struct{ value, rule string }{{"MISSING", "required"}, {"[[\"!secret\"]]", "ordinary_word"}, {"[[]]", "ordinary_word"}, {"[[\"\"]]", "nonempty_word"}, {"[[\"/[secret/\"]]", "valid_regexp"}} {
		t.Run(tt.rule+tt.value, func(t *testing.T) {
			replacement := "      block: " + tt.value + "\n"
			if tt.value == "MISSING" {
				replacement = ""
			}
			content := strings.Replace(base, "      block: []\n", replacement, 1)
			_, err := Load(writeTestConfig(t, content))
			if err == nil || !strings.Contains(err.Error(), "sources.official[0].block") || !strings.Contains(err.Error(), tt.rule) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected sanitized block rule %s, got %v", tt.rule, err)
			}
		})
	}
}
