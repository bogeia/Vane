package biz

import (
	"cmp"
	"context"
	"math"
	"slices"
	"sync"
	"time"
)

type HotMention struct {
	StoryID, Title string
	Source         ItemSource
	PublishedAt    time.Time
}
type HotStory struct {
	StoryID string
	Title   string
	Heat    int
	Trend   string
	Sources []ItemSource
}
type hotScore struct {
	story HotStory
	raw   float64
}

func hotScores(mentions []HotMention, now time.Time) map[string]hotScore {
	latest := map[string]map[string]HotMention{}
	cutoff := now.Add(-48 * time.Hour)
	for _, mention := range mentions {
		if mention.PublishedAt.Before(cutoff) || mention.PublishedAt.After(now) {
			continue
		}
		if latest[mention.StoryID] == nil {
			latest[mention.StoryID] = map[string]HotMention{}
		}
		old, found := latest[mention.StoryID][mention.Source.ID]
		if !found || mention.PublishedAt.After(old.PublishedAt) {
			latest[mention.StoryID][mention.Source.ID] = mention
		}
	}
	scores := map[string]hotScore{}
	for id, sources := range latest {
		if len(sources) < 2 {
			continue
		}
		score := hotScore{story: HotStory{StoryID: id, Sources: []ItemSource{}}}
		ids := make([]string, 0, len(sources))
		for id := range sources {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		// Stable source order also keeps floating-point summation deterministic.
		for _, sourceID := range ids {
			mention := sources[sourceID]
			score.story.Title = mention.Title
			score.story.Sources = append(score.story.Sources, mention.Source)
			score.raw += math.Pow(0.5, now.Sub(mention.PublishedAt).Hours()/24)
		}
		score.story.Heat = int(math.Round(score.raw * 100))
		scores[id] = score
	}
	return scores
}

func RankHot(mentions []HotMention, now time.Time) []HotStory {
	current, previous := hotScores(mentions, now), hotScores(mentions, now.Add(-6*time.Hour))
	scores := make([]hotScore, 0, len(current))
	for id, score := range current {
		old, listed := previous[id]
		if !listed {
			score.story.Trend = TrendNew
		} else if score.raw >= old.raw*1.2 {
			score.story.Trend = TrendRising
		}
		scores = append(scores, score)
	}
	slices.SortFunc(scores, func(a, b hotScore) int {
		if order := cmp.Compare(b.raw, a.raw); order != 0 {
			return order
		}
		return cmp.Compare(a.story.StoryID, b.story.StoryID)
	})
	result := make([]HotStory, 0, len(scores))
	for _, score := range scores {
		result = append(result, score.story)
	}
	return result
}

type HotRepository interface {
	HotMentions(context.Context, time.Time, time.Time) ([]HotMention, error)
}
type HotUseCase struct {
	repo     HotRepository
	now      func() time.Time
	mu       sync.Mutex
	cachedAt time.Time
	cached   []HotStory
}

func NewHotUseCase(repo HotRepository, now func() time.Time) *HotUseCase {
	return &HotUseCase{repo: repo, now: now}
}

func (h *HotUseCase) List(ctx context.Context) ([]HotStory, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	if !h.cachedAt.IsZero() && !now.Before(h.cachedAt) && now.Sub(h.cachedAt) < 5*time.Minute {
		return h.cached, nil
	}
	mentions, err := h.repo.HotMentions(ctx, now.Add(-54*time.Hour), now)
	if err != nil {
		return nil, err
	}
	h.cached = RankHot(mentions, now)
	h.cachedAt = now
	return h.cached, nil
}
