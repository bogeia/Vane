package main

import (
	"time"

	"github.com/bogeia/vane/internal/biz"
)

type outputItemSource struct {
	IconURL string `json:"icon_url"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Tier    string `json:"tier"`
}
type outputItem struct {
	ID            string           `json:"id"`
	Title         string           `json:"title"`
	OriginalTitle string           `json:"original_title"`
	Summary       string           `json:"summary"`
	Reason        string           `json:"reason"`
	URL           string           `json:"url"`
	ImageURL      string           `json:"image_url"`
	Source        outputItemSource `json:"source"`
	PublishedAt   time.Time        `json:"published_at"`
	Score         int              `json:"score"`
	Selected      bool             `json:"selected"`
	Category      string           `json:"category"`
	Tags          []string         `json:"tags"`
	StoryID       string           `json:"story_id"`
	StoryOthers   int              `json:"story_others"`
	StoryItemIDs  []string         `json:"story_item_ids"`
}
type outputReportSection struct {
	Category string       `json:"category"`
	Items    []outputItem `json:"items"`
}
type outputReport struct {
	Date       string                `json:"date"`
	Headline   *outputItem           `json:"headline"`
	Highlights []outputItem          `json:"highlights"`
	Sections   []outputReportSection `json:"sections"`
	Briefs     []outputItem          `json:"briefs"`
}
type outputSourceHealth struct {
	NewBlocked       int64    `json:"new_blocked"`
	Days             int      `json:"days"`
	Fetches          int64    `json:"fetches"`
	FetchSuccessRate *float64 `json:"fetch_success_rate"`
	Prefiltered      int64    `json:"prefiltered"`
	AIRelatedRate    *float64 `json:"ai_related_rate"`
	Done             int64    `json:"done"`
	SelectionRate    *float64 `json:"selection_rate"`
	Scored           int64    `json:"scored"`
	AverageScore     *float64 `json:"average_score"`
}
type outputSourceStatus struct {
	Health          outputSourceHealth `json:"health"`
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Tier            string             `json:"tier"`
	Enabled         bool               `json:"enabled"`
	LastError       string             `json:"last_error"`
	ItemCount       int64              `json:"item_count"`
	LastCollectedAt *time.Time         `json:"last_collected_at"`
}
type outputFailedItem struct {
	ID            string `json:"id"`
	OriginalTitle string `json:"original_title"`
	Attempts      int    `json:"attempts"`
	LastError     string `json:"last_error"`
}
type outputBlockedContent struct {
	SourceID       string    `json:"source_id"`
	OriginalTitle  string    `json:"original_title"`
	URL            string    `json:"url"`
	FirstBlockedAt time.Time `json:"first_blocked_at"`
}
type outputStatus struct {
	RecentBlocked []outputBlockedContent `json:"recent_blocked"`
	Sources       []outputSourceStatus   `json:"sources"`
	Stages        map[string]int64       `json:"stages"`
	FailedItems   []outputFailedItem     `json:"failed_items"`
}

// outputGoldItem keeps the editable label ahead of the original body in exported YAML.
type outputGoldItem struct {
	ID            int64     `yaml:"id"`
	URL           string    `yaml:"url"`
	Tier          string    `yaml:"tier"`
	Source        string    `yaml:"source"`
	Title         string    `yaml:"title"`
	OriginalTitle string    `yaml:"original_title"`
	PublishedAt   time.Time `yaml:"published_at"`
	Score1        int       `yaml:"score1"`
	Score2        int       `yaml:"score2"`
	Label         string    `yaml:"label"`
	Body          string    `yaml:"body"`
}
type outputGoldSet struct {
	Items []outputGoldItem `yaml:"items"`
}
type outputEvaluationMetrics struct {
	Total                   int            `json:"total"`
	Labels                  map[string]int `json:"labels"`
	Answered                int            `json:"answered"`
	Failures                int            `json:"failures"`
	Coverage                float64        `json:"coverage"`
	Accuracy                float64        `json:"accuracy"`
	CompleteAccuracy        float64        `json:"complete_accuracy"`
	Precision               float64        `json:"precision"`
	Recall                  float64        `json:"recall"`
	F1                      float64        `json:"f1"`
	GoldSelectionRate       float64        `json:"gold_selection_rate"`
	PredictionSelectionRate float64        `json:"prediction_selection_rate"`
}
type outputThresholdMetrics struct {
	Threshold               int     `json:"threshold"`
	Accuracy                float64 `json:"accuracy"`
	Precision               float64 `json:"precision"`
	Recall                  float64 `json:"recall"`
	F1                      float64 `json:"f1"`
	PredictionSelectionRate float64 `json:"prediction_selection_rate"`
}
type outputEvaluationMistake struct {
	ID     int64  `json:"id"`
	Tier   string `json:"tier"`
	Title  string `json:"title"`
	Label  string `json:"label"`
	Score1 *int   `json:"score1"`
	Score2 *int   `json:"score2"`
}
type outputEvaluation struct {
	All                   outputEvaluationMetrics             `json:"all"`
	Tiers                 map[string]outputEvaluationMetrics  `json:"tiers"`
	ThresholdScan         map[string][]outputThresholdMetrics `json:"threshold_scan"`
	RecommendedThresholds map[string]*int                     `json:"recommended_thresholds"`
	Mistakes              []outputEvaluationMistake           `json:"mistakes"`
	AverageScoreDelta     *float64                            `json:"average_score_delta"`
}

func toOutputItemSource(value biz.ItemSource) outputItemSource {
	result := outputItemSource{ID: value.ID, Name: value.Name, Tier: value.Tier, IconURL: sourceIconURL(value.ID, value.IconVersion)}
	return result
}

func toOutputItem(value biz.Item) outputItem {
	result := outputItem{ID: value.ID, Title: value.Title, OriginalTitle: value.OriginalTitle, Summary: value.Summary, Reason: value.Reason, URL: value.URL, ImageURL: value.ImageURL, Source: toOutputItemSource(value.Source), PublishedAt: value.PublishedAt, Score: value.Score, Selected: value.Selected, Category: value.Category, Tags: value.Tags, StoryID: value.StoryID, StoryOthers: value.StoryOthers, StoryItemIDs: value.StoryItemIDs}
	return result
}

func toOutputReportSection(value biz.ReportSection) outputReportSection {
	result := outputReportSection{Category: value.Category}
	if value.Items != nil {
		result.Items = make([]outputItem, len(value.Items))
		for i := range value.Items {
			result.Items[i] = toOutputItem(value.Items[i])
		}
	}
	return result
}

func toOutputReport(value biz.Report) outputReport {
	result := outputReport{Date: value.Date}
	if value.Headline != nil {
		converted := toOutputItem(*value.Headline)
		result.Headline = &converted
	}
	if value.Highlights != nil {
		result.Highlights = make([]outputItem, len(value.Highlights))
		for i := range value.Highlights {
			result.Highlights[i] = toOutputItem(value.Highlights[i])
		}
	}
	if value.Sections != nil {
		result.Sections = make([]outputReportSection, len(value.Sections))
		for i := range value.Sections {
			result.Sections[i] = toOutputReportSection(value.Sections[i])
		}
	}
	if value.Briefs != nil {
		result.Briefs = make([]outputItem, len(value.Briefs))
		for i := range value.Briefs {
			result.Briefs[i] = toOutputItem(value.Briefs[i])
		}
	}
	return result
}

func toOutputSourceHealth(value biz.SourceHealth) outputSourceHealth {
	result := outputSourceHealth{NewBlocked: value.NewBlocked, Days: value.Days, Fetches: value.Fetches, FetchSuccessRate: value.FetchSuccessRate, Prefiltered: value.Prefiltered, AIRelatedRate: value.AIRelatedRate, Done: value.Done, SelectionRate: value.SelectionRate, Scored: value.Scored, AverageScore: value.AverageScore}
	return result
}

func toOutputSourceStatus(value biz.SourceStatus) outputSourceStatus {
	result := outputSourceStatus{Health: toOutputSourceHealth(value.Health), ID: value.ID, Name: value.Name, Tier: value.Tier, Enabled: value.Enabled, LastError: value.LastError, ItemCount: value.ItemCount, LastCollectedAt: value.LastCollectedAt}
	return result
}

func toOutputFailedItem(value biz.FailedItem) outputFailedItem {
	result := outputFailedItem{ID: value.ID, OriginalTitle: value.OriginalTitle, Attempts: value.Attempts, LastError: value.LastError}
	return result
}

func toOutputStatus(value biz.Status) outputStatus {
	result := outputStatus{Stages: value.Stages}
	if value.RecentBlocked != nil {
		result.RecentBlocked = make([]outputBlockedContent, len(value.RecentBlocked))
		for i, row := range value.RecentBlocked {
			result.RecentBlocked[i] = outputBlockedContent{SourceID: row.SourceID, OriginalTitle: row.OriginalTitle, URL: row.URL, FirstBlockedAt: row.FirstBlockedAt}
		}
	}
	if value.Sources != nil {
		result.Sources = make([]outputSourceStatus, len(value.Sources))
		for i := range value.Sources {
			result.Sources[i] = toOutputSourceStatus(value.Sources[i])
		}
	}
	if value.FailedItems != nil {
		result.FailedItems = make([]outputFailedItem, len(value.FailedItems))
		for i := range value.FailedItems {
			result.FailedItems[i] = toOutputFailedItem(value.FailedItems[i])
		}
	}
	return result
}

func toOutputGoldItem(value biz.GoldItem) outputGoldItem {
	result := outputGoldItem{ID: value.ID, URL: value.URL, Tier: value.Tier, Source: value.Source, Title: value.Title, OriginalTitle: value.OriginalTitle, PublishedAt: value.PublishedAt, Score1: value.Score1, Score2: value.Score2, Label: value.Label, Body: value.Body}
	return result
}

func fromOutputGoldItem(value outputGoldItem) biz.GoldItem {
	result := biz.GoldItem{ID: value.ID, URL: value.URL, Tier: value.Tier, Source: value.Source, Title: value.Title, OriginalTitle: value.OriginalTitle, PublishedAt: value.PublishedAt, Score1: value.Score1, Score2: value.Score2, Label: value.Label, Body: value.Body}
	return result
}

func toOutputGoldSet(value biz.GoldSet) outputGoldSet {
	result := outputGoldSet{}
	if value.Items != nil {
		result.Items = make([]outputGoldItem, len(value.Items))
		for i := range value.Items {
			result.Items[i] = toOutputGoldItem(value.Items[i])
		}
	}
	return result
}

func toOutputEvaluationMetrics(value biz.EvaluationMetrics) outputEvaluationMetrics {
	result := outputEvaluationMetrics{Total: value.Total, Labels: value.Labels, Answered: value.Answered, Failures: value.Failures, Coverage: value.Coverage, Accuracy: value.Accuracy, CompleteAccuracy: value.CompleteAccuracy, Precision: value.Precision, Recall: value.Recall, F1: value.F1, GoldSelectionRate: value.GoldSelectionRate, PredictionSelectionRate: value.PredictionSelectionRate}
	return result
}

func toOutputThresholdMetrics(value biz.ThresholdMetrics) outputThresholdMetrics {
	result := outputThresholdMetrics{Threshold: value.Threshold, Accuracy: value.Accuracy, Precision: value.Precision, Recall: value.Recall, F1: value.F1, PredictionSelectionRate: value.PredictionSelectionRate}
	return result
}

func toOutputEvaluationMistake(value biz.EvaluationMistake) outputEvaluationMistake {
	result := outputEvaluationMistake{ID: value.ID, Tier: value.Tier, Title: value.Title, Label: value.Label, Score1: value.Score1, Score2: value.Score2}
	return result
}

func toOutputEvaluation(value biz.Evaluation) outputEvaluation {
	result := outputEvaluation{All: toOutputEvaluationMetrics(value.All), RecommendedThresholds: value.RecommendedThresholds, AverageScoreDelta: value.AverageScoreDelta}
	if value.Tiers != nil {
		result.Tiers = make(map[string]outputEvaluationMetrics, len(value.Tiers))
		for key, item := range value.Tiers {
			result.Tiers[key] = toOutputEvaluationMetrics(item)
		}
	}
	if value.ThresholdScan != nil {
		result.ThresholdScan = make(map[string][]outputThresholdMetrics, len(value.ThresholdScan))
		for key, items := range value.ThresholdScan {
			if items == nil {
				result.ThresholdScan[key] = nil
				continue
			}
			converted := make([]outputThresholdMetrics, len(items))
			for i, item := range items {
				converted[i] = toOutputThresholdMetrics(item)
			}
			result.ThresholdScan[key] = converted
		}
	}
	if value.Mistakes != nil {
		result.Mistakes = make([]outputEvaluationMistake, len(value.Mistakes))
		for i := range value.Mistakes {
			result.Mistakes[i] = toOutputEvaluationMistake(value.Mistakes[i])
		}
	}
	return result
}
