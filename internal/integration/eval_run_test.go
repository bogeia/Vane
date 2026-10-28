package integration

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"

	"github.com/bogeia/vane/internal/biz"
)

func TestEvalRunReportsMetricsWithoutDatabaseAndUsesOnlineModelInput(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	var arrivals atomic.Int32
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid", 400)
			return
		}
		var material map[string]json.RawMessage
		for _, message := range request.Messages {
			if message.Role == "user" {
				_ = json.Unmarshal([]byte(message.Content), &material)
			}
		}
		var title, body string
		_ = json.Unmarshal(material["title"], &title)
		_ = json.Unmarshal(material["body"], &body)
		if len(material) != 3 || material["published_at"] == nil || len([]rune(body)) != 6000 {
			t.Errorf("unexpected model input keys/body: %v/%d", material, len([]rune(body)))
		}
		key := r.URL.Path + "/" + title
		mu.Lock()
		calls[key]++
		mu.Unlock()
		if r.URL.Path == "/prefilter/chat/completions" {
			if n := arrivals.Add(1); n <= 2 {
				if n == 2 {
					close(ready)
				}
				select {
				case <-ready:
				case <-time.After(time.Second):
					t.Error("worker.count=2 did not process two items concurrently")
				}
			}
			label := "PASS"
			if title == "blocked" {
				label = "BLOCK"
			}
			writeFakeCompletion(w, `{"label":"`+label+`"}`)
			return
		}
		if r.URL.Path != "/score/chat/completions" {
			t.Errorf("unexpected step route %s", r.URL.Path)
		}
		if title == "failed" {
			http.Error(w, "unavailable", 500)
			return
		}
		score := map[string]int{"tp": 80, "fp": 70, "fn": 50, "either": 100, "reject-tier": 90, "t2": 80}[title]
		content, _ := json.Marshal(map[string]int{"attention_score": score})
		writeFakeCompletion(w, string(content))
	}))
	defer server.Close()
	configPath := writeEvalRunConfig(t, server.URL, true)
	gold := biz.GoldSet{}
	for i, row := range []struct {
		title, tier, label string
		old                int
	}{
		{"tp", "official", "select", 60},
		{"fp", "official", "reject", 60},
		{"fn", "official", "select", 60},
		{"blocked", "official", "reject", 60},
		{"failed", "official", "select", 60},
		{"either", "official", "either", 0},
		{"reject-tier", "media", "reject", 80},
		{"t2", "media", "select", 70},
	} {
		gold.Items = append(gold.Items, biz.GoldItem{ID: int64(i + 1), URL: "https://example.test/" + row.title, Tier: row.tier, Source: "secret source", Title: "中文 " + row.title, OriginalTitle: row.title, PublishedAt: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), Score1: row.old, Score2: row.old, Label: row.label, Body: strings.Repeat("正文", 3001)})
	}
	content, err := marshalGoldFixture(gold)
	if err != nil {
		t.Fatal(err)
	}
	goldPath := filepath.Join(t.TempDir(), "gold.yaml")
	if err := os.WriteFile(goldPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), buildVane(t), "eval", "run", "--conf", configPath, "--gold", goldPath)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal("eval run command failed (output suppressed to protect configuration)")
	}
	type metrics struct {
		Total, Answered, Failures int
		Labels                    map[string]int
		Coverage                  float64 `json:"coverage"`
		Accuracy                  float64 `json:"accuracy"`
		CompleteAccuracy          float64 `json:"complete_accuracy"`
		Precision                 float64 `json:"precision"`
		Recall                    float64 `json:"recall"`
		F1                        float64 `json:"f1"`
		GoldSelectionRate         float64 `json:"gold_selection_rate"`
		PredictionSelectionRate   float64 `json:"prediction_selection_rate"`
	}
	var result struct {
		All           metrics
		Tiers         map[string]metrics
		ThresholdScan map[string][]struct {
			Threshold               int
			Accuracy                float64 `json:"accuracy"`
			Precision               float64 `json:"precision"`
			Recall                  float64 `json:"recall"`
			F1                      float64 `json:"f1"`
			PredictionSelectionRate float64 `json:"prediction_selection_rate"`
		} `json:"threshold_scan"`
		RecommendedThresholds map[string]*int `json:"recommended_thresholds"`
		Mistakes              []struct {
			ID                 int64
			Tier, Title, Label string
			Score1, Score2     *int
		}
		AverageScoreDelta *float64 `json:"average_score_delta"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	all := result.All
	if all.Total != 8 || all.Answered != 6 || all.Failures != 1 || all.Labels["select"] != 4 || all.Labels["reject"] != 3 || all.Labels["either"] != 1 || all.Coverage != .857 || all.Accuracy != .5 || all.CompleteAccuracy != .429 || all.Precision != .5 || all.Recall != .667 || all.F1 != .571 || all.GoldSelectionRate != .571 || all.PredictionSelectionRate != .667 {
		t.Fatalf("all metrics=%+v", all)
	}
	t1 := result.Tiers["official"]
	if t1.Total != 6 || t1.Answered != 4 || t1.Failures != 1 || t1.Coverage != .8 || t1.Accuracy != .5 || t1.CompleteAccuracy != .4 || t1.Precision != .5 || t1.Recall != .5 || t1.F1 != .5 || t1.GoldSelectionRate != .6 || t1.PredictionSelectionRate != .5 {
		t.Fatalf("official metrics=%+v", t1)
	}
	if len(result.Tiers) != 2 || result.Tiers["media"].Total != 2 || result.Tiers["media"].F1 != .667 {
		t.Fatalf("tier metrics=%+v", result.Tiers)
	}
	for _, tier := range []string{"official", "media"} {
		scan := result.ThresholdScan[tier]
		if len(scan) != 26 || scan[0].Threshold != 40 || scan[25].Threshold != 90 {
			t.Fatalf("scan %s=%+v", tier, scan)
		}
		for i, point := range scan {
			if point.Threshold != 40+2*i {
				t.Errorf("non-even scan point %+v", point)
			}
		}
	}
	low := result.ThresholdScan["official"][5]
	if low.Threshold != 50 || low.Accuracy != .75 || low.Precision != .667 || low.Recall != 1 || low.F1 != .8 || low.PredictionSelectionRate != .75 {
		t.Fatalf("official t50=%+v", low)
	}
	if result.RecommendedThresholds["official"] == nil || *result.RecommendedThresholds["official"] != 50 || result.RecommendedThresholds["media"] == nil || *result.RecommendedThresholds["media"] != 76 {
		t.Fatalf("recommended=%+v", result.RecommendedThresholds)
	}
	if len(result.Mistakes) != 3 || result.Mistakes[0].ID != 2 || result.Mistakes[1].ID != 3 || result.Mistakes[2].ID != 7 || result.Mistakes[0].Score1 == nil || *result.Mistakes[0].Score1 != 70 || result.Mistakes[0].Score2 == nil || *result.Mistakes[0].Score2 != 70 || result.Mistakes[0].Label != "reject" || result.Mistakes[0].Title != "中文 fp" || result.Mistakes[0].Tier != "official" {
		t.Fatalf("mistakes=%+v", result.Mistakes)
	}
	if result.AverageScoreDelta == nil || *result.AverageScoreDelta != 23.333 {
		t.Fatalf("average score delta=%v", result.AverageScoreDelta)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["/score/chat/completions/blocked"] != 0 || calls["/score/chat/completions/failed"] != 2 {
		t.Fatalf("unexpected blocked/failed score calls=%v", calls)
	}
	for _, item := range gold.Items {
		if calls["/prefilter/chat/completions/"+item.OriginalTitle] != 1 {
			t.Fatalf("prefilter repeated or missing: %v", calls)
		}
	}
}

func writeEvalRunConfig(t *testing.T, modelURL string, enabled bool) string {
	t.Helper()
	path := writeSubprocessConfig(t, "", "127.0.0.1:8080", t.TempDir())
	settings := viper.New()
	settings.SetConfigFile(path)
	if err := settings.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	settings.Set("llm.enabled", enabled)
	settings.Set("llm.base_url", modelURL)
	settings.Set("llm.api_key", "test-key")
	settings.Set("llm.steps.prefilter.base_url", modelURL+"/prefilter")
	settings.Set("llm.steps.score.base_url", modelURL+"/score")
	settings.Set("llm.retry.max_attempts", 1)
	settings.Set("worker.count", 2)
	settings.Set("worker.retry.max_attempts", 2)
	settings.Set("worker.retry.initial_interval", "1ms")
	settings.Set("worker.retry.max_interval", "1ms")
	if err := settings.WriteConfig(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEvalRunRejectsInvalidGoldBeforeSendingRequests(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeFakeCompletion(w, `{"label":"BLOCK"}`)
	}))
	defer server.Close()
	configPath := writeEvalRunConfig(t, server.URL, true)
	binary := buildVane(t)
	good := map[string]any{"id": 1, "url": "https://example.test/1", "tier": "official", "source": "Fixture", "title": "中文标题", "original_title": "original", "published_at": time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), "score1": 80, "score2": 80, "label": "select", "body": "正文"}
	type mutation struct {
		name, field string
		value       any
		remove      bool
	}
	var cases []mutation
	for _, field := range []string{"id", "url", "tier", "source", "title", "original_title", "published_at", "score1", "score2", "label", "body"} {
		cases = append(cases, mutation{name: "missing " + field, field: field, remove: true}, mutation{name: "null " + field, field: field})
	}
	for _, tier := range []string{"T1", "T1_5", "T2"} {
		cases = append(cases, mutation{name: "old tier " + tier, field: "tier", value: tier})
	}
	cases = append(cases,
		mutation{name: "invalid tier", field: "tier", value: "T0"}, mutation{name: "invalid label", field: "label", value: "maybe"},
		mutation{name: "score too high", field: "score1", value: 101}, mutation{name: "negative score", field: "score2", value: -1},
		mutation{name: "wrong title type", field: "title", value: []string{"a"}}, mutation{name: "bad time", field: "published_at", value: "not-a-time"},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := maps.Clone(good)
			if tc.remove {
				delete(row, tc.field)
			} else {
				row[tc.field] = tc.value
			}
			content, err := yaml.Marshal(map[string]any{"items": []any{good, row}})
			if err != nil {
				t.Fatal(err)
			}
			goldPath := filepath.Join(t.TempDir(), "invalid.yaml")
			if err := os.WriteFile(goldPath, content, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), binary, "eval", "run", "--conf", configPath, "--gold", goldPath)
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "gold item 2") {
				t.Fatalf("error=%v output=%s, want gold item 2 error", err, output)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid gold sent %d model requests", calls.Load())
	}
}

func TestEvalRunRejectsDisabledModelAndMalformedFiles(t *testing.T) {
	configPath := writeEvalRunConfig(t, "https://models.example", false)
	binary := buildVane(t)
	goldPath := filepath.Join(t.TempDir(), "gold.yaml")
	if err := os.WriteFile(goldPath, []byte("items: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), binary, "eval", "run", "--conf", configPath, "--gold", goldPath)
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "model is disabled") {
		t.Fatalf("disabled model error=%v output=%s", err, output)
	}
	configPath = writeEvalRunConfig(t, "https://models.example", true)
	for _, content := range []string{"items: [", "{}", "items: null", "items: {}", "items: [null]", "items: [42]"} {
		t.Run(content, func(t *testing.T) {
			if err := os.WriteFile(goldPath, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), binary, "eval", "run", "--conf", configPath, "--gold", goldPath)
			if err := cmd.Run(); err == nil {
				t.Fatalf("invalid gold accepted: %s", content)
			}
		})
	}
	for _, args := range [][]string{{"eval", "run"}, {"eval", "run", "--conf", configPath}, {"eval", "run", "--gold", goldPath}, {"eval", "run", "--conf", configPath, "--gold", goldPath, "extra"}} {
		if err := exec.CommandContext(t.Context(), binary, args...).Run(); err == nil {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
}

func TestEvalRunHandlesEitherFailuresBlockedSelectionsAndEmptyGold(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Messages []struct{ Content string } }
		_ = json.NewDecoder(r.Body).Decode(&request)
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "either-fail") {
				http.Error(w, "unavailable", 500)
				return
			}
		}
		writeFakeCompletion(w, `{"label":"BLOCK"}`)
	}))
	defer server.Close()
	configPath := writeEvalRunConfig(t, server.URL, true)
	binary := buildVane(t)
	items := make([]biz.GoldItem, 0)
	for i, row := range []struct{ title, tier, label string }{{"either-blocked", "official", "either"}, {"either-fail", "official", "either"}, {"blocked-select", "media", "select"}} {
		items = append(items, biz.GoldItem{ID: int64(i + 1), URL: "https://example.test/", Tier: row.tier, Source: "Fixture", Title: row.title, OriginalTitle: row.title, PublishedAt: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), Score1: 80, Score2: 80, Label: row.label, Body: "正文"})
	}
	for _, gold := range []biz.GoldSet{{Items: items}, {Items: []biz.GoldItem{}}} {
		content, err := marshalGoldFixture(gold)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "gold.yaml")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		output, err := exec.CommandContext(t.Context(), binary, "eval", "run", "--conf", configPath, "--gold", path).Output()
		if err != nil {
			t.Fatal("evaluation command failed")
		}
		type metrics struct {
			Total, Answered, Failures                 int
			Coverage, Accuracy, Precision, Recall, F1 float64
		}
		var result struct {
			All                   metrics
			Tiers                 map[string]metrics
			AverageScoreDelta     *float64        `json:"average_score_delta"`
			RecommendedThresholds map[string]*int `json:"recommended_thresholds"`
			Mistakes              []struct {
				ID             int64
				Score1, Score2 *int
			}
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatal(err)
		}
		if result.AverageScoreDelta != nil || result.RecommendedThresholds["official"] != nil {
			t.Fatalf("no-score/either summary=%s", output)
		}
		if len(gold.Items) == 0 {
			if result.All.Total != 0 || result.All.Answered != 0 || result.All.Failures != 0 || result.All.Coverage != 0 || len(result.Mistakes) != 0 || result.RecommendedThresholds["media"] != nil {
				t.Fatalf("empty gold summary=%s", output)
			}
			continue
		}
		if threshold := result.RecommendedThresholds["media"]; threshold == nil || *threshold != 76 {
			t.Fatalf("blocked selection summary=%s, want media threshold 76", output)
		}
		if result.All.Total != 3 || result.All.Answered != 1 || result.All.Failures != 1 || result.All.Coverage != 1 || result.All.Accuracy != 0 || result.All.Precision != 0 || result.All.Recall != 0 || result.All.F1 != 0 || result.Tiers["official"].Failures != 1 || result.Tiers["official"].Answered != 0 || result.Tiers["official"].Coverage != 0 {
			t.Fatalf("either failure summary=%s", output)
		}
		if len(result.Mistakes) != 1 || result.Mistakes[0].ID != 3 || result.Mistakes[0].Score1 != nil || result.Mistakes[0].Score2 != nil {
			t.Fatalf("blocked selection mistake=%s", output)
		}
	}
}

// marshalGoldFixture constructs the CLI input protocol independently of domain tags.
func marshalGoldFixture(gold biz.GoldSet) ([]byte, error) {
	items := make([]map[string]any, 0, len(gold.Items))
	for _, item := range gold.Items {
		items = append(items, map[string]any{
			"id":             item.ID,
			"url":            item.URL,
			"tier":           item.Tier,
			"source":         item.Source,
			"title":          item.Title,
			"original_title": item.OriginalTitle,
			"published_at":   item.PublishedAt,
			"score1":         item.Score1,
			"score2":         item.Score2,
			"label":          item.Label,
			"body":           item.Body,
		})
	}
	return yaml.Marshal(map[string]any{"items": items})
}
