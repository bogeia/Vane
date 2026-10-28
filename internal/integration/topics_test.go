package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestTopicsAPICombinesFiltersAfterStoryFoldingAndCountsSelectedItems(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	app := assembleTestApp(t, cfg)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for _, source := range []string{"official", "media"} {
		if _, err := testDB(t, app.Data).ExecContext(t.Context(), `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES($1,$1,'official','https://example.test/feed',false,60)`, source); err != nil {
			t.Fatal(err)
		}
	}
	add := func(story int64, name, category, stage string, selected bool, topics []string) int64 {
		if story == 0 {
			story = insertFoldTestStory(t, t.Context(), testDB(t, app.Data), name, now)
		}
		id := insertFoldTestItem(t, t.Context(), testDB(t, app.Data), story, stage, "official", name, name, "主题摘要", "推荐理由", "https://example.test/"+name, category, []string{"Agent"}, now, selected)
		if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE items SET topics=$2 WHERE id=$1`, id, topics); err != nil {
			t.Fatal(err)
		}
		if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE stories SET representative_item_id=COALESCE(representative_item_id,$2) WHERE id=$1`, story, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	storyA := insertFoldTestStory(t, t.Context(), testDB(t, app.Data), "representative outside topic", now)
	add(storyA, "OfficialOutside", "模型", "done", false, []string{"model"})
	add(storyA, "SiblingOnlyKeyword", "论文", "done", true, []string{"agent", "paper"})
	storyB := insertFoldTestStory(t, t.Context(), testDB(t, app.Data), "representative inside topic", now)
	representative := add(storyB, "RepresentativeKeyword", "论文", "done", true, []string{"agent", "paper"})
	add(storyB, "MemberModel", "模型", "done", true, []string{"agent", "model"})
	add(0, "UnselectedAgent", "论文", "done", false, []string{"agent", "paper"})
	roundup := add(0, "RoundupAgent", "论文", "done", false, []string{"agent", "paper"})
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), `UPDATE items SET roundup=true WHERE id=$1`, roundup); err != nil {
		t.Fatal(err)
	}
	add(0, "HiddenAgent", "论文", "analyzed", true, []string{"agent", "paper"})
	add(0, "OldWithoutBackfill", "论文", "done", true, []string{})
	for i := range 25 {
		add(0, fmt.Sprintf("代理主题动态%d", i), "论文", "done", true, []string{"agent", "paper"})
	}
	for _, tc := range []struct {
		query string
		count int
	}{
		{"view=all&topic=agent&limit=100", 30},
		{"view=selected&topic=agent&limit=100", 26},
		{"view=selected&topic=agent&q=SiblingOnlyKeyword", 0},
		{"view=selected&topic=agent&category=模型", 0},
		{"view=all&topic=agent&category=模型", 1},
		{"view=selected&topic=agent&category=论文&q=RepresentativeKeyword", 1},
	} {
		t.Run(tc.query, func(t *testing.T) {
			response := requestFoldItems(t, app.Router, "/api/items?"+tc.query)
			var page itemPageResponse
			if err := json.Unmarshal(response, &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != tc.count {
				t.Fatalf("items=%d want %d", len(page.Items), tc.count)
			}
			if tc.count == 1 && tc.query == "view=selected&topic=agent&category=论文&q=RepresentativeKeyword" && page.Items[0].ID != strconv.FormatInt(representative, 10) {
				t.Fatal("selected filter returned a member rather than representative")
			}
		})
	}
	response := httptest.NewRecorder()
	app.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/items?topic=unknown", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid topic status=%d", response.Code)
	}
	response = httptest.NewRecorder()
	app.Router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/topics", nil))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("topics status/cache=%d/%s", response.Code, response.Header().Get("Cache-Control"))
	}
	var catalog struct {
		Topics []struct {
			Slug, Name, Group, Description string
			SelectedCount                  int64  `json:"selected_count"`
			AllCount                       *int64 `json:"all_count"`
		} `json:"topics"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for i, topic := range catalog.Topics {
		slugs = append(slugs, topic.Slug)
		group := "company"
		if i >= 15 {
			group = "field"
		}
		if i >= 30 {
			group = "genre"
		}
		if topic.Name == "" || topic.Description == "" || topic.Group != group {
			t.Fatalf("invalid topic %+v", topic)
		}
		want := int64(0)
		switch topic.Slug {
		case "agent":
			want = 28
		case "paper":
			want = 27
		case "model":
			want = 1
		}
		var all itemPageResponse
		if err := json.Unmarshal(requestFoldItems(t, app.Router, "/api/items?view=all&limit=100&topic="+topic.Slug), &all); err != nil {
			t.Fatal(err)
		}
		if topic.AllCount == nil || *topic.AllCount != int64(len(all.Items)) {
			t.Fatalf("topic %s all_count=%v want %d", topic.Slug, topic.AllCount, len(all.Items))
		}
		if topic.SelectedCount != want {
			t.Fatalf("topic %s count=%d want %d", topic.Slug, topic.SelectedCount, want)
		}
	}
	if !slices.Equal(slugs, []string{"openai", "anthropic", "google", "deepseek", "qwen", "kimi", "minimax", "zhipu", "xai", "meta", "microsoft", "nvidia", "hugging-face", "cursor", "openrouter", "agent", "coding", "reasoning", "multimodal", "voice", "video", "image-gen", "rag", "on-device", "data-training", "search", "engineering", "open-source", "embodied", "mcp", "model", "product", "industry", "paper", "tutorial", "opinion"}) {
		t.Fatalf("topics/order=%v", slugs)
	}
}
