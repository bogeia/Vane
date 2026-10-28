package biz_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

func TestCompileReportAppliesBeijingWindowAndPublicationAge(t *testing.T) {
	beijing, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.October, 21, 8, 0, 0, 0, beijing)
	end := start.Add(24 * time.Hour)
	selectedAt := func(id string, processed, published time.Time, selected bool) biz.ReportCandidate {
		item := reportItem(id, "story-"+id, id, "模型", 80)
		item.Selected = selected
		item.PublishedAt = published
		return biz.ReportCandidate{Item: item, ProcessedAt: processed, DiscoveredAt: processed}
	}
	candidates := []biz.ReportCandidate{
		selectedAt("at-start", start, start.Add(-48*time.Hour), true),
		selectedAt("before-start", start.Add(-time.Nanosecond), start, true),
		selectedAt("at-end", end, end, true),
		selectedAt("not-selected", start.Add(time.Hour), start, false),
		selectedAt("old-at-discovery", start.Add(2*time.Hour), start.Add(-46*time.Hour-time.Nanosecond), true),
		selectedAt("old-by-a-nanosecond", start.Add(3*time.Hour), start.Add(-45*time.Hour-time.Nanosecond), true),
	}

	got, err := biz.CompileReport("2026-10-22", candidates, nil)
	if err != nil {
		t.Fatalf("CompileReport() error = %v", err)
	}
	if got.Date != "2026-10-22" || got.Headline == nil || got.Headline.ID != "at-start" {
		t.Fatalf("report = %+v, want only the inclusive 08:00 boundary item with exactly 48h age", got)
	}
	if len(got.Highlights) != 0 || len(got.Briefs) != 0 || len(got.Sections) != 1 {
		t.Fatalf("single-item report has unexpected highlights/briefs/sections: %+v", got)
	}

	empty, err := biz.CompileReport("2026-10-22", candidates[1:], nil)
	if err != nil {
		t.Fatalf("CompileReport(empty) error = %v", err)
	}
	if empty.Headline != nil || len(empty.Highlights) != 0 || len(empty.Sections) != 0 || len(empty.Briefs) != 0 {
		t.Fatalf("empty report must have nil headline and empty arrays: %+v", empty)
	}
}

func TestCompileReportExcludesRoundupFromEverySection(t *testing.T) {
	processed := time.Date(2026, time.October, 21, 9, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	candidates := make([]biz.ReportCandidate, 0, 16)
	for i := range 16 {
		id := fmt.Sprintf("item-%02d", i)
		item := reportItem(id, id, id, "模型", 100-i)
		item.Roundup = i == 0 || i >= 12
		item.PublishedAt = processed.Add(-time.Duration(i) * time.Minute)
		candidates = append(candidates, biz.ReportCandidate{Item: item, ProcessedAt: processed, DiscoveredAt: processed})
	}

	got, err := biz.CompileReport("2026-10-22", candidates, nil)
	if err != nil {
		t.Fatalf("CompileReport() error = %v", err)
	}
	var included []biz.Item
	if got.Headline != nil {
		included = append(included, *got.Headline)
	}
	included = append(included, got.Highlights...)
	for _, section := range got.Sections {
		included = append(included, section.Items...)
	}
	included = append(included, got.Briefs...)
	for _, item := range included {
		if item.Roundup {
			t.Errorf("roundup item %q appears in the report", item.ID)
		}
	}
	if got.Headline == nil || got.Headline.ID != "item-01" {
		t.Fatalf("headline = %+v, want highest-ranked ordinary item", got.Headline)
	}
}

func TestCompileReportDeduplicatesRecentStoriesAndChoosesQualifiedRepresentative(t *testing.T) {
	beijing, _ := time.LoadLocation("Asia/Shanghai")
	start := time.Date(2026, time.October, 21, 8, 0, 0, 0, beijing)
	processed := start.Add(time.Hour)
	candidate := func(id, story, source, tier, title, category string, score int, selected bool) biz.ReportCandidate {
		item := reportItem(id, story, source, category, score)
		item.Title, item.Source.Tier, item.Selected = title, tier, selected
		item.PublishedAt = processed.Add(-time.Duration(score) * time.Minute)
		return biz.ReportCandidate{Item: item, ProcessedAt: processed, DiscoveredAt: processed}
	}
	candidates := []biz.ReportCandidate{
		candidate("old-pick", "recent", "official", "official", "old issue", "模型", 100, true),
		candidate("unselected-rep", "qualified", "official", "official", "unselected title", "产品", 100, false),
		candidate("selected-rep", "qualified", "media", "media", "selected title", "行业", 99, true),
		candidate("peer", "qualified", "second-media", "media", "peer title", "论文", 80, true),
		candidate("independent", "independent", "another", "official", "independent title", "教程", 90, true),
	}

	got, err := biz.CompileReport("2026-10-22", candidates, []string{"recent"})
	if err != nil {
		t.Fatalf("CompileReport() error = %v", err)
	}
	if got.Headline == nil || got.Headline.ID != "selected-rep" {
		t.Fatalf("headline = %+v, want representative chosen only among selected eligible items", got.Headline)
	}
	if got.Headline.StoryID != "qualified" || len(got.Highlights) != 1 || got.Highlights[0].ID != "independent" {
		t.Fatalf("report did not exclude a recent story or sort remaining events by score plus source bonus: %+v", got)
	}
	if len(got.Sections) != 2 || got.Sections[0].Category != "行业" || got.Sections[1].Category != "教程" {
		t.Fatalf("sections = %+v, want categories ordered by their first appearance in the sorted body", got.Sections)
	}
}

func TestCompileReportLimitsBodyPerSourceAndBuildsSectionsAndBriefs(t *testing.T) {
	beijing, _ := time.LoadLocation("Asia/Shanghai")
	processed := time.Date(2026, time.October, 21, 9, 0, 0, 0, beijing)
	categories := []string{"", "模型", "产品", "行业", "论文", "教程", "观点"}
	var candidates []biz.ReportCandidate
	for sourceIndex := range 7 {
		for storyIndex := range 3 {
			id := fmt.Sprintf("source-%d-item-%d", sourceIndex, storyIndex)
			item := reportItem(id, id, fmt.Sprintf("source-%d", sourceIndex), categories[sourceIndex], 100-sourceIndex*3-storyIndex)
			item.PublishedAt = processed.Add(-time.Duration(sourceIndex*3+storyIndex) * time.Minute)
			candidates = append(candidates, biz.ReportCandidate{Item: item, ProcessedAt: processed, DiscoveredAt: processed})
		}
	}

	got, err := biz.CompileReport("2026-10-22", candidates, nil)
	if err != nil {
		t.Fatalf("CompileReport() error = %v", err)
	}
	body := reportBody(got)
	if len(body) != 12 || len(got.Briefs) != 9 {
		t.Fatalf("body=%d, briefs=%d; want 12 and remaining 9: %+v", len(body), len(got.Briefs), got)
	}
	perSource := make(map[string]int)
	for _, item := range body {
		perSource[item.Source.ID]++
	}
	for source, count := range perSource {
		if count > 2 {
			t.Errorf("source %q contributes %d body items, want at most 2", source, count)
		}
	}
	if got.Headline == nil || got.Headline.ID != "source-0-item-0" || len(got.Highlights) != 3 {
		t.Errorf("headline/highlights do not reflect ranked body positions 1 and 2–4: %+v", got)
	} else {
		wantHighlights := []string{"source-0-item-1", "source-1-item-0", "source-1-item-1"}
		for i, want := range wantHighlights {
			if got.Highlights[i].ID != want {
				t.Errorf("highlight %d = %q, want %q", i, got.Highlights[i].ID, want)
			}
		}
	}
	if len(got.Sections) == 0 {
		t.Fatal("body items were not grouped into report sections")
	}
	var sectionItems []biz.Item
	for _, section := range got.Sections {
		sectionItems = append(sectionItems, section.Items...)
	}
	if len(sectionItems) != len(body) {
		t.Fatalf("sections contain %d items, want all %d body items", len(sectionItems), len(body))
	}
	var sectionNames []string
	for _, section := range got.Sections {
		sectionNames = append(sectionNames, section.Category)
	}
	if !slices.Equal(sectionNames, []string{"模型", "产品", "行业", "论文", "教程", "其他"}) {
		t.Errorf("sections = %v, want configured category order with empty category last as 其他", sectionNames)
	}
}

func TestCompileReportLimitsBodyPerCategory(t *testing.T) {
	for _, tc := range []struct {
		name       string
		categories []string
		section    string
	}{
		{name: "known category", categories: []string{"产品"}, section: "产品"},
		{name: "missing and unknown share other", categories: []string{"", "unknown", "其他"}, section: "其他"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			processed := time.Date(2026, time.October, 21, 9, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
			var candidates []biz.ReportCandidate
			for i := range 6 {
				id := fmt.Sprintf("item-%d", i)
				item := reportItem(id, id, id, tc.categories[i%len(tc.categories)], 100-i)
				candidates = append(candidates, biz.ReportCandidate{Item: item, ProcessedAt: processed, DiscoveredAt: processed})
			}
			got, err := biz.CompileReport("2026-10-22", candidates, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Sections) != 1 || got.Sections[0].Category != tc.section {
				t.Fatalf("sections = %+v, want only %s", got.Sections, tc.section)
			}
			var bodyIDs, briefIDs []string
			for _, item := range reportBody(got) {
				bodyIDs = append(bodyIDs, item.ID)
			}
			for _, item := range got.Briefs {
				briefIDs = append(briefIDs, item.ID)
			}
			if !slices.Equal(bodyIDs, []string{"item-0", "item-1", "item-2", "item-3"}) || !slices.Equal(briefIDs, []string{"item-4", "item-5"}) {
				t.Fatalf("body = %v, briefs = %v; want four highest-ranked items in body and remainder in briefs", bodyIDs, briefIDs)
			}
		})
	}
}

func TestCompileReportAppliesSourceAndCategoryLimitsTogether(t *testing.T) {
	processed := time.Date(2026, time.October, 21, 9, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	var candidates []biz.ReportCandidate
	for i, entry := range []struct{ source, category string }{
		{"a", "产品"},
		{"a", "产品"},
		{"a", "模型"},
		{"b", "产品"},
		{"c", "产品"},
		{"d", "产品"},
		{"d", "模型"},
		{"d", "模型"},
		{"e", "模型"},
	} {
		id := fmt.Sprintf("item-%d", i)
		item := reportItem(id, id, entry.source, entry.category, 100-i)
		candidates = append(candidates, biz.ReportCandidate{Item: item, ProcessedAt: processed, DiscoveredAt: processed})
	}
	got, err := biz.CompileReport("2026-10-22", candidates, nil)
	if err != nil {
		t.Fatal(err)
	}
	var bodyIDs, briefIDs []string
	for _, item := range reportBody(got) {
		bodyIDs = append(bodyIDs, item.ID)
	}
	for _, item := range got.Briefs {
		briefIDs = append(briefIDs, item.ID)
	}
	slices.Sort(bodyIDs)
	if !slices.Equal(bodyIDs, []string{"item-0", "item-1", "item-3", "item-4", "item-6", "item-7", "item-8"}) || !slices.Equal(briefIDs, []string{"item-2", "item-5"}) {
		t.Fatalf("body = %v, briefs = %v; want either full limit to route to briefs without consuming the other limit", bodyIDs, briefIDs)
	}
}

func reportItem(id, story, source, category string, score int) biz.Item {
	return biz.Item{
		ID: id, Title: "title " + id, Source: biz.ItemSource{ID: source, Name: source, Tier: "media"},
		PublishedAt: time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC),
		Score:       score, Selected: true, Category: category, StoryID: story,
	}
}

func reportBody(report biz.Report) []biz.Item {
	var body []biz.Item
	for _, section := range report.Sections {
		body = append(body, section.Items...)
	}
	return body
}
