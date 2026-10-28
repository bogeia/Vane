package integration

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/service"
)

func TestStatusCommandOutputsSourcesAndStages(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	cfg := loadTestConfig(t, databaseURL)
	resetTestSchema(t, databaseURL)
	app := assembleTestApp(t, cfg)
	blockedAt := time.Now().UTC().Truncate(time.Second)
	result := biz.NewCollectionResult(biz.CollectSource{ID: cfg.Sources[0].ID, IntervalMinutes: cfg.Sources[0].IntervalMinutes}, biz.Feed{}, blockedAt, "")
	result.Blocked = []biz.BlockedContent{{URL: "https://example.test/blocked-cli", OriginalTitle: "Blocked CLI title", FirstBlockedAt: blockedAt}}
	if err := data.NewCollectionRepo(app.Data).SaveCollection(t.Context(), result, nil); err != nil {
		t.Fatal(err)
	}
	configPath := writeSubprocessConfig(t, databaseURL, "127.0.0.1:8080", cfg.Web.Dir)
	binaryPath := buildVane(t)

	command := exec.CommandContext(t.Context(), binaryPath, "status", "--conf", configPath)
	output, err := command.Output()
	if err != nil {
		t.Fatal("status command failed (output suppressed to protect database credentials)")
	}
	var status struct {
		Sources []struct {
			ID     string `json:"id"`
			Health struct {
				NewBlocked int64 `json:"new_blocked"`
			} `json:"health"`
		} `json:"sources"`
		RecentBlocked []struct {
			SourceID       string    `json:"source_id"`
			URL            string    `json:"url"`
			OriginalTitle  string    `json:"original_title"`
			FirstBlockedAt time.Time `json:"first_blocked_at"`
		} `json:"recent_blocked"`
		Stages map[string]int64 `json:"stages"`
	}
	if err := json.Unmarshal(output, &status); err != nil {
		t.Fatalf("status command did not output JSON: %v", err)
	}
	if len(status.Sources) != 28 {
		t.Fatalf("status source count = %d, want 28", len(status.Sources))
	}
	var newBlocked int64
	for _, source := range status.Sources {
		if source.ID == cfg.Sources[0].ID {
			newBlocked = source.Health.NewBlocked
		}
	}
	if newBlocked != 1 || len(status.RecentBlocked) != 1 {
		t.Fatalf("blocked count=%d recent=%+v", newBlocked, status.RecentBlocked)
	}
	blocked := status.RecentBlocked[0]
	if blocked.SourceID != cfg.Sources[0].ID || blocked.URL != "https://example.test/blocked-cli" || blocked.OriginalTitle != "Blocked CLI title" || !blocked.FirstBlockedAt.Equal(blockedAt) {
		t.Fatalf("blocked=%+v", blocked)
	}
	if len(status.Stages) != 6 {
		t.Fatalf("status stage count = %d, want 6", len(status.Stages))
	}
	for _, stage := range []string{"new", "prefiltered", "analyzed", "done", "blocked", "failed"} {
		if count, ok := status.Stages[stage]; !ok || count != 0 {
			t.Errorf("status stage %q = %d, present=%v; want 0", stage, count, ok)
		}
	}
}

func TestReportCommandOverwritesRequestedDate(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	application := assembleTestApp(t, cfg)
	processedAt := time.Date(2026, time.October, 21, 23, 0, 0, 0, time.UTC)
	seedReportCandidate(t, t.Context(), testDB(t, application.Data), processedAt)

	if _, err := application.Reports.Generate(t.Context(), "2026-10-22"); err != nil {
		t.Fatalf("generate initial report: %v", err)
	}
	configPath := writeSubprocessConfig(t, databaseURL, "127.0.0.1:8080", cfg.Web.Dir)
	binaryPath := buildVane(t)
	for _, dateFlag := range []string{"-date", "--date"} {
		t.Run(dateFlag, func(t *testing.T) {
			wantTitle := "Overwritten by " + dateFlag
			if _, err := testDB(t, application.Data).ExecContext(t.Context(), `UPDATE items SET title=$1 WHERE url='https://example.test/daily-item'`, wantTitle); err != nil {
				t.Fatalf("update source material: %v", err)
			}
			command := exec.CommandContext(t.Context(), binaryPath, "report", "--conf", configPath, dateFlag, "2026-10-22")
			output, err := command.Output()
			if err != nil {
				t.Fatal("report command failed (output suppressed to protect database credentials)")
			}
			var report service.Report
			if err := json.Unmarshal(output, &report); err != nil {
				t.Fatalf("report command did not output JSON: %v", err)
			}
			if report.Date != "2026-10-22" || report.Headline == nil || report.Headline.Title != wantTitle {
				t.Fatalf("report command output = %+v, want overwritten requested date", report)
			}
		})
	}
}

func TestServeProcessWithBuiltFrontend(t *testing.T) {
	webDir := filepath.Join("..", "..", "web", "dist")
	if _, err := os.Stat(filepath.Join(webDir, "index.html")); err != nil {
		t.Skip("web/dist/index.html is unavailable; run npm run build --prefix web")
	}
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	address := freeAddress(t)
	configPath := writeSubprocessConfig(t, databaseURL, address, webDir)
	binaryPath := buildVane(t)
	command := exec.CommandContext(t.Context(), binaryPath, "serve", "--conf", configPath)
	if err := command.Start(); err != nil {
		t.Fatal("start serve process")
	}
	waitForServe(t, command, address)
}

func writeSubprocessConfig(t *testing.T, databaseURL, address, webDir string) string {
	t.Helper()
	path := copyExampleConfig(t)
	settings := viper.New()
	settings.SetConfigFile(path)
	if err := settings.ReadInConfig(); err != nil {
		t.Fatal("read sanitized example config")
	}
	settings.Set("database.url", databaseURL)
	settings.Set("server.address", address)
	settings.Set("log.filename", filepath.Join(t.TempDir(), "vane.log"))
	settings.Set("web.dir", webDir)
	output := filepath.Join(filepath.Dir(path), "integration.yaml")
	if err := settings.WriteConfigAs(output); err != nil {
		t.Fatal("write temporary integration config")
	}
	if err := os.Chmod(output, 0o600); err != nil {
		t.Fatal("restrict temporary integration config permissions")
	}
	return output
}

func buildVane(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vane")
	command := exec.CommandContext(t.Context(), "go", "build", "-o", path, "../../cmd/vane")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build vane command: %s", output)
	}
	return path
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve local address")
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal("release local address")
	}
	return address
}

func waitForServe(t *testing.T, command *exec.Cmd, address string) {
	t.Helper()
	started := time.Now()
	deadline := started.Add(time.Minute)
	if testDeadline, ok := t.Deadline(); ok && testDeadline.Add(-5*time.Second).Before(deadline) {
		deadline = testDeadline.Add(-5 * time.Second)
	}
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	var processErr error
	done := make(chan struct{})
	go func() {
		processErr = command.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-done
	})
	baseURL := "http://" + address
	client := &http.Client{Timeout: time.Second}
	paths := []struct {
		path       string
		status     int
		bodyHas    string
		contentHas string
	}{
		{"/", http.StatusOK, "Vane", ""},
		{"/items/1", http.StatusOK, "Vane", ""},
		{"/api/不存在", http.StatusNotFound, "", "application/problem+json"},
		{"/docs", http.StatusOK, "", ""},
		{"/openapi.json", http.StatusOK, "", ""},
		{"/healthz", http.StatusOK, "", ""},
	}
	allReady := false
	// Allow real database migration I/O, within one minute and the test shutdown reserve.
	for {
		allReady = true
		for _, test := range paths {
			response, err := client.Get(baseURL + test.path)
			if err != nil {
				allReady = false
				break
			}
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil || response.StatusCode != test.status || !strings.Contains(string(body), test.bodyHas) || (test.contentHas != "" && !strings.Contains(response.Header.Get("Content-Type"), test.contentHas)) {
				t.Fatalf("GET %s returned unexpected HTTP response", test.path)
			}
		}
		if allReady {
			break
		}
		select {
		case <-done:
			t.Fatalf("serve process exited before HTTP endpoints were ready after %s: %v", time.Since(started), processErr)
		case <-ctx.Done():
			t.Fatalf("serve process did not expose HTTP endpoints within test startup budget (%s): %v", time.Since(started), ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
	t.Logf("serve HTTP endpoints ready after %s", time.Since(started))
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal("send graceful shutdown signal")
	}
	select {
	case <-done:
		if processErr != nil {
			t.Fatalf("serve process did not stop cleanly: %v", processErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve process did not stop after interrupt")
	}
}

func TestServeProcessUsesReservationAndReleasesIt(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html><body>Vane</body></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	address := freeAddress(t)
	configPath := writeSubprocessConfig(t, databaseURL, address, webDir)
	command := exec.CommandContext(t.Context(), buildVane(t), "serve", "--conf", configPath)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waitForServe(t, command, address)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("serve leaked its reserved port: %v", err)
	}
	listener.Close()
}
