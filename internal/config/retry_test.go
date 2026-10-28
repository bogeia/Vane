package config

import (
	"strings"
	"testing"
)

func TestRetryConfigurationBounds(t *testing.T) {
	sample, err := readConfigSample(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ old, value string }{
		{"max_attempts: 4", "max_attempts: 0"},
		{"max_attempts: 4", "max_attempts: 11"},
		{"initial_interval: 1s", "initial_interval: 0s"},
		{"initial_interval: 1s", "initial_interval: 2h"},
		{"max_interval: 16s", "max_interval: 500ms"},
		{"max_interval: 16s", "max_interval: 2h"},
		{"request_timeout: 20s", "request_timeout: 0s"},
		{"request_timeout: 20s", "request_timeout: 6m"},
		{"attempt_timeout: 2m", "attempt_timeout: 0s"},
		{"attempt_timeout: 2m", "attempt_timeout: 31m"},
	} {
		path := writeTestConfig(t, strings.Replace(string(sample), test.old, test.value, 1))
		if _, err := Load(path); err == nil {
			t.Fatalf("accepted %s", test.value)
		}
	}
}
