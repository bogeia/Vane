package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/infra/fetch"
	"github.com/bogeia/vane/internal/service"
)

func TestSelectedViewFoldsStoriesBeforeFilteringAndPaging(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Collect.Enabled = false
	cfg.LLM.Enabled = false
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	application, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, data.NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}), nil))
	if err != nil {
		t.Fatalf("assemble application: %s", strings.ReplaceAll(err.Error(), databaseURL, "[redacted]"))
	}
	t.Cleanup(cleanup)
	ctx := t.Context()
	for _, source := range []struct {
		id, name, tier string
	}{
		{"official", "Official", "official"},
		{"media-one", "Media One", "media"},
		{"media-two", "Media Two", "media"},
	} {
		if _, err := testDB(t, application.Data).ExecContext(ctx, `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES($1,$2,$3,$4,false,60)`, source.id, source.name, source.tier, "https://example.test/"+source.id); err != nil {
			t.Fatalf("seed source %s: %v", source.id, err)
		}
	}

	sharedStoryID := insertFoldTestStory(t, ctx, testDB(t, application.Data), "Shared event", now)
	sharedAt := now.Add(-time.Hour)
	representativeID := insertFoldTestItem(t, ctx, testDB(t, application.Data), sharedStoryID, "done", "official", "Official original", "RepresentativeKeyword", "Official summary", "Official reason", "https://example.test/official", "模型", []string{"OpenAI"}, sharedAt, false)
	insertFoldTestItem(t, ctx, testDB(t, application.Data), sharedStoryID, "done", "media-one", "SiblingOnlyKeyword", "SiblingOnlyKeyword", "Sibling summary one", "Sibling reason one", "https://example.test/media-one", "论文", []string{"RAG"}, sharedAt.Add(time.Minute), true)
	insertFoldTestItem(t, ctx, testDB(t, application.Data), sharedStoryID, "done", "media-two", "Second sibling original", "Second sibling title", "Sibling summary two", "Sibling reason two", "https://example.test/media-two", "行业", []string{"OpenAI"}, sharedAt.Add(2*time.Minute), true)
	if _, err := testDB(t, application.Data).ExecContext(ctx, `UPDATE stories SET representative_item_id=$2 WHERE id=$1`, sharedStoryID, representativeID); err != nil {
		t.Fatalf("set shared story representative: %v", err)
	}
	for index := range 3 {
		storyID := insertFoldTestStory(t, ctx, testDB(t, application.Data), "Pagination story", now)
		itemID := insertFoldTestItem(t, ctx, testDB(t, application.Data), storyID, "done", "media-one", "Pagination original "+strconv.Itoa(index), "Pagination event "+strconv.Itoa(index), "Pagination summary", "Pagination reason", "https://example.test/pagination/"+strconv.Itoa(index), "产品", []string{"AI"}, sharedAt, true)
		if _, err := testDB(t, application.Data).ExecContext(ctx, `UPDATE stories SET representative_item_id=$2 WHERE id=$1`, storyID, itemID); err != nil {
			t.Fatalf("set pagination representative: %v", err)
		}
	}
	for _, stage := range []string{"new", "prefiltered", "analyzed", "blocked", "failed"} {
		insertFoldTestItem(t, ctx, testDB(t, application.Data), sharedStoryID, stage, "media-two", "Hidden "+stage, "Hidden "+stage, "hidden", "hidden", "https://example.test/hidden/"+stage, "教程", []string{}, sharedAt, true)
	}

	selectedResponse := requestFoldItems(t, application.Router, "/api/items?view=selected")
	var selected struct {
		Items []service.Item `json:"items"`
	}
	if err := json.Unmarshal(selectedResponse, &selected); err != nil {
		t.Fatalf("decode selected items: %v", err)
	}
	if len(selected.Items) != 4 {
		t.Fatalf("selected event count = %d, want shared event plus three pagination stories: %+v", len(selected.Items), selected.Items)
	}
	var sharedRepresentative *service.Item
	for index := range selected.Items {
		if selected.Items[index].StoryID == strconv.FormatInt(sharedStoryID, 10) {
			sharedRepresentative = &selected.Items[index]
		}
	}
	if sharedRepresentative == nil || sharedRepresentative.ID != strconv.FormatInt(representativeID, 10) || sharedRepresentative.Selected || sharedRepresentative.StoryOthers != 2 {
		t.Fatalf("selected shared event = %+v, want unselected official representative with story_others=2", sharedRepresentative)
	}

	allResponse := requestFoldItems(t, application.Router, "/api/items?view=all")
	var all struct {
		Items []service.Item `json:"items"`
	}
	if err := json.Unmarshal(allResponse, &all); err != nil {
		t.Fatalf("decode all items: %v", err)
	}
	sharedCount := 0
	for _, item := range all.Items {
		if item.StoryID == strconv.FormatInt(sharedStoryID, 10) {
			sharedCount++
		}
	}
	if sharedCount != 3 {
		t.Errorf("view=all shared-story item count = %d, want all three done items", sharedCount)
	}

	for _, test := range []struct {
		query string
		want  int
	}{
		{query: "category=模型", want: 1},
		{query: "category=论文", want: 0},
		{query: "q=RepresentativeKeyword", want: 1},
		{query: "q=SiblingOnlyKeyword", want: 0},
	} {
		response := requestFoldItems(t, application.Router, "/api/items?view=selected&"+test.query)
		var page struct {
			Items []service.Item `json:"items"`
		}
		if err := json.Unmarshal(response, &page); err != nil {
			t.Fatalf("decode selected filter %s: %v", test.query, err)
		}
		if len(page.Items) != test.want {
			t.Errorf("selected %s returned %d items, want %d: %+v", test.query, len(page.Items), test.want, page.Items)
		}
	}

	cursor := ""
	seen := map[string]bool{}
	for range 3 {
		endpoint := "/api/items?view=selected&limit=1&q=" + url.QueryEscape("Pagination event")
		if cursor != "" {
			endpoint += "&cursor=" + url.QueryEscape(cursor)
		}
		response := httptest.NewRecorder()
		application.Router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s returned %d: %s", endpoint, response.Code, response.Body.String())
		}
		var page struct {
			Items      []service.Item `json:"items"`
			NextCursor string         `json:"next_cursor"`
		}
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatalf("decode folded page: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("folded page returned %d items, want one", len(page.Items))
		}
		if seen[page.Items[0].StoryID] {
			t.Fatalf("folded page repeated story %s", page.Items[0].StoryID)
		}
		seen[page.Items[0].StoryID] = true
		cursor = page.NextCursor
		if cursor == "" && len(seen) != 3 {
			t.Fatalf("cursor ended after %d pagination stories, want 3", len(seen))
		}
	}
	if len(seen) != 3 {
		t.Fatalf("folded cursor returned %d unique stories, want 3", len(seen))
	}
	if cursor != "" {
		t.Fatalf("last folded cursor unexpectedly has value %q", cursor)
	}

	rss := httptest.NewRecorder()
	application.Router.ServeHTTP(rss, httptest.NewRequestWithContext(ctx, http.MethodGet, "/feed.xml", nil))
	if rss.Code != http.StatusOK {
		t.Fatalf("GET /feed.xml returned %d: %s", rss.Code, rss.Body.String())
	}
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Channel struct {
			Language string `xml:"language"`
			Items    []struct {
				Link    string `xml:"link"`
				PubDate string `xml:"pubDate"`
				GUID    struct {
					Value       string `xml:",chardata"`
					IsPermaLink string `xml:"isPermaLink,attr"`
				} `xml:"guid"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(rss.Body.Bytes(), &feed); err != nil {
		t.Fatalf("decode /feed.xml: %v", err)
	}
	if feed.XMLName.Local != "rss" || len(feed.Channel.Items) != 4 {
		t.Errorf("selected RSS contains %d items, want one per 4 selected events", len(feed.Channel.Items))
	}
	var representativeLinks, hiddenSiblingLinks int
	for _, item := range feed.Channel.Items {
		if item.Link == "https://example.test/official" {
			representativeLinks++
		}
		if item.Link == "https://example.test/media-one" || item.Link == "https://example.test/media-two" {
			hiddenSiblingLinks++
		}
	}
	if representativeLinks != 1 || hiddenSiblingLinks != 0 {
		t.Errorf("selected RSS representative count=%d sibling count=%d; want only the story representative", representativeLinks, hiddenSiblingLinks)
	}
	if feed.Channel.Language != "zh-CN" {
		t.Errorf("selected RSS language = %q, want zh-CN", feed.Channel.Language)
	}
	wantGUID := strings.TrimRight(cfg.Site.PublicURL, "/") + "/stories/" + strconv.FormatInt(sharedStoryID, 10)
	for _, item := range feed.Channel.Items {
		if item.Link != "https://example.test/official" {
			continue
		}
		if item.GUID.Value != wantGUID || item.GUID.IsPermaLink != "false" {
			t.Errorf("selected RSS GUID = %+v, want value %q with isPermaLink=false", item.GUID, wantGUID)
		}
		gotPubDate, err := time.Parse(time.RFC1123Z, item.PubDate)
		if err != nil {
			t.Errorf("parse selected RSS pubDate %q: %v", item.PubDate, err)
		} else if !gotPubDate.Equal(sharedAt) {
			t.Errorf("selected RSS pubDate = %s, want instant %s", gotPubDate, sharedAt)
		}
	}
}

func TestSelectedViewCountsSameSourceArticlesAsStoryOthers(t *testing.T) {
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
	if _, err := testDB(t, application.Data).ExecContext(ctx, `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('same-source','Single Source','media','https://example.test/same-source',false,60)`); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	storyID := insertFoldTestStory(t, ctx, testDB(t, application.Data), "Same source event", now)
	published := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	representativeID := insertFoldTestItem(t, ctx, testDB(t, application.Data), storyID, "done", "same-source", "Original one", "Representative one", "Summary one", "Reason one", "https://example.test/same-source/one", "论文", []string{"AI"}, published, true)
	insertFoldTestItem(t, ctx, testDB(t, application.Data), storyID, "done", "same-source", "Original two", "Representative two", "Summary two", "Reason two", "https://example.test/same-source/two", "论文", []string{"AI"}, published.Add(time.Minute), true)
	insertFoldTestItem(t, ctx, testDB(t, application.Data), storyID, "done", "same-source", "Original three", "Representative three", "Summary three", "Reason three", "https://example.test/same-source/three", "论文", []string{"AI"}, published.Add(2*time.Minute), true)
	if _, err := testDB(t, application.Data).ExecContext(ctx, `UPDATE stories SET representative_item_id=$2 WHERE id=$1`, storyID, representativeID); err != nil {
		t.Fatalf("set story representative: %v", err)
	}
	selected := requestFoldItems(t, application.Router, "/api/items?view=selected")
	var selectedPage struct {
		Items []service.Item `json:"items"`
	}
	if err := json.Unmarshal(selected, &selectedPage); err != nil {
		t.Fatalf("decode selected items: %v", err)
	}
	if len(selectedPage.Items) != 1 || selectedPage.Items[0].ID != strconv.FormatInt(representativeID, 10) || selectedPage.Items[0].StoryOthers != 2 {
		t.Fatalf("selected same-source event = %+v, want one representative and two other articles", selectedPage.Items)
	}
	all := requestFoldItems(t, application.Router, "/api/items?view=all")
	var allPage struct {
		Items []service.Item `json:"items"`
	}
	if err := json.Unmarshal(all, &allPage); err != nil {
		t.Fatalf("decode all items: %v", err)
	}
	if len(allPage.Items) != 3 {
		t.Errorf("view=all returned %d same-source items, want 3", len(allPage.Items))
	}
}

func requestFoldItems(t *testing.T, router http.Handler, endpoint string) []byte {
	t.Helper()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s returned %d: %s", endpoint, response.Code, response.Body.String())
	}
	return response.Body.Bytes()
}

func insertFoldTestStory(t *testing.T, ctx context.Context, db *sql.DB, title string, createdAt time.Time) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(ctx, `INSERT INTO stories(title,created_at) VALUES($1,$2) RETURNING id`, title, createdAt).Scan(&id); err != nil {
		t.Fatalf("seed story %q: %v", title, err)
	}
	return id
}

func insertFoldTestItem(t *testing.T, ctx context.Context, db *sql.DB, storyID int64, stage, sourceID, originalTitle, title, summary, reason, itemURL, category string, tags []string, publishedAt time.Time, selected bool) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(ctx, `INSERT INTO items(source_id,url,original_title,published_at,stage,score1,score2,title,summary,reason,category,tags,selected,story_id) VALUES($1,$2,$3,$4,$5,80,90,$6,$7,$8,$9,$10,$11,$12) RETURNING id`, sourceID, itemURL, originalTitle, publishedAt, stage, title, summary, reason, category, tags, selected, storyID).Scan(&id)
	if err != nil {
		t.Fatalf("seed item %q: %v", originalTitle, err)
	}
	return id
}
