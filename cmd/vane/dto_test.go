package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/bogeia/vane/internal/biz"
)

func assertCLIJSON(t *testing.T, value any, want string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected any
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("JSON=%s\nwant=%s", encoded, want)
	}
}

func TestCLIStatusJSONContract(t *testing.T) {
	assertCLIJSON(t, toOutputStatus(biz.Status{}), `{"recent_blocked":null,"sources":null,"stages":null,"failed_items":null}`)
	assertCLIJSON(t, toOutputStatus(biz.Status{Sources: []biz.SourceStatus{}, Stages: map[string]int64{}, FailedItems: []biz.FailedItem{}}), `{"recent_blocked":null,"sources":[],"stages":{},"failed_items":[]}`)
	blockedAt := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	assertCLIJSON(t, toOutputStatus(biz.Status{RecentBlocked: []biz.BlockedContent{{SourceID: "fixture", OriginalTitle: "blocked", URL: "https://example.test/blocked", FirstBlockedAt: blockedAt}}}), `{"recent_blocked":[{"source_id":"fixture","original_title":"blocked","url":"https://example.test/blocked","first_blocked_at":"2026-10-23T00:00:00Z"}],"sources":null,"stages":null,"failed_items":null}`)
	rate, score := 0.5, 87.5
	stamp := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	value := biz.Status{Sources: []biz.SourceStatus{{ID: "s", Name: "Source", Tier: "official", Enabled: true, LastError: "failed", ItemCount: 3, LastCollectedAt: &stamp, Health: biz.SourceHealth{Days: 7, Fetches: 2, FetchSuccessRate: &rate, Prefiltered: 4, AIRelatedRate: &rate, Done: 3, SelectionRate: &rate, Scored: 2, AverageScore: &score}}}, Stages: map[string]int64{"done": 3}, FailedItems: []biz.FailedItem{{ID: "9", OriginalTitle: "Original", Attempts: 2, LastError: "failure"}}}
	assertCLIJSON(t, toOutputStatus(value), `{"recent_blocked":null,"sources":[{"health":{"new_blocked":0,"days":7,"fetches":2,"fetch_success_rate":0.5,"prefiltered":4,"ai_related_rate":0.5,"done":3,"selection_rate":0.5,"scored":2,"average_score":87.5},"id":"s","name":"Source","tier":"official","enabled":true,"last_error":"failed","item_count":3,"last_collected_at":"2026-10-23T00:00:00Z"}],"stages":{"done":3},"failed_items":[{"id":"9","original_title":"Original","attempts":2,"last_error":"failure"}]}`)
}

func TestCLIReportJSONHidesInternalMetadata(t *testing.T) {
	stamp := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	raw := 95
	item := biz.Item{ID: "9007199254740993", Title: "中文", PublishedAt: stamp, Score: 87, Score1: &raw, Score2: &raw, Roundup: true, Tags: []string{}, StoryItemIDs: []string{}}
	assertCLIJSON(t, toOutputReport(biz.Report{Date: "2026-10-23", PushedAt: &stamp, Headline: &item, Highlights: []biz.Item{}, Sections: []biz.ReportSection{}, Briefs: nil}), `{"date":"2026-10-23","headline":{"id":"9007199254740993","title":"中文","original_title":"","summary":"","reason":"","url":"","image_url":"","source":{"id":"","name":"","tier":"","icon_url":""},"published_at":"2026-10-23T00:00:00Z","score":87,"selected":false,"category":"","tags":[],"story_id":"","story_others":0,"story_item_ids":[]},"highlights":[],"sections":[],"briefs":null}`)
}

func TestCLIEvaluationJSONContract(t *testing.T) {
	metrics := biz.EvaluationMetrics{Total: 2, Labels: map[string]int{"select": 1}, Answered: 2, Failures: 1, Coverage: 0.9, Accuracy: 0.8, CompleteAccuracy: 0.7, Precision: 0.6, Recall: 0.5, F1: 0.4, GoldSelectionRate: 0.3, PredictionSelectionRate: 0.2}
	threshold, score, delta := 75, 88, 1.5
	value := biz.Evaluation{All: metrics, Tiers: map[string]biz.EvaluationMetrics{}, ThresholdScan: map[string][]biz.ThresholdMetrics{"official": {{Threshold: 75, Accuracy: 0.8, Precision: 0.6, Recall: 0.5, F1: 0.4, PredictionSelectionRate: 0.2}}, "media": nil}, RecommendedThresholds: map[string]*int{"official": &threshold, "media": nil}, Mistakes: []biz.EvaluationMistake{{ID: 9, Tier: "official", Title: "标题", Label: "reject", Score1: &score}}, AverageScoreDelta: &delta}
	assertCLIJSON(t, toOutputEvaluation(value), `{"all":{"total":2,"labels":{"select":1},"answered":2,"failures":1,"coverage":0.9,"accuracy":0.8,"complete_accuracy":0.7,"precision":0.6,"recall":0.5,"f1":0.4,"gold_selection_rate":0.3,"prediction_selection_rate":0.2},"tiers":{},"threshold_scan":{"official":[{"threshold":75,"accuracy":0.8,"precision":0.6,"recall":0.5,"f1":0.4,"prediction_selection_rate":0.2}],"media":null},"recommended_thresholds":{"official":75,"media":null},"mistakes":[{"id":9,"tier":"official","title":"标题","label":"reject","score1":88,"score2":null}],"average_score_delta":1.5}`)
}

func TestCLIGoldYAMLContract(t *testing.T) {
	const document = `items:
  - id: 9
    url: https://example.test/a
    tier: official
    source: Source
    title: 中文
    original_title: Original
    published_at: 2026-10-23T00:00:00Z
    score1: 80
    score2: 90
    label: either
    body: 正文
`
	gold, err := ParseGold([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if len(gold.Items) != 1 || gold.Items[0].OriginalTitle != "Original" || gold.Items[0].Score1 != 80 || gold.Items[0].Score2 != 90 || gold.Items[0].Label != "either" || gold.Items[0].Body != "正文" {
		t.Fatalf("gold=%+v", gold)
	}
	encoded, err := yaml.Marshal(toOutputGoldSet(gold))
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected any
	if err := yaml.Unmarshal(encoded, &actual); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(document), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("YAML=%s", encoded)
	}
	if strings.Index(string(encoded), "label:") > strings.Index(string(encoded), "body:") {
		t.Fatal("editable label must precede body")
	}
	for _, set := range []biz.GoldSet{{}, {Items: []biz.GoldItem{}}} {
		encoded, err := yaml.Marshal(toOutputGoldSet(set))
		if err != nil || string(encoded) != "items: []\n" {
			t.Fatalf("empty=%q err=%v", encoded, err)
		}
	}
}

func TestIconVersionReachesItemAndReportDTO(t *testing.T) {
	item := biz.Item{Source: biz.ItemSource{ID: "publisher/name", IconVersion: "ba7816bf8f01cfea414140de"}}
	want := "/api/source-icons/publisher%2Fname?v=ba7816bf8f01cfea414140de"
	if got := toOutputItem(item).Source.IconURL; got != want {
		t.Fatalf("item icon=%q", got)
	}
	report := toOutputReport(biz.Report{Headline: &item, Highlights: []biz.Item{item}, Sections: []biz.ReportSection{{Items: []biz.Item{item}}}, Briefs: []biz.Item{item}})
	for _, got := range []string{report.Headline.Source.IconURL, report.Highlights[0].Source.IconURL, report.Sections[0].Items[0].Source.IconURL, report.Briefs[0].Source.IconURL} {
		if got != want {
			t.Fatalf("report icon=%q", got)
		}
	}
}
