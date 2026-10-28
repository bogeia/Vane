package biz

import (
	"cmp"
	"errors"
	"slices"
	"time"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

type ReportCandidate struct {
	Item                      Item
	ProcessedAt, DiscoveredAt time.Time
}
type ReportSection struct {
	Category string
	Items    []Item
}
type Report struct {
	PushedAt   *time.Time
	Date       string
	Headline   *Item
	Highlights []Item
	Sections   []ReportSection
	Briefs     []Item
}

var ErrInvalidReportDate = errors.New("invalid report date")

var beijing = time.FixedZone("Asia/Shanghai", 8*60*60)

func ReportWindow(date string) (time.Time, time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", date, beijing)
	if err != nil {
		return time.Time{}, time.Time{}, ErrInvalidReportDate
	}
	end := day.Add(8 * time.Hour)
	return end.Add(-24 * time.Hour), end, nil
}

func CompileReport(date string, candidates []ReportCandidate, recentStoryIDs []string) (Report, error) {
	start, end, err := ReportWindow(date)
	if err != nil {
		return Report{}, err
	}
	report := Report{Date: date, Highlights: []Item{}, Sections: []ReportSection{}, Briefs: []Item{}}
	grouped := map[string][]Item{}
	for _, candidate := range candidates {
		item := candidate.Item
		if item.Roundup || !item.Selected || item.StoryID == "" || candidate.ProcessedAt.Before(start) || !candidate.ProcessedAt.Before(end) || candidate.DiscoveredAt.Sub(item.PublishedAt) > 48*time.Hour || slices.Contains(recentStoryIDs, item.StoryID) {
			continue
		}
		grouped[item.StoryID] = append(grouped[item.StoryID], item)
	}
	type ranked struct {
		item  Item
		score int
	}
	ranking := make([]ranked, 0, len(grouped))
	for _, items := range grouped {
		sources := map[string]bool{}
		highest := 0
		for _, item := range items {
			sources[item.Source.ID] = true
			highest = max(highest, item.Score)
		}
		ranking = append(ranking, ranked{Representative(items), highest + 5*len(sources)})
	}
	slices.SortFunc(ranking, func(a, b ranked) int {
		if order := cmp.Compare(b.score, a.score); order != 0 {
			return order
		}
		if order := a.item.PublishedAt.Compare(b.item.PublishedAt); order != 0 {
			return order
		}
		return cmp.Compare(a.item.StoryID, b.item.StoryID)
	})
	body := make([]Item, 0)
	used := map[string]int{}
	usedCategories := map[string]int{}
	for _, entry := range ranking {
		category := entry.item.Category
		if !slices.Contains(Categories, category) {
			category = taxonomy.CategoryOther
		}
		if len(body) < 12 && used[entry.item.Source.ID] < 2 && usedCategories[category] < 4 {
			body = append(body, entry.item)
			used[entry.item.Source.ID]++
			usedCategories[category]++
		} else if len(report.Briefs) < 10 {
			report.Briefs = append(report.Briefs, entry.item)
		}
	}
	if len(body) > 0 {
		headline := body[0]
		report.Headline = &headline
	}
	if len(body) > 1 {
		report.Highlights = append(report.Highlights, body[1:min(4, len(body))]...)
	}
	categories := append(slices.Clone(Categories), taxonomy.CategoryOther)
	for _, category := range categories {
		section := ReportSection{Category: category, Items: []Item{}}
		for _, item := range body {
			key := item.Category
			if !slices.Contains(Categories, key) {
				key = taxonomy.CategoryOther
			}
			if key == category {
				section.Items = append(section.Items, item)
			}
		}
		if len(section.Items) > 0 {
			report.Sections = append(report.Sections, section)
		}
	}
	return report, nil
}
