package data

import (
	"encoding/json"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

type storedItemSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tier string `json:"tier"`
}
type storedItem struct {
	ID            string           `json:"id"`
	Title         string           `json:"title"`
	OriginalTitle string           `json:"originalTitle"`
	Summary       string           `json:"summary"`
	Reason        string           `json:"reason"`
	URL           string           `json:"url"`
	ImageURL      string           `json:"imageUrl"`
	Source        storedItemSource `json:"source"`
	PublishedAt   time.Time        `json:"publishedAt"`
	Score         int              `json:"score"`
	Selected      bool             `json:"selected"`
	Category      string           `json:"category"`
	Tags          []string         `json:"tags"`
	StoryID       string           `json:"storyId"`
	StoryOthers   int              `json:"storyOthers"`
	StoryItemIDs  []string         `json:"storyItemIds"`
}
type storedReportSection struct {
	Category string       `json:"category"`
	Items    []storedItem `json:"items"`
}
type storedReport struct {
	Date       string                `json:"date"`
	Headline   *storedItem           `json:"headline"`
	Highlights []storedItem          `json:"highlights"`
	Sections   []storedReportSection `json:"sections"`
	Briefs     []storedItem          `json:"briefs"`
}
type storedFact struct {
	Title      string  `json:"title"`
	Subject    string  `json:"subject"`
	Action     string  `json:"action"`
	Object     string  `json:"object"`
	OccurredAt *string `json:"occurredAt"`
}

func toStoredItemSource(value biz.ItemSource) storedItemSource {
	result := storedItemSource{ID: value.ID, Name: value.Name, Tier: value.Tier}
	return result
}

func fromStoredItemSource(value storedItemSource) biz.ItemSource {
	result := biz.ItemSource{ID: value.ID, Name: value.Name, Tier: value.Tier}
	return result
}

func toStoredItem(value biz.Item) storedItem {
	result := storedItem{ID: value.ID, Title: value.Title, OriginalTitle: value.OriginalTitle, Summary: value.Summary, Reason: value.Reason, URL: value.URL, ImageURL: value.ImageURL, Source: toStoredItemSource(value.Source), PublishedAt: value.PublishedAt, Score: value.Score, Selected: value.Selected, Category: value.Category, Tags: value.Tags, StoryID: value.StoryID, StoryOthers: value.StoryOthers, StoryItemIDs: value.StoryItemIDs}
	return result
}

func fromStoredItem(value storedItem) biz.Item {
	result := biz.Item{ID: value.ID, Title: value.Title, OriginalTitle: value.OriginalTitle, Summary: value.Summary, Reason: value.Reason, URL: value.URL, ImageURL: value.ImageURL, Source: fromStoredItemSource(value.Source), PublishedAt: value.PublishedAt, Score: value.Score, Selected: value.Selected, Category: value.Category, Tags: value.Tags, StoryID: value.StoryID, StoryOthers: value.StoryOthers, StoryItemIDs: value.StoryItemIDs}
	return result
}

func toStoredReportSection(value biz.ReportSection) storedReportSection {
	result := storedReportSection{Category: value.Category}
	if value.Items != nil {
		result.Items = make([]storedItem, len(value.Items))
		for i := range value.Items {
			result.Items[i] = toStoredItem(value.Items[i])
		}
	}
	return result
}

func fromStoredReportSection(value storedReportSection) biz.ReportSection {
	result := biz.ReportSection{Category: value.Category}
	if value.Items != nil {
		result.Items = make([]biz.Item, len(value.Items))
		for i := range value.Items {
			result.Items[i] = fromStoredItem(value.Items[i])
		}
	}
	return result
}

func toStoredReport(value biz.Report) storedReport {
	result := storedReport{Date: value.Date}
	if value.Headline != nil {
		converted := toStoredItem(*value.Headline)
		result.Headline = &converted
	}
	if value.Highlights != nil {
		result.Highlights = make([]storedItem, len(value.Highlights))
		for i := range value.Highlights {
			result.Highlights[i] = toStoredItem(value.Highlights[i])
		}
	}
	if value.Sections != nil {
		result.Sections = make([]storedReportSection, len(value.Sections))
		for i := range value.Sections {
			result.Sections[i] = toStoredReportSection(value.Sections[i])
		}
	}
	if value.Briefs != nil {
		result.Briefs = make([]storedItem, len(value.Briefs))
		for i := range value.Briefs {
			result.Briefs[i] = toStoredItem(value.Briefs[i])
		}
	}
	return result
}

func fromStoredReport(value storedReport) biz.Report {
	result := biz.Report{Date: value.Date}
	if value.Headline != nil {
		converted := fromStoredItem(*value.Headline)
		result.Headline = &converted
	}
	if value.Highlights != nil {
		result.Highlights = make([]biz.Item, len(value.Highlights))
		for i := range value.Highlights {
			result.Highlights[i] = fromStoredItem(value.Highlights[i])
		}
	}
	if value.Sections != nil {
		result.Sections = make([]biz.ReportSection, len(value.Sections))
		for i := range value.Sections {
			result.Sections[i] = fromStoredReportSection(value.Sections[i])
		}
	}
	if value.Briefs != nil {
		result.Briefs = make([]biz.Item, len(value.Briefs))
		for i := range value.Briefs {
			result.Briefs[i] = fromStoredItem(value.Briefs[i])
		}
	}
	return result
}

func toStoredFact(value biz.Fact) storedFact {
	result := storedFact{Title: value.Title, Subject: value.Subject, Action: value.Action, Object: value.Object, OccurredAt: value.OccurredAt}
	return result
}

func fromStoredFact(value storedFact) biz.Fact {
	result := biz.Fact{Title: value.Title, Subject: value.Subject, Action: value.Action, Object: value.Object, OccurredAt: value.OccurredAt}
	return result
}
func encodeReport(value biz.Report) ([]byte, error) { return json.Marshal(toStoredReport(value)) }
func decodeReport(content []byte) (biz.Report, error) {
	var value storedReport
	err := json.Unmarshal(content, &value)
	return fromStoredReport(value), err
}

func encodeFact(value *biz.Fact) ([]byte, error) {
	if value == nil {
		return json.Marshal(nil)
	}
	return json.Marshal(toStoredFact(*value))
}

func decodeFact(content []byte) (*biz.Fact, error) {
	var value *storedFact
	if err := json.Unmarshal(content, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	result := fromStoredFact(*value)
	return &result, nil
}
