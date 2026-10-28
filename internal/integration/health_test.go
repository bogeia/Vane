package integration

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
)

func TestHealthz(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	cfg := loadTestConfig(t, databaseURL)
	resetTestSchema(t, databaseURL)
	application := assembleTestApp(t, cfg)

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /healthz returned %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
}

func TestServeInitializesSourcesAndMigrations(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	cfg := loadTestConfig(t, databaseURL)
	resetTestSchema(t, databaseURL)
	application := assembleTestApp(t, cfg)

	var sourceCount int
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), "SELECT count(*) FROM sources").Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if sourceCount != 28 {
		t.Fatalf("sources count = %d, want 28", sourceCount)
	}

	if err := data.Migrate(t.Context(), application.Data); err != nil {
		t.Fatalf("re-running migrations failed: %v", err)
	}
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), "SELECT count(*) FROM sources").Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if sourceCount != 28 {
		t.Fatalf("sources count after re-running migrations = %d, want 28", sourceCount)
	}

	removedSource := cfg.Sources[len(cfg.Sources)-1]
	remainingSources := cfg.Sources[:len(cfg.Sources)-1]
	if err := data.SyncSources(t.Context(), application.Data, remainingSources); err != nil {
		t.Fatalf("syncing reduced source list failed: %v", err)
	}
	var enabled bool
	if err := testDB(t, application.Data).QueryRowContext(t.Context(), "SELECT enabled FROM sources WHERE id = $1", removedSource.ID).Scan(&enabled); err != nil {
		t.Fatalf("removed source history was deleted: %v", err)
	}
	if enabled {
		t.Fatalf("removed source %q remains enabled", removedSource.ID)
	}
}

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	databaseURL := os.Getenv("VANE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("VANE_TEST_DATABASE_URL is not set")
	}
	if err := validateTestDatabaseURL(databaseURL); err != nil {
		t.Fatal(err)
	}
	return databaseURL
}

func loadTestConfig(t *testing.T, databaseURL string) config.Config {
	t.Helper()
	cfg, err := config.Load(copyExampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.URL = databaseURL
	cfg.LLM.Retry.InitialInterval = time.Millisecond
	cfg.LLM.Retry.MaxInterval = 16 * time.Millisecond
	cfg.Fetch.Retry.InitialInterval = time.Millisecond
	cfg.Fetch.Retry.MaxInterval = 16 * time.Millisecond
	return cfg
}

func assembleTestApp(t *testing.T, cfg config.Config) *testApp {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	application, cleanup, err := newTestApp(t.Context(), cfg, logger, func() time.Time {
		return time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC)
	})
	if err != nil {
		dsn := cfg.Database.URL
		t.Fatalf("assemble app: %s", strings.ReplaceAll(err.Error(), dsn, "[redacted]"))
	}
	t.Cleanup(cleanup)
	return application
}

func resetTestSchema(t *testing.T, databaseURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	if err := resetTestSchemaContext(ctx, databaseURL); err != nil {
		t.Fatal(strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
}

func resetTestSchemaContext(ctx context.Context, databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open test database")
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin test schema transaction: %w", err)
	}
	defer tx.Rollback()
	var database string
	if err := tx.QueryRowContext(ctx, "SELECT current_database()").Scan(&database); err != nil {
		return fmt.Errorf("check test database: %w", err)
	}
	if database != "vane_test" {
		return fmt.Errorf("refusing to reset schema in database %q", database)
	}
	if _, err := tx.ExecContext(ctx, "DROP SCHEMA IF EXISTS public CASCADE"); err != nil {
		return fmt.Errorf("drop test schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA public"); err != nil {
		return fmt.Errorf("create test schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit test schema transaction: %w", err)
	}
	return nil
}

func TestValidateTestDatabaseURL(t *testing.T) {
	for _, test := range []struct {
		name string
		url  string
		want bool
	}{
		{"test database", "postgres://localhost/vane_test", true},
		{"other database", "postgres://localhost/vane", false},
		{"dbname override", "postgres://localhost/vane_test?dbname=vane", false},
		{"missing database", "postgres://localhost/", false},
		{"malformed URL", "://invalid", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateTestDatabaseURL(test.url)
			if (err == nil) != test.want {
				t.Fatalf("validateTestDatabaseURL(%q) error = %v, want valid=%v", test.url, err, test.want)
			}
		})
	}
}

func validateTestDatabaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" || parsed.Path != "/vane_test" {
		return fmt.Errorf("VANE_TEST_DATABASE_URL must target the vane_test database")
	}
	config, err := pgx.ParseConfig(raw)
	if err != nil || config.Database != "vane_test" {
		return fmt.Errorf("VANE_TEST_DATABASE_URL must target the vane_test database")
	}
	return nil
}

func copyExampleConfig(t *testing.T) string {
	t.Helper()
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
	return filepath.Join(dir, "config.yaml")
}
