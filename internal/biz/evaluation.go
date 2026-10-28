package biz

import (
	"context"
	"math"
	"sync"

	"github.com/bogeia/vane/internal/biz/taxonomy"
	"github.com/bogeia/vane/internal/infra/retry"
)

type EvaluationModel interface {
	Prefilter(context.Context, WorkItem) (string, error)
	Score(context.Context, WorkItem) (int, error)
}

type EvaluationMetrics struct {
	Total                   int
	Labels                  map[string]int
	Answered                int
	Failures                int
	Coverage                float64
	Accuracy                float64
	CompleteAccuracy        float64
	Precision               float64
	Recall                  float64
	F1                      float64
	GoldSelectionRate       float64
	PredictionSelectionRate float64
	f1                      float64
}

type ThresholdMetrics struct {
	Threshold               int
	Accuracy                float64
	Precision               float64
	Recall                  float64
	F1                      float64
	PredictionSelectionRate float64
}

type EvaluationMistake struct {
	ID     int64
	Tier   string
	Title  string
	Label  string
	Score1 *int
	Score2 *int
}

type Evaluation struct {
	All                   EvaluationMetrics
	Tiers                 map[string]EvaluationMetrics
	ThresholdScan         map[string][]ThresholdMetrics
	RecommendedThresholds map[string]*int
	Mistakes              []EvaluationMistake
	AverageScoreDelta     *float64
}

type evaluationCase struct {
	item              GoldItem
	available, passed bool
	scores            [2]*int
}

func Evaluate(ctx context.Context, gold GoldSet, model EvaluationModel, cfg EvaluationOptions) (Evaluation, error) {
	cases := make([]evaluationCase, len(gold.Items))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(cfg.Worker.Count, len(gold.Items)) {
		workers.Go(func() {
			for index := range jobs {
				cases[index] = evaluateItem(ctx, gold.Items[index], model, cfg.Worker)
			}
		})
	}
dispatch:
	for index := range gold.Items {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return Evaluation{}, err
	}
	return summarizeEvaluation(cases, cfg.Thresholds), nil
}

func evaluateItem(ctx context.Context, item GoldItem, model EvaluationModel, worker WorkerOptions) evaluationCase {
	result := evaluationCase{item: item}
	input := WorkItem{OriginalTitle: item.OriginalTitle, PublishedAt: item.PublishedAt, Body: item.Body}
	label := ""
	_, err := retry.Do(ctx, worker.Retry, func() (struct{}, error) {
		attempt, cancel := context.WithTimeout(ctx, worker.AttemptTimeout)
		defer cancel()
		if label == "" {
			var err error
			label, err = model.Prefilter(attempt, input)
			if err != nil {
				return struct{}{}, err
			}
		}
		if label == PrefilterBlock {
			return struct{}{}, nil
		}
		for slot := range 2 {
			if result.scores[slot] != nil {
				continue
			}
			score, err := model.Score(attempt, input)
			if err != nil {
				return struct{}{}, err
			}
			result.scores[slot] = &score
		}
		return struct{}{}, nil
	})
	result.available = err == nil
	result.passed = label != PrefilterBlock
	return result
}

func (c evaluationCase) selected(thresholds Thresholds) bool {
	if !c.passed {
		return false
	}
	selected, _ := Selection(c.item.Tier, *c.scores[0], *c.scores[1], thresholds)
	return selected
}

func evaluationMetrics(cases []evaluationCase, thresholds Thresholds) EvaluationMetrics {
	result := EvaluationMetrics{Total: len(cases), Labels: map[string]int{LabelSelect: 0, LabelReject: 0, LabelEither: 0}}
	tp, fp, fn, tn := 0, 0, 0, 0
	for _, c := range cases {
		result.Labels[c.item.Label]++
		if !c.available {
			result.Failures++
		}
		if !c.available || c.item.Label == LabelEither {
			continue
		}
		result.Answered++
		selected := c.selected(thresholds)
		switch {
		case selected && c.item.Label == LabelSelect:
			tp++
		case selected:
			fp++
		case c.item.Label == LabelSelect:
			fn++
		default:
			tn++
		}
	}
	decisive := result.Labels[LabelSelect] + result.Labels[LabelReject]
	result.Coverage = evaluationRatio(result.Answered, decisive)
	result.Accuracy = evaluationRatio(tp+tn, result.Answered)
	result.CompleteAccuracy = evaluationRatio(tp+tn, decisive)
	result.Precision = evaluationRatio(tp, tp+fp)
	result.Recall = evaluationRatio(tp, tp+fn)
	result.f1 = float64(2*tp) / float64(max(1, 2*tp+fp+fn))
	result.F1 = roundEvaluation(result.f1)
	result.GoldSelectionRate = evaluationRatio(result.Labels[LabelSelect], decisive)
	result.PredictionSelectionRate = evaluationRatio(tp+fp, result.Answered)
	return result
}

func evaluationRatio(numerator, denominator int) float64 {
	return roundEvaluation(float64(numerator) / float64(max(1, denominator)))
}
func roundEvaluation(value float64) float64 { return math.Round(value*1000) / 1000 }

func summarizeEvaluation(cases []evaluationCase, thresholds Thresholds) Evaluation {
	result := Evaluation{All: evaluationMetrics(cases, thresholds), Tiers: map[string]EvaluationMetrics{}, ThresholdScan: map[string][]ThresholdMetrics{}, RecommendedThresholds: map[string]*int{}, Mistakes: []EvaluationMistake{}}
	for _, tier := range []string{taxonomy.TierOfficial, taxonomy.TierMedia} {
		var tierCases []evaluationCase
		for _, c := range cases {
			if c.item.Tier == tier {
				tierCases = append(tierCases, c)
			}
		}
		result.Tiers[tier] = evaluationMetrics(tierCases, thresholds)
		current := map[string]int{taxonomy.TierOfficial: thresholds.Official, taxonomy.TierMedia: thresholds.Media}[tier]
		var recommended *int
		best := -1.0
		for threshold := 40; threshold <= 90; threshold += 2 {
			metric := evaluationMetrics(tierCases, Thresholds{Official: threshold, Media: threshold})
			result.ThresholdScan[tier] = append(result.ThresholdScan[tier], ThresholdMetrics{threshold, metric.Accuracy, metric.Precision, metric.Recall, metric.F1, metric.PredictionSelectionRate})
			if metric.Labels[LabelSelect] > 0 && (metric.f1 > best || metric.f1 == best && recommended != nil && math.Abs(float64(threshold-current)) < math.Abs(float64(*recommended-current))) {
				recommended = &threshold
				best = metric.f1
			}
		}
		result.RecommendedThresholds[tier] = recommended
	}
	delta, count := 0.0, 0
	for _, c := range cases {
		if !c.available {
			continue
		}
		if c.item.Label != LabelEither && c.selected(thresholds) != (c.item.Label == LabelSelect) {
			result.Mistakes = append(result.Mistakes, EvaluationMistake{c.item.ID, c.item.Tier, c.item.Title, c.item.Label, c.scores[0], c.scores[1]})
		}
		if c.scores[0] != nil && c.scores[1] != nil {
			delta += float64(*c.scores[0]+*c.scores[1]-c.item.Score1-c.item.Score2) / 2
			count++
		}
	}
	if count > 0 {
		average := roundEvaluation(delta / float64(count))
		result.AverageScoreDelta = &average
	}
	return result
}

type EvaluationOptions struct {
	Worker     WorkerOptions
	Thresholds Thresholds
}
