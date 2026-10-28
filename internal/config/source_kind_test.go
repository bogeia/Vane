package config

import (
	"strings"
	"testing"
)

func TestSourceKindConfiguration(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(sample)
	web := "      web_list:\n        item: article\n        link: a\n        title: h2\n        date: time\n"
	for _, tc := range []struct{ name, content, path string }{
		{"missing kind", strings.Replace(valid, "      kind: rss\n", "", 1), "kind"},
		{"invalid kind", strings.Replace(valid, "      kind: rss", "      kind: secret-invalid", 1), "kind"},
		{"web list missing section", strings.Replace(valid, "      kind: rss", "      kind: web_list", 1), "web_list"},
		{"missing selector", strings.Replace(valid, "      kind: rss", "      kind: web_list\n"+strings.Replace(web, "        date: time\n", "", 1), 1), "date"},
		{"invalid selector", strings.Replace(valid, "      kind: rss", "      kind: web_list\n"+strings.Replace(web, "item: article", "item: '[secret'", 1), 1), "item"},
		{"blank selector", strings.Replace(valid, "      kind: rss", "      kind: web_list\n"+strings.Replace(web, "title: h2", "title: '  '", 1), 1), "title"},
		{"hf with section", strings.Replace(strings.Replace(valid, "      kind: rss", "      kind: hf_models\n"+web, 1), "https://openai.com/news/rss.xml", "https://huggingface.co/Qwen", 1), "web_list"},
		{"rss with section", strings.Replace(valid, "      kind: rss", "      kind: rss\n"+web, 1), "web_list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeTestConfig(t, tc.content))
			if err == nil || (tc.path != "" && !strings.Contains(err.Error(), tc.path)) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected sanitized error for %s, got %v", tc.name, err)
			}
		})
	}
}
