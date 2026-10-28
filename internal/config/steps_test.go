package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresEveryModelStepAndField(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"prefilter", "score", "write", "group"} {
		for _, field := range []string{"base_url", "api_key", "model", "reasoning_effort"} {
			t.Run(step+"/"+field, func(t *testing.T) {
				content := string(sample)
				start := strings.Index(content, "    "+step+":\n")
				if start < 0 {
					t.Fatal("sample lacks step")
				}
				tail := content[start:]
				offset := strings.Index(tail, "      "+field+":")
				if offset < 0 {
					t.Fatal("sample lacks field")
				}
				lineEnd := strings.Index(tail[offset:], "\n") + offset + 1
				content = content[:start] + tail[:offset] + tail[lineEnd:]
				if _, err := Load(writeTestConfig(t, content)); err == nil {
					t.Fatal("missing step field accepted")
				}
			})
		}
		t.Run("missing/"+step, func(t *testing.T) {
			content := string(sample)
			start := strings.Index(content, "    "+step+":\n")
			if start < 0 {
				t.Fatal("sample lacks step")
			}
			tail := content[start:]
			end := len(tail)
			for i, line := range strings.SplitAfter(tail, "\n") {
				if i > 0 && strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "      ") {
					end = strings.Index(tail, line)
					break
				}
			}
			if _, err := Load(writeTestConfig(t, content[:start]+tail[end:])); err == nil {
				t.Fatal("missing step accepted")
			}
		})
	}
}

func TestLoadValidatesMergedModelStepsWithoutLeakingValues(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	enabled := replaceOnce(t, string(sample), "llm:\n  enabled: false", "llm:\n  enabled: true")
	enabled = replaceOnce(t, enabled, "  api_key: \"\"\n", "  api_key: shared-secret\n")
	for _, step := range []string{"prefilter", "score", "write", "group"} {
		for _, tc := range []struct {
			field, value string
			valid        bool
		}{
			{"base_url", "ftp://private-secret.example", false},
			{"api_key", "   ", false},
			{"model", "   ", false},
			{"base_url", "https://models.example/v1", true},
			{"api_key", "override-secret", true},
			{"model", "other-model", true},
		} {
			t.Run(step+"/"+tc.field+"/"+tc.value, func(t *testing.T) {
				start := strings.Index(enabled, "    "+step+":\n")
				content := enabled[:start] + strings.Replace(enabled[start:], "      "+tc.field+": \"\"", "      "+tc.field+": \""+tc.value+"\"", 1)
				_, err := Load(writeTestConfig(t, content))
				if (err == nil) != tc.valid {
					t.Fatalf("valid=%v error=%v", tc.valid, err)
				}
				if err != nil && (strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), tc.value) && strings.TrimSpace(tc.value) != "") {
					t.Fatalf("configuration value leaked: %v", err)
				}
				disabled := strings.Replace(content, "llm:\n  enabled: true", "llm:\n  enabled: false", 1)
				if _, err := Load(writeTestConfig(t, disabled)); err != nil {
					t.Fatalf("disabled unused step rejected: %v", err)
				}
			})
		}
	}
}

func TestLoadAllowsAllStepsToOverrideEmptySharedValues(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	content := replaceOnce(t, string(sample), "llm:\n  enabled: false", "llm:\n  enabled: true")
	content = replaceOnce(t, content, "  base_url: https://api.deepseek.com", "  base_url: \"\"")
	content = replaceOnce(t, content, "  model: deepseek-chat", "  model: \"\"")
	for _, field := range []struct{ name, value string }{{"base_url", "https://models.example"}, {"api_key", "test-key"}, {"model", "step-model"}} {
		content = strings.ReplaceAll(content, "      "+field.name+": \"\"", "      "+field.name+": "+field.value)
	}
	if _, err := Load(writeTestConfig(t, content)); err != nil {
		t.Fatalf("complete overrides rejected: %v", err)
	}
}
