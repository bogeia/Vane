package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
)

func TestReportExcludesStoriesFromSevenMostRecentIssues(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	application, _ := newReportTestApp(t, databaseURL)
	ctx := t.Context()
	processedAt := time.Date(2026, time.October, 21, 23, 0, 0, 0, time.UTC)
	recentStory := seedReportEvent(t, ctx, testDB(t, application.Data), "recent-story", "recent-source", "Recent repeat", processedAt)
	olderStory := seedReportEvent(t, ctx, testDB(t, application.Data), "older-story", "older-source", "Outside seven issues", processedAt)

	for day := 14; day <= 21; day++ {
		date := time.Date(2026, time.October, day, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		storyID := olderStory
		if day >= 15 {
			storyID = recentStory
		}
		old := biz.Report{
			Date:       date,
			Sections:   []biz.ReportSection{{Category: "模型", Items: []biz.Item{{ID: "old-" + date, StoryID: storyID}}}},
			Highlights: []biz.Item{}, Briefs: []biz.Item{},
		}
		content, err := reportFixtureJSON(old)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := testDB(t, application.Data).ExecContext(ctx, `INSERT INTO reports(date,content) VALUES($1,$2)`, date, content); err != nil {
			t.Fatalf("seed previous report %s: %v", date, err)
		}
	}
	report, err := application.Reports.Generate(ctx, "2026-10-22")
	if err != nil {
		t.Fatalf("generate report: %v", err)
	}
	if report.Headline == nil || report.Headline.StoryID != olderStory || report.Headline.Title != "Outside seven issues" {
		t.Fatalf("report headline = %+v, want event seen only eight issues back", report.Headline)
	}
	if len(report.Highlights) != 0 || len(report.Briefs) != 0 || len(report.Sections) != 1 || len(report.Sections[0].Items) != 1 {
		t.Fatalf("recently reported event was not excluded: %+v", report)
	}
}

func TestReportListReturnsOnlyTheMostRecentSixtyIssues(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	application, _ := newReportTestApp(t, databaseURL)
	ctx := t.Context()
	base := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	for index := range 61 {
		date := base.AddDate(0, 0, index).Format("2006-01-02")
		report := biz.Report{Date: date, Headline: &biz.Item{Title: "headline " + date}, Highlights: []biz.Item{}, Sections: []biz.ReportSection{}, Briefs: []biz.Item{}}
		content, err := reportFixtureJSON(report)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := testDB(t, application.Data).ExecContext(ctx, `INSERT INTO reports(date,content) VALUES($1,$2)`, date, content); err != nil {
			t.Fatalf("seed issue %s: %v", date, err)
		}
	}
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/reports", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/reports returned %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []biz.ReportSummary `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode report list: %v", err)
	}
	if len(payload.Items) != 60 {
		t.Fatalf("report list returned %d issues, want 60", len(payload.Items))
	}
	if payload.Items[0].Date != base.AddDate(0, 0, 60).Format("2006-01-02") || payload.Items[59].Date != base.AddDate(0, 0, 1).Format("2006-01-02") {
		t.Fatalf("report list endpoints are %q and %q, want latest through second newest", payload.Items[0].Date, payload.Items[59].Date)
	}
}

func TestDailyFeedIsValidRSS(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	application, _ := newReportTestApp(t, databaseURL)
	processedAt := time.Date(2026, time.October, 21, 23, 0, 0, 0, time.UTC)
	seedReportEvent(t, t.Context(), testDB(t, application.Data), "feed-story", "feed-source", "日报头条 & 更新", processedAt)
	if _, err := application.Reports.Generate(t.Context(), "2026-10-22"); err != nil {
		t.Fatalf("generate feed issue: %v", err)
	}
	response := httptest.NewRecorder()
	application.Router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/feed/daily.xml", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /feed/daily.xml returned %d: %s", response.Code, response.Body.String())
	}
	var feed struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel struct {
			Title    string `xml:"title"`
			Language string `xml:"language"`
			Items    []struct {
				Title       string `xml:"title"`
				Description string `xml:"description"`
				Link        string `xml:"link"`
				PubDate     string `xml:"pubDate"`
				GUID        struct {
					Value       string `xml:",chardata"`
					IsPermaLink string `xml:"isPermaLink,attr"`
				} `xml:"guid"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(response.Body.Bytes(), &feed); err != nil {
		t.Fatalf("decode daily RSS: %v", err)
	}
	if feed.Version != "2.0" || feed.Channel.Title != "Vane · 日报" || len(feed.Channel.Items) != 1 || feed.Channel.Items[0].Title != "Vane 日报 · 2026-10-22" || !strings.Contains(feed.Channel.Items[0].Description, "日报头条 & 更新") {
		t.Fatalf("daily RSS = %+v, want RSS 2.0 containing the report headline", feed)
	}
	if feed.Channel.Language != "zh-CN" {
		t.Errorf("daily RSS language = %q, want zh-CN", feed.Channel.Language)
	}
	wantLink := "http://localhost:8080/reports/2026-10-22"
	item := feed.Channel.Items[0]
	if item.Link != wantLink || item.GUID.Value != wantLink || item.GUID.IsPermaLink != "true" {
		t.Errorf("daily RSS item link/GUID = %q/%+v, want %q with isPermaLink=true", item.Link, item.GUID, wantLink)
	}
	gotPubDate, err := time.Parse(time.RFC1123Z, item.PubDate)
	if err != nil {
		t.Fatalf("parse daily RSS pubDate %q: %v", item.PubDate, err)
	}
	if want := time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC); !gotPubDate.Equal(want) {
		t.Errorf("daily RSS pubDate = %s, want instant %s", gotPubDate, want)
	}
	if _, err := testDB(t, application.Data).ExecContext(t.Context(), `DELETE FROM reports`); err != nil {
		t.Fatalf("remove reports for empty-feed check: %v", err)
	}
	emptyResponse := httptest.NewRecorder()
	application.Router.ServeHTTP(emptyResponse, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/feed/daily.xml", nil))
	if emptyResponse.Code != http.StatusOK {
		t.Fatalf("GET empty /feed/daily.xml returned %d: %s", emptyResponse.Code, emptyResponse.Body.String())
	}
	var emptyFeed struct {
		Channel struct {
			Language string     `xml:"language"`
			Items    []struct{} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(emptyResponse.Body.Bytes(), &emptyFeed); err != nil {
		t.Fatalf("decode empty daily RSS: %v", err)
	}
	if emptyFeed.Channel.Language != "zh-CN" || len(emptyFeed.Channel.Items) != 0 {
		t.Errorf("empty daily RSS language/items = %q/%d, want zh-CN/0", emptyFeed.Channel.Language, len(emptyFeed.Channel.Items))
	}
}

func newReportTestApp(t *testing.T, databaseURL string) (*testApp, config.Config) {
	t.Helper()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	cfg.Collect.Enabled = false
	cfg.LLM.Enabled = false
	return assembleTestApp(t, cfg), cfg
}

func seedReportEvent(t *testing.T, ctx context.Context, db *sql.DB, storyID, sourceID, title string, processedAt time.Time) string {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES($1,$1,'official',$2,false,60) ON CONFLICT(id) DO NOTHING`, sourceID, "https://example.test/"+sourceID); err != nil {
		t.Fatalf("seed report source: %v", err)
	}
	var story int64
	if err := db.QueryRowContext(ctx, `INSERT INTO stories(title,created_at) VALUES($1,$2) RETURNING id`, title, processedAt).Scan(&story); err != nil {
		t.Fatalf("seed report story: %v", err)
	}
	var item int64
	if err := db.QueryRowContext(ctx, `INSERT INTO items(source_id,url,original_title,published_at,discovered_at,processed_at,stage,score1,score2,title,summary,reason,category,tags,selected,story_id) VALUES($1,$2,$3,$4,$5,$5,'done',90,90,$3,'Summary','Reason','模型',ARRAY['AI'],true,$6) RETURNING id`, sourceID, "https://example.test/report/"+storyID, title, processedAt.Add(-time.Hour), processedAt, story).Scan(&item); err != nil {
		t.Fatalf("seed report item: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE stories SET representative_item_id=$1 WHERE id=$2`, item, story); err != nil {
		t.Fatalf("set report representative: %v", err)
	}
	return strconv.FormatInt(story, 10)
}
