package biz

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestSimilarityUsesChineseBigramsAndEnglishWordSets(t *testing.T) {
	testCases := []struct {
		name             string
		titleA, summaryA string
		titleB, summaryB string
		want             float64
	}{
		{
			name:   "mixed Chinese and English tokens",
			titleA: "OpenAI 发布 GPT",
			titleB: "openai 发布",
			want:   2.0 / 3.0,
		},
		{
			name:   "tokens are sets rather than counts",
			titleA: "AI AI 发布 发布",
			titleB: "ai 发布",
			want:   1,
		},
		{
			name:   "punctuation separates Chinese bigrams",
			titleA: "中。文",
			titleB: "中文",
			want:   0,
		},
		{
			name:     "title and summary do not join across their boundary",
			titleA:   "中",
			summaryA: "文",
			titleB:   "中文",
			want:     0,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			got := Similarity(test.titleA, test.summaryA, test.titleB, test.summaryB)
			if math.Abs(got-test.want) > 1e-12 {
				t.Errorf("Similarity() = %.12f, want %.12f", got, test.want)
			}
		})
	}
}

func TestRecallUsesMaximumPerStoryThresholdAndTopFive(t *testing.T) {
	query := "一二三四五六七八九十百"

	t.Run("max similarity per story and inclusive threshold", func(t *testing.T) {
		members := []StoryMember{
			{StoryID: 7, Title: "杂乱无关文本"},
			{StoryID: 8, Title: "一二"},
			{StoryID: 9, Title: "百人"},
			{StoryID: 7, Title: query},
		}
		got := Recall(query, "", members)
		if len(got) != 2 {
			t.Fatalf("Recall() returned %d candidates, want the matching story and exact-threshold story: %+v", len(got), got)
		}
		if got[0].StoryID != 7 || math.Abs(got[0].Similarity-1) > 1e-12 {
			t.Errorf("first candidate = %+v, want story 7 at max similarity 1", got[0])
		}
		if got[1].StoryID != 8 || math.Abs(got[1].Similarity-0.1) > 1e-12 {
			t.Errorf("second candidate = %+v, want story 8 at inclusive similarity 0.1", got[1])
		}
	})

	t.Run("equal similarities sort by story id and limit to five", func(t *testing.T) {
		members := make([]StoryMember, 0, 6)
		for id := int64(6); id >= 1; id-- {
			members = append(members, StoryMember{StoryID: id, Title: "OpenAI launches a model"})
		}
		got := Recall("OpenAI launches a model", "", members)
		if len(got) != 5 {
			t.Fatalf("Recall() returned %d candidates, want top five", len(got))
		}
		for index, candidate := range got {
			wantID := int64(index + 1)
			if candidate.StoryID != wantID {
				t.Errorf("candidate %d story id = %d, want %d in deterministic tie order", index, candidate.StoryID, wantID)
			}
		}
	})
}

func TestRepresentativePrefersOfficialThenScorePublicationTimeAndStableID(t *testing.T) {
	timestamp := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: "z", Source: ItemSource{Tier: "media"}, Score: 100, PublishedAt: timestamp.Add(-time.Hour)},
		{ID: "d", Source: ItemSource{Tier: "media"}, Score: 90, PublishedAt: timestamp.Add(-2 * time.Hour)},
		{ID: "c", Source: ItemSource{Tier: "official"}, Score: 80, PublishedAt: timestamp},
		{ID: "b", Source: ItemSource{Tier: "official"}, Score: 80, PublishedAt: timestamp.Add(-time.Hour)},
		{ID: "a", Source: ItemSource{Tier: "official"}, Score: 80, PublishedAt: timestamp.Add(-time.Hour)},
	}

	got := Representative(items)
	if got.ID != "a" {
		t.Fatalf("Representative() id = %q, want official with highest precedence, then score, earliest time, and stable id", got.ID)
	}
}

func TestRepresentativeSkipsRoundupItems(t *testing.T) {
	items := []Item{
		{ID: "roundup", Source: ItemSource{Tier: "official"}, Score: 100, Roundup: true},
		{ID: "ordinary", Source: ItemSource{Tier: "media"}, Score: 40},
	}
	if got := Representative(items); got.ID != "ordinary" {
		t.Fatalf("Representative() = %q, want ordinary item", got.ID)
	}
	if got := Representative(items[:1]); got.ID != "" {
		t.Fatalf("Representative(all roundup) = %q, want empty item", got.ID)
	}
}

func TestChooseStoryUsesConfidenceAndRelationPriority(t *testing.T) {
	testCases := []struct {
		name       string
		candidates []StoryCandidate
		relations  []StoryRelation
		wantID     int64
	}{
		{
			name: "roundup always starts a separate story",
			candidates: []StoryCandidate{
				{StoryMember: StoryMember{StoryID: 3}},
				{StoryMember: StoryMember{StoryID: 8}},
			},
			relations: []StoryRelation{
				{StoryID: 3, Relation: "SAME_OCCURRENCE", Confidence: 0.9},
				{StoryID: 8, Relation: "ROUNDUP", Confidence: 0.4},
			},
			wantID: 0,
		},
		{
			name: "same occurrence outranks earlier same story candidate",
			candidates: []StoryCandidate{
				{StoryMember: StoryMember{StoryID: 8}},
				{StoryMember: StoryMember{StoryID: 3}},
			},
			relations: []StoryRelation{
				{StoryID: 8, Relation: "SAME_STORY", Confidence: 0.9},
				{StoryID: 3, Relation: "SAME_OCCURRENCE", Confidence: 0.6},
			},
			wantID: 3,
		},
		{
			name: "confidence threshold is inclusive and candidates keep recall order",
			candidates: []StoryCandidate{
				{StoryMember: StoryMember{StoryID: 8}},
				{StoryMember: StoryMember{StoryID: 3}},
			},
			relations: []StoryRelation{
				{StoryID: 3, Relation: "SAME_OCCURRENCE", Confidence: 0.59},
				{StoryID: 8, Relation: "SAME_STORY", Confidence: 0.6},
			},
			wantID: 8,
		},
		{
			name:       "unrelated or low confidence creates a new story",
			candidates: []StoryCandidate{{StoryMember: StoryMember{StoryID: 8}}},
			relations:  []StoryRelation{{StoryID: 8, Relation: "UNRELATED", Confidence: 1}},
			wantID:     0,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			if got := ChooseStory(test.candidates, test.relations); got != test.wantID {
				t.Errorf("ChooseStory() = %d, want %d", got, test.wantID)
			}
		})
	}
}

func BenchmarkRecall(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			members := make([]StoryMember, size)
			for i := range members {
				members[i] = StoryMember{StoryID: int64(i + 1), Title: "OpenAI Atlas model release", Summary: "OpenAI releases a new model with improved research capabilities"}
			}
			b.ReportAllocs()
			for b.Loop() {
				Recall("OpenAI Atlas model release", "OpenAI model research capabilities", members)
			}
		})
	}
}
