package integration

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestItemListsIncludeEveryPublicStoryMemberOutsideFiltersAndPage(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	cfg.Sources = nil
	app := assembleTestApp(t, cfg)
	ctx := t.Context()
	if _, err := testDB(t, app.Data).ExecContext(ctx, `INSERT INTO sources(id,name,tier,feed_url,enabled,interval_minutes) VALUES('members','Members','official','https://example.test/members',false,60)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	shared := insertFoldTestStory(t, ctx, testDB(t, app.Data), "Shared", now)
	singleton := insertFoldTestStory(t, ctx, testDB(t, app.Data), "Singleton", now)
	insert := func(story int64, stage, title, category string) int64 {
		return insertFoldTestItem(t, ctx, testDB(t, app.Data), story, stage, "members", title, title, "summary", "reason", "https://example.test/"+title, category, []string{}, now, true)
	}
	representative := insert(shared, "done", "Representative", "模型")
	sibling := insert(shared, "done", "Sibling", "论文")
	single := insert(singleton, "done", "Singleton", "模型")
	for _, stage := range []string{"new", "prefiltered", "analyzed", "blocked", "failed"} {
		insert(shared, stage, "hidden-"+stage, "模型")
	}
	for story, rep := range map[int64]int64{shared: representative, singleton: single} {
		if _, err := testDB(t, app.Data).ExecContext(ctx, `UPDATE stories SET representative_item_id=$2 WHERE id=$1`, story, rep); err != nil {
			t.Fatal(err)
		}
	}
	for _, view := range []string{"selected", "all"} {
		for _, query := range []struct {
			q    string
			want []string
		}{{"Representative", []string{strconv.FormatInt(representative, 10), strconv.FormatInt(sibling, 10)}}, {"Singleton", []string{strconv.FormatInt(single, 10)}}} {
			response := requestFoldItems(t, app.Router, fmt.Sprintf("/api/items?view=%s&limit=1&category=模型&q=%s", view, query.q))
			var page struct {
				Items []struct {
					ID           string   `json:"id"`
					StoryItemIDs []string `json:"story_item_ids"`
				} `json:"items"`
			}
			if err := json.Unmarshal(response, &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 1 {
				t.Fatalf("%s/%s items=%d", view, query.q, len(page.Items))
			}
			slices.Sort(page.Items[0].StoryItemIDs)
			slices.Sort(query.want)
			if !slices.Equal(page.Items[0].StoryItemIDs, query.want) {
				t.Fatalf("%s/%s story_item_ids=%v want %v", view, query.q, page.Items[0].StoryItemIDs, query.want)
			}
		}
	}
}
