package biz_test

import (
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

func TestRankHotUsesLatestSourceMentionAnd48HourWindow(t *testing.T) {
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	mentions := []biz.HotMention{
		{StoryID: "weight", Title: "Weight event", Source: biz.ItemSource{ID: "source-a", Name: "A"}, PublishedAt: now.Add(-30 * time.Minute)},
		{StoryID: "weight", Title: "Weight event", Source: biz.ItemSource{ID: "source-a", Name: "A"}, PublishedAt: now},
		{StoryID: "weight", Title: "Weight event", Source: biz.ItemSource{ID: "source-b", Name: "B"}, PublishedAt: now.Add(-48 * time.Hour)},
		{StoryID: "outside-only", Title: "Outside event", Source: biz.ItemSource{ID: "outside-a", Name: "Outside A"}, PublishedAt: now.Add(-48*time.Hour - time.Nanosecond)},
		{StoryID: "outside-only", Title: "Outside event", Source: biz.ItemSource{ID: "outside-b", Name: "Outside B"}, PublishedAt: now.Add(-48*time.Hour - time.Nanosecond)},
		{StoryID: "weight", Title: "Weight event", Source: biz.ItemSource{ID: "future", Name: "Future"}, PublishedAt: now.Add(time.Nanosecond)},
		{StoryID: "single-source", Title: "One source only", Source: biz.ItemSource{ID: "only", Name: "Only"}, PublishedAt: now},
		{StoryID: "single-source", Title: "One source only", Source: biz.ItemSource{ID: "only", Name: "Only"}, PublishedAt: now.Add(-time.Hour)},
		{StoryID: "future-only", Title: "Future only", Source: biz.ItemSource{ID: "future-a", Name: "Future A"}, PublishedAt: now.Add(time.Hour)},
		{StoryID: "future-only", Title: "Future only", Source: biz.ItemSource{ID: "future-b", Name: "Future B"}, PublishedAt: now.Add(2 * time.Hour)},
	}

	got := biz.RankHot(mentions, now)
	if len(got) != 1 {
		t.Fatalf("RankHot() returned %d stories, want only the two-source in-window event: %+v", len(got), got)
	}
	if got[0].StoryID != "weight" || got[0].Title != "Weight event" || got[0].Heat != 125 || got[0].Trend != "new" {
		t.Fatalf("RankHot() = %+v, want boundary-inclusive heat=125 and trend=new", got[0])
	}
	if len(got[0].Sources) != 2 {
		t.Errorf("RankHot() sources = %+v, want latest source A and boundary source B only", got[0].Sources)
	}
}

func TestRankHotComparesRawHeatForTrendsAndStableOrdering(t *testing.T) {
	now := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	mentions := []biz.HotMention{
		{StoryID: "rising", Title: "Rising", Source: biz.ItemSource{ID: "a", Name: "A"}, PublishedAt: now.Add(-30 * time.Hour)},
		{StoryID: "rising", Title: "Rising", Source: biz.ItemSource{ID: "a", Name: "A"}, PublishedAt: now.Add(-time.Hour)},
		{StoryID: "rising", Title: "Rising", Source: biz.ItemSource{ID: "b", Name: "B"}, PublishedAt: now.Add(-30 * time.Hour)},
		{StoryID: "rising", Title: "Rising", Source: biz.ItemSource{ID: "b", Name: "B"}, PublishedAt: now.Add(-time.Hour)},
		{StoryID: "steady", Title: "Steady", Source: biz.ItemSource{ID: "a", Name: "A"}, PublishedAt: now.Add(-7 * time.Hour)},
		{StoryID: "steady", Title: "Steady", Source: biz.ItemSource{ID: "b", Name: "B"}, PublishedAt: now.Add(-7 * time.Hour)},
		{StoryID: "steady", Title: "Steady", Source: biz.ItemSource{ID: "a", Name: "A"}, PublishedAt: now.Add(-5 * time.Hour)},
		{StoryID: "steady", Title: "Steady", Source: biz.ItemSource{ID: "b", Name: "B"}, PublishedAt: now.Add(-5 * time.Hour)},
		{StoryID: "new-event", Title: "New event", Source: biz.ItemSource{ID: "a", Name: "A"}, PublishedAt: now},
		{StoryID: "new-event", Title: "New event", Source: biz.ItemSource{ID: "b", Name: "B"}, PublishedAt: now},
		{StoryID: "z-higher-raw", Title: "Higher raw heat", Source: biz.ItemSource{ID: "a", Name: "A"}, PublishedAt: now},
		{StoryID: "z-higher-raw", Title: "Higher raw heat", Source: biz.ItemSource{ID: "b", Name: "B"}, PublishedAt: now.Add(-24 * time.Hour)},
		{StoryID: "a-lower-raw", Title: "Lower raw heat", Source: biz.ItemSource{ID: "a", Name: "A"}, PublishedAt: now.Add(-time.Minute)},
		{StoryID: "a-lower-raw", Title: "Lower raw heat", Source: biz.ItemSource{ID: "b", Name: "B"}, PublishedAt: now.Add(-24*time.Hour - time.Minute)},
		{StoryID: "a-tied", Title: "Tied heat", Source: biz.ItemSource{ID: "a", Name: "A"}, PublishedAt: now},
		{StoryID: "a-tied", Title: "Tied heat", Source: biz.ItemSource{ID: "b", Name: "B"}, PublishedAt: now.Add(-24 * time.Hour)},
	}

	got := biz.RankHot(mentions, now)
	byID := make(map[string]biz.HotStory, len(got))
	for _, story := range got {
		byID[story.StoryID] = story
	}
	if byID["rising"].Trend != "rising" {
		t.Errorf("rising event trend = %q, want rising", byID["rising"].Trend)
	}
	if byID["steady"].Trend != "" {
		t.Errorf("steady event trend = %q, want empty below 20%% growth", byID["steady"].Trend)
	}
	if byID["new-event"].Trend != "new" {
		t.Errorf("new event trend = %q, want new without a prior ranked window", byID["new-event"].Trend)
	}
	if byID["z-higher-raw"].Heat != byID["a-lower-raw"].Heat {
		t.Errorf("raw-order fixtures rounded to different heat: higher=%d lower=%d", byID["z-higher-raw"].Heat, byID["a-lower-raw"].Heat)
	}
	if byID["z-higher-raw"].Heat != byID["a-tied"].Heat {
		t.Errorf("equal-raw fixtures rounded to different heat: higher=%d tied=%d", byID["z-higher-raw"].Heat, byID["a-tied"].Heat)
	}
	positions := make(map[string]int, len(got))
	for index, story := range got {
		positions[story.StoryID] = index
	}
	if positions["z-higher-raw"] >= positions["a-lower-raw"] {
		t.Errorf("stories with equal rounded heat were not ranked by raw heat: %+v", got)
	}
	if positions["a-tied"] >= positions["z-higher-raw"] {
		t.Errorf("equal raw heat was not ordered by story ID: %+v", got)
	}
}
