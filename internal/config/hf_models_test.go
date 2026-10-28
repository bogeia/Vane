package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHFModelsSourceURL(t *testing.T) {
	sample, err := readConfigSample(t)
	require.NoError(t, err)
	for _, tc := range []struct {
		url   string
		valid bool
	}{
		{"https://huggingface.co/Qwen", true},
		{"https://huggingface.co/deepseek-ai", true},
		{"https://huggingface.co/zai-org", true},
		{"https://huggingface.co/secret/org", false},
		{"https://huggingface.co/secret?x=secret", false},
		{"https://huggingface.co/secret?", false},
		{"https://huggingface.co/secret#secret", false},
		{"https://huggingface.co/secret#", false},
		{"https://secret.example/Qwen", false},
		{"http://huggingface.co/secret", false},
		{"https://huggingface.co:443/secret", false},
		{"https://secret@huggingface.co/org", false},
		{"https://huggingface.co/", false},
		{"https://huggingface.co/secret/", false},
		{"https://huggingface.co/secret%2Fother", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			content := strings.Replace(string(sample), "kind: rss", "kind: hf_models", 1)
			content = strings.Replace(content, "https://openai.com/news/rss.xml", tc.url, 1)
			_, err := Load(writeTestConfig(t, content))
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "feed_url")
				assert.Contains(t, err.Error(), "hf_organization_url")
				assert.NotContains(t, err.Error(), "secret")
			}
		})
	}
}
