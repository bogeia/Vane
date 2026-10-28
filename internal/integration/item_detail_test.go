package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/infra/fetch"
	"github.com/bogeia/vane/internal/service"
)

func TestItemDetailReturnsPublicFieldsAndOnlyPublishedStorySiblings(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Collect.Enabled = false
	cfg.LLM.Enabled = false
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time {
		return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	}, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	ctx := t.Context()
	if _, err := testDB(t, application.Data).ExecContext(ctx, `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('detail-fixture','Detail Fixture','official','https://example.test/feed',false,60)`); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	var storyID int64
	if err := testDB(t, application.Data).QueryRowContext(ctx, `INSERT INTO stories(title,created_at) VALUES('Detail story', $1) RETURNING id`, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)).Scan(&storyID); err != nil {
		t.Fatalf("seed story: %v", err)
	}
	firstPublished := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	targetPublished := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	targetID := insertDetailTestItem(t, ctx, testDB(t, application.Data), storyID, "done", "Original target title", "Written target title", "Target summary", "Target reason", "https://example.test/target", "https://example.test/target.jpg", targetPublished, 70, 90, true, "模型", []string{"OpenAI", "RAG"})
	firstRelatedID := insertDetailTestItem(t, ctx, testDB(t, application.Data), storyID, "done", "Original related one", "Written related one", "Related summary one", "Related reason one", "https://example.test/related-one", "https://example.test/related-one.jpg", firstPublished, 60, 80, false, "论文", []string{"RAG"})
	secondRelatedID := insertDetailTestItem(t, ctx, testDB(t, application.Data), storyID, "done", "Original related two", "Written related two", "Related summary two", "Related reason two", "https://example.test/related-two", "https://example.test/related-two.jpg", firstPublished, 50, 70, false, "行业", []string{"OpenAI"})
	for _, stage := range []string{"new", "prefiltered", "analyzed", "blocked", "failed"} {
		insertDetailTestItem(t, ctx, testDB(t, application.Data), storyID, stage, "Hidden "+stage, "Hidden "+stage, "hidden summary", "hidden reason", "https://example.test/hidden-"+stage, "", targetPublished, 1, 1, false, "模型", []string{})
	}
	if _, err := testDB(t, application.Data).ExecContext(ctx, `UPDATE stories SET representative_item_id=$2 WHERE id=$1`, storyID, targetID); err != nil {
		t.Fatalf("set story representative: %v", err)
	}

	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, path.Join("/api/items", strconv.FormatInt(targetID, 10)), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET item detail returned %d: %s", response.Code, response.Body.String())
	}
	var detail struct {
		service.Item
		RelatedItems []service.Item `json:"related_items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatalf("decode item detail: %v", err)
	}
	want := service.Item{
		ID: strconv.FormatInt(targetID, 10), Title: "Written target title", OriginalTitle: "Original target title",
		Summary: "Target summary", Reason: "Target reason", URL: "https://example.test/target", ImageURL: "https://example.test/target.jpg",
		Source: service.ItemSource{ID: "detail-fixture", Name: "Detail Fixture", Tier: "official"}, PublishedAt: targetPublished,
		Score: 80, Selected: true, Category: "模型", Tags: []string{"OpenAI", "RAG"}, StoryID: strconv.FormatInt(storyID, 10), StoryOthers: 2,
		StoryItemIDs: []string{strconv.FormatInt(targetID, 10), strconv.FormatInt(firstRelatedID, 10), strconv.FormatInt(secondRelatedID, 10)},
	}
	gotItem := detail.Item
	if !gotItem.PublishedAt.Equal(want.PublishedAt) {
		t.Errorf("item publication time = %s, want %s", gotItem.PublishedAt, want.PublishedAt)
	}
	gotItem.PublishedAt = want.PublishedAt
	if !reflect.DeepEqual(gotItem, want) {
		t.Errorf("item detail = %+v, want all public item fields %+v", gotItem, want)
	}
	if len(detail.RelatedItems) != 2 {
		t.Fatalf("related item count = %d, want 2 published siblings: %+v", len(detail.RelatedItems), detail.RelatedItems)
	}
	if detail.RelatedItems[0].ID != strconv.FormatInt(firstRelatedID, 10) || detail.RelatedItems[1].ID != strconv.FormatInt(secondRelatedID, 10) {
		t.Errorf("related items are not ordered by published_at then id: %+v", detail.RelatedItems)
	}
	for _, related := range detail.RelatedItems {
		if related.OriginalTitle == "Original target title" || strings.HasPrefix(related.OriginalTitle, "Hidden ") {
			t.Errorf("related items included self or unpublished material: %+v", related)
		}
	}

	var loneStoryID int64
	if err := testDB(t, application.Data).QueryRowContext(ctx, `INSERT INTO stories(title,created_at) VALUES('Lone detail story', $1) RETURNING id`, targetPublished).Scan(&loneStoryID); err != nil {
		t.Fatalf("seed lone story: %v", err)
	}
	loneID := insertDetailTestItem(t, ctx, testDB(t, application.Data), loneStoryID, "done", "Lone original", "Lone written", "Lone summary", "Lone reason", "https://example.test/lone", "", targetPublished, 40, 60, false, "论文", []string{})
	if _, err := testDB(t, application.Data).ExecContext(ctx, `UPDATE stories SET representative_item_id=$2 WHERE id=$1`, loneStoryID, loneID); err != nil {
		t.Fatalf("set lone story representative: %v", err)
	}
	loneResponse := httptest.NewRecorder()
	application.Router.ServeHTTP(loneResponse, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/items/"+strconv.FormatInt(loneID, 10), nil))
	if loneResponse.Code != http.StatusOK {
		t.Fatalf("GET lone item detail returned %d: %s", loneResponse.Code, loneResponse.Body.String())
	}
	var lone struct {
		RelatedItems []service.Item `json:"related_items"`
	}
	if err := json.NewDecoder(loneResponse.Body).Decode(&lone); err != nil {
		t.Fatalf("decode lone item detail: %v", err)
	}
	if lone.RelatedItems == nil || len(lone.RelatedItems) != 0 {
		t.Errorf("lone item's related_items = %#v, want an empty array", lone.RelatedItems)
	}
}

func TestItemDetailRejectsMissingAndUnpublishedItems(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Collect.Enabled = false
	cfg.LLM.Enabled = false
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time {
		return time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	}, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	ctx := t.Context()
	if _, err := testDB(t, application.Data).ExecContext(ctx, `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('detail-fixture','Detail Fixture','media','https://example.test/feed',false,60)`); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	var storyID int64
	if err := testDB(t, application.Data).QueryRowContext(ctx, `INSERT INTO stories(title) VALUES('Hidden story') RETURNING id`).Scan(&storyID); err != nil {
		t.Fatalf("seed story: %v", err)
	}
	for _, stage := range []string{"new", "prefiltered", "analyzed", "blocked", "failed"} {
		id := insertDetailTestItem(t, ctx, testDB(t, application.Data), storyID, stage, "Hidden "+stage, "Hidden "+stage, "hidden summary", "hidden reason", "https://example.test/hidden-check-"+stage, "", time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC), 1, 1, false, "模型", []string{})
		if stage == "new" {
			if _, err := testDB(t, application.Data).ExecContext(ctx, `UPDATE stories SET representative_item_id=$2 WHERE id=$1`, storyID, id); err != nil {
				t.Fatalf("set story representative: %v", err)
			}
		}
	}
	for _, test := range []struct {
		id         string
		wantStatus int
	}{
		{id: "99999999", wantStatus: http.StatusNotFound},
		{id: "0", wantStatus: http.StatusBadRequest},
		{id: "-1", wantStatus: http.StatusBadRequest},
		{id: "invalid", wantStatus: http.StatusBadRequest},
	} {
		response := httptest.NewRecorder()
		application.Router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/items/"+test.id, nil))
		if response.Code != test.wantStatus {
			t.Errorf("GET item id %q returned %d, want %d", test.id, response.Code, test.wantStatus)
		}
		if response.Code >= 400 && !strings.HasPrefix(response.Header().Get("Content-Type"), "application/problem+json") {
			t.Errorf("GET item id %q error content type = %q, want problem+json", test.id, response.Header().Get("Content-Type"))
		}
	}
	for _, stage := range []string{"new", "prefiltered", "analyzed", "blocked", "failed"} {
		var id string
		if err := testDB(t, application.Data).QueryRowContext(ctx, `SELECT id::text FROM items WHERE original_title=$1`, "Hidden "+stage).Scan(&id); err != nil {
			t.Fatalf("find seeded %s item: %v", stage, err)
		}
		response := httptest.NewRecorder()
		application.Router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/items/"+id, nil))
		if response.Code != http.StatusNotFound || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/problem+json") {
			t.Errorf("GET %s item %s returned %d %q, want 404 problem+json", stage, id, response.Code, response.Header().Get("Content-Type"))
		}
	}
}

func insertDetailTestItem(t *testing.T, ctx context.Context, db *sql.DB, storyID int64, stage, originalTitle, title, summary, reason, itemURL, imageURL string, publishedAt time.Time, score1, score2 int, selected bool, category string, tags []string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(ctx, `INSERT INTO items(source_id,url,original_title,image_url,published_at,stage,score1,score2,title,summary,reason,category,tags,selected,story_id) VALUES('detail-fixture',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, itemURL, originalTitle, imageURL, publishedAt, stage, score1, score2, title, summary, reason, category, tags, selected, storyID).Scan(&id)
	if err != nil {
		t.Fatalf("seed %s item: %v", stage, err)
	}
	return id
}
