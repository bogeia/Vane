package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunConfigCommandsWithoutDatabase(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"config", "sources"} {
		sample, err := os.ReadFile(filepath.Join("..", "..", "configs", name+".example.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), sample, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(dir, "config.yaml")
	badConfig := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(badConfig, []byte("server: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"serve", "report", "status"} {
		t.Run(name, func(t *testing.T) {
			if err := run([]string{"vane", name}); err == nil {
				t.Fatalf("%s accepted a missing --conf", name)
			}
			if err := run([]string{"vane", name, "--conf", filepath.Join(t.TempDir(), "missing.yaml")}); err == nil {
				t.Fatalf("%s accepted a missing config file", name)
			}
			if err := run([]string{"vane", name, "--conf", badConfig}); err == nil {
				t.Fatalf("%s accepted an invalid config", name)
			}
			if err := run([]string{"vane", name, "--conf", configPath, "extra"}); err == nil {
				t.Fatalf("%s accepted an extra positional argument", name)
			}
		})
	}
	if err := run([]string{"vane", "report", "--conf", configPath}); err == nil {
		t.Fatal("report accepted a missing --date")
	}
	if err := run([]string{"vane", "report", "--conf", configPath, "--date", "not-a-date"}); err == nil {
		t.Fatal("report accepted an invalid date")
	}
}
