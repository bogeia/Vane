package logging

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bogeia/vane/internal/config"
)

func TestFormatsAndRotation(t *testing.T) {
	dir := t.TempDir()
	console, err := os.Create(filepath.Join(dir, "console.txt"))
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = console
	t.Cleanup(func() {
		os.Stdout = old
		if err := console.Close(); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(dir, "app.log")
	logger, closeLog, err := New(config.Log{Level: slog.LevelInfo, Console: true, Filename: path, MaxSize: 1, MaxBackups: 2, MaxAge: 1, Compress: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeLog(); err != nil {
			t.Error(err)
		}
	})
	logger = logger.With("app", "test").WithGroup("request").With("id", "123")
	logger.Debug("filtered")
	for range 3 {
		logger.Info("rotation check", "payload", strings.Repeat("x", 400_000))
	}
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "app*.log"))
	if err != nil || len(files) != 2 {
		t.Fatalf("rotation did not produce an active file and backup: %v, %v", files, err)
	}
	lines := 0
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
			var record map[string]any
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatalf("file is not JSON: %v", err)
			}
			request, ok := record["request"].(map[string]any)
			if record["app"] != "test" || !ok || request["id"] != "123" || record["msg"] != "rotation check" {
				t.Fatal("lost structured attributes or failed level filtering")
			}
			lines++
		}
	}
	if lines != 3 {
		t.Fatalf("lost records during rotation: %d", lines)
	}
	if _, err := console.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(console)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	lines = 0
	for scanner.Scan() {
		if !strings.Contains(scanner.Text(), "level=INFO") || !strings.Contains(scanner.Text(), "request.id=123") {
			t.Fatal("console is not text or lost attributes")
		}
		lines++
	}
	if err := scanner.Err(); err != nil || lines != 3 {
		t.Fatalf("console records: %d, error: %v", lines, err)
	}
	before, err := console.Stat()
	if err != nil {
		t.Fatal(err)
	}
	fileOnlyPath := filepath.Join(dir, "file-only.log")
	fileOnly, closeFileOnly, err := New(config.Log{Level: slog.LevelInfo, Filename: fileOnlyPath, MaxSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	fileOnly.Info("file only")
	if err := closeFileOnly(); err != nil {
		t.Fatal(err)
	}
	after, err := console.Stat()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(fileOnlyPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() || !strings.Contains(string(content), `"msg":"file only"`) {
		t.Fatal("console-disabled logger did not write exclusively to its file")
	}
}
