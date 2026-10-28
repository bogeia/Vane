package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/infra/fetch"
	"github.com/bogeia/vane/internal/service"
)

func TestReportSchedulerPublishesOnceAndReportsAreAvailableOverHTTP(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Collect.Enabled = false
	cfg.LLM.Enabled = false
	now := time.Date(2026, time.October, 22, 8, 5, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	ctx := t.Context()
	seedReportCandidate(t, ctx, testDB(t, application.Data), now.Add(-8*time.Hour))

	if err := application.Reports.RunOnce(ctx); err != nil {
		t.Fatalf("first report scheduler run: %v", err)
	}
	if _, err := testDB(t, application.Data).ExecContext(ctx, `UPDATE items SET title='Changed after first issue' WHERE url='https://example.test/daily-item'`); err != nil {
		t.Fatalf("update candidate after first issue: %v", err)
	}
	const concurrentRuns = 5
	var wait sync.WaitGroup
	for range concurrentRuns {
		wait.Go(func() {
			if err := application.Reports.RunOnce(ctx); err != nil {
				t.Errorf("concurrent report scheduler run: %v", err)
			}
		})
	}
	wait.Wait()

	report := requestDailyReport(t, application.Router, "/api/reports/2026-10-22")
	if report.Date != "2026-10-22" || report.Headline == nil || report.Headline.Title != "Daily headline" {
		t.Fatalf("daily report = %+v, want generated report with seeded headline", report)
	}
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/reports", nil)
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/reports returned %d: %s", response.Code, response.Body.String())
	}
	var list struct {
		Items []struct {
			Date     string `json:"date"`
			Headline string `json:"headline"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
		t.Fatalf("decode report list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Date != "2026-10-22" || list.Items[0].Headline != "Daily headline" {
		t.Fatalf("report list = %+v, want one generated issue", list.Items)
	}
}

func seedReportCandidate(t *testing.T, ctx context.Context, db *sql.DB, processedAt time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('daily-source','Daily Source','official','https://example.test/daily',false,60)`); err != nil {
		t.Fatalf("seed report source: %v", err)
	}
	var storyID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO stories(title,created_at) VALUES('Daily story',$1) RETURNING id`, processedAt).Scan(&storyID); err != nil {
		t.Fatalf("seed report story: %v", err)
	}
	var itemID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO items(source_id,url,original_title,published_at,discovered_at,processed_at,stage,score1,score2,title,summary,reason,category,tags,selected,story_id) VALUES('daily-source','https://example.test/daily-item','Daily original',$1,$2,$2,'done',90,90,'Daily headline','Daily summary','Daily reason','模型',ARRAY['AI'],true,$3) RETURNING id`, processedAt.Add(-time.Hour), processedAt, storyID).Scan(&itemID); err != nil {
		t.Fatalf("seed report item: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE stories SET representative_item_id=$1 WHERE id=$2`, itemID, storyID); err != nil {
		t.Fatalf("set report representative: %v", err)
	}
}

func requestDailyReport(t *testing.T, router http.Handler, path string) service.Report {
	t.Helper()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s returned %d: %s", path, response.Code, response.Body.String())
	}
	var report service.Report
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return report
}
