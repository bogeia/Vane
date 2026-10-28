package config

import (
	"strings"
	"testing"
)

func TestLoadEvaluationSkipsOnlyDatabaseValueValidation(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	content := replaceDatabaseURL(t, string(sample), "''")
	path := writeTestConfig(t, content)
	if _, err := LoadEvaluation(path); err != nil {
		t.Fatalf("evaluation requires database values: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("normal load accepted an empty database URL")
	}
	for _, invalid := range []string{
		replaceDatabaseURL(t, content, ""),
		strings.Replace(content, "  public_url: http://localhost:8080", "  public_url: ''", 1),
		strings.Replace(content, "  enabled: false\n  base_url:", "  enabled: true\n  base_url:", 1),
	} {
		if _, err := LoadEvaluation(writeTestConfig(t, invalid)); err == nil {
			t.Fatal("evaluation accepted a missing field or invalid non-database value")
		}
	}
}
