package integration

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCategoryFeedFiltersSelectedRepresentativesAndLimitsRSS(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	app := assembleTestApp(t, cfg)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('fixture','Fixture','official','https://example.test/feed',false,60)`); err != nil {
		t.Fatal(err)
	}
	add := func(story int64, title, category string, selected bool, published time.Time) int64 {
		if story == 0 {
			story = insertFoldTestStory(t, t.Context(), testDB(t, app.Data), title, now)
		}
		id := insertFoldTestItem(t, t.Context(), testDB(t, app.Data), story, "done", "fixture", title, title, "研究摘要 & 数据", "推荐理由", "https://example.test/"+strconv.FormatInt(story, 10)+"/"+title, category, []string{}, published, selected)
		if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE stories SET representative_item_id=COALESCE(representative_item_id,$2) WHERE id=$1`, story, id); err != nil {
			t.Fatal(err)
		}
		return story
	}
	paperStory := add(0, "论文代表 & 研究", "论文", true, now)
	add(paperStory, "同事件另一篇论文", "论文", true, now.Add(-time.Minute))
	modelStory := add(0, "模型代表", "模型", true, now)
	add(modelStory, "模型事件中的论文资料", "论文", true, now.Add(-time.Minute))
	add(0, "未入选论文", "论文", false, now)
	for i := range 100 {
		add(0, fmt.Sprintf("论文%d", i), "论文", true, now.Add(-time.Hour))
	}
	response := httptest.NewRecorder()
	app.Router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/feed/category/paper.xml", nil))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body)
	}
	var rss struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title string `xml:"title"`
				GUID  struct {
					Value       string `xml:",chardata"`
					IsPermaLink string `xml:"isPermaLink,attr"`
				} `xml:"guid"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(response.Body.Bytes(), &rss); err != nil {
		t.Fatal(err)
	}
	if rss.XMLName.Local != "rss" || rss.Version != "2.0" || rss.Channel.Title != cfg.Site.Name+" · 精选 · 论文" || len(rss.Channel.Items) != 100 {
		t.Fatalf("rss root/version/title/count=%s/%s/%s/%d", rss.XMLName.Local, rss.Version, rss.Channel.Title, len(rss.Channel.Items))
	}
	seen := map[string]bool{}
	foundRepresentative := false
	for _, item := range rss.Channel.Items {
		if seen[item.GUID.Value] || !strings.HasPrefix(item.GUID.Value, strings.TrimRight(cfg.Site.PublicURL, "/")+"/stories/") || item.GUID.IsPermaLink != "false" {
			t.Fatalf("invalid/duplicate GUID %+v", item.GUID)
		}
		seen[item.GUID.Value] = true
		switch item.Title {
		case "未入选论文", "模型代表", "模型事件中的论文资料", "同事件另一篇论文":
			t.Fatalf("excluded item in category RSS: %s", item.Title)
		case "论文代表 & 研究":
			foundRepresentative = true
		}
	}
	if !foundRepresentative || !seen[strings.TrimRight(cfg.Site.PublicURL, "/")+"/stories/"+strconv.FormatInt(paperStory, 10)] {
		t.Fatal("missing representative story GUID")
	}
}
