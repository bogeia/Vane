package biz

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

type StoryMember struct {
	StoryID int64
	Title   string
	Summary string
}
type StoryCandidate struct {
	StoryMember
	Similarity float64
}

func tokens(title, summary string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, text := range []string{title, summary} {
		runes := []rune(strings.ToLower(text))
		for i := 0; i < len(runes); {
			start := i
			if unicode.Is(unicode.Han, runes[i]) {
				for i < len(runes) && unicode.Is(unicode.Han, runes[i]) {
					i++
				}
				for j := start; j+1 < i; j++ {
					result[string(runes[j:j+2])] = struct{}{}
				}
			} else if runes[i] >= 'a' && runes[i] <= 'z' {
				for i < len(runes) && runes[i] >= 'a' && runes[i] <= 'z' {
					i++
				}
				result[string(runes[start:i])] = struct{}{}
			} else {
				i++
			}
		}
	}
	return result
}

func Similarity(titleA, summaryA, titleB, summaryB string) float64 {
	a, b := tokens(titleA, summaryA), tokens(titleB, summaryB)
	intersection := 0
	for token := range a {
		if _, ok := b[token]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func Recall(title, summary string, members []StoryMember) []StoryCandidate {
	best := map[int64]StoryCandidate{}
	for _, member := range members {
		score := Similarity(title, summary, member.Title, member.Summary)
		if score >= 0.1 && score > best[member.StoryID].Similarity {
			best[member.StoryID] = StoryCandidate{member, score}
		}
	}
	result := make([]StoryCandidate, 0, len(best))
	for _, candidate := range best {
		result = append(result, candidate)
	}
	slices.SortFunc(result, func(a, b StoryCandidate) int {
		if order := cmp.Compare(b.Similarity, a.Similarity); order != 0 {
			return order
		}
		return cmp.Compare(a.StoryID, b.StoryID)
	})
	return result[:min(len(result), 5)]
}

func Representative(items []Item) Item {
	var best Item
	for _, item := range items {
		if item.Roundup {
			continue
		}
		if best.ID == "" {
			best = item
			continue
		}
		better := false
		switch {
		case (item.Source.Tier == taxonomy.TierOfficial) != (best.Source.Tier == taxonomy.TierOfficial):
			better = item.Source.Tier == taxonomy.TierOfficial
		case item.Score != best.Score:
			better = item.Score > best.Score
		case !item.PublishedAt.Equal(best.PublishedAt):
			better = item.PublishedAt.Before(best.PublishedAt)
		default:
			better = item.ID < best.ID
		}
		if better {
			best = item
		}
	}
	return best
}

type StoryRelation struct {
	StoryID    int64
	Relation   string
	Confidence float64
}

func ChooseStory(candidates []StoryCandidate, relations []StoryRelation) int64 {
	byID := map[int64]StoryRelation{}
	for _, relation := range relations {
		byID[relation.StoryID] = relation
	}
	for _, candidate := range candidates {
		if byID[candidate.StoryID].Relation == RelationRoundup {
			return 0
		}
	}
	for _, kind := range []string{RelationSameOccurrence, RelationSameStory} {
		for _, candidate := range candidates {
			relation := byID[candidate.StoryID]
			if relation.Relation == kind && relation.Confidence >= 0.6 {
				return candidate.StoryID
			}
		}
	}
	return 0
}

type GroupingPlan struct {
	PreviousStage  string
	Stage          string
	Since, Until   time.Time
	Topics         []string
	MaxAttempts    int
	Recall         func([]StoryMember) []StoryCandidate
	Choose         func(context.Context, []StoryCandidate) (int64, error)
	Representative func([]Item) Item
}

func PlanGrouping(item WorkItem, now time.Time, choose func(context.Context, []StoryCandidate) (int64, error)) GroupingPlan {
	return GroupingPlan{
		Stage: StageDone, PreviousStage: item.Stage,
		Since: now.Add(-14 * 24 * time.Hour), Until: now, MaxAttempts: 3,
		Topics: taxonomy.AssignTopics(item.Writing.Title, item.OriginalTitle, item.Writing.Subjects, item.Writing.Tags, item.Writing.Category),
		Recall: func(members []StoryMember) []StoryCandidate {
			return Recall(item.Writing.Title, item.Writing.Summary, members)
		},
		Choose: choose,
		Representative: func(items []Item) Item {
			for index := range items {
				items[index] = CompleteItem(items[index])
			}
			return Representative(items)
		},
	}
}
