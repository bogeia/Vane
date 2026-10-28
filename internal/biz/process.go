package biz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bogeia/vane/internal/infra/retry"
)

type Writer interface {
	Write(context.Context, WorkItem) (Writing, error)
	Prefilter(context.Context, WorkItem) (string, error)
	Score(context.Context, WorkItem) (int, error)
	Group(context.Context, WorkItem, []StoryCandidate) ([]StoryRelation, error)
}
type ProcessResult struct {
	Stage, LastError string
	PreviousStage    string
	Attempts         int
	NextAttemptAt    time.Time
}
type WorkRepository interface {
	Group(context.Context, WorkItem, time.Time, GroupingPlan) error
	ProcessNext(context.Context, time.Time, func(context.Context, WorkItem, func(StepResult) error) (ProcessResult, error)) (bool, error)
}
type Processor struct {
	repo       WorkRepository
	writer     Writer
	enabled    bool
	count      int
	now        func() time.Time
	cfg        WorkerOptions
	thresholds Thresholds
	log        *slog.Logger
}

func NewProcessor(repo WorkRepository, writer Writer, options ProcessorOptions, now func() time.Time, log *slog.Logger) *Processor {
	return &Processor{repo: repo, writer: writer, enabled: options.Enabled, count: options.Worker.Count, now: now, cfg: options.Worker, thresholds: options.Thresholds, log: log}
}

func (p *Processor) RunOnce(ctx context.Context) error {
	if !p.enabled {
		return nil
	}
	type outcome struct {
		count int
		last  error
	}
	failures := make(chan outcome, p.count)
	var workers sync.WaitGroup
	for range p.count {
		workers.Go(func() {
			var failure outcome
			defer func() { failures <- failure }()
			for {
				processed, err := p.repo.ProcessNext(ctx, p.now(), p.process)
				if err != nil {
					failure.count++
					failure.last = err
					p.log.ErrorContext(ctx, "process item failed", "error", err)
				}
				if !processed || ctx.Err() != nil {
					return
				}
			}
		})
	}
	workers.Wait()
	close(failures)
	var total outcome
	for failure := range failures {
		total.count += failure.count
		if failure.last != nil {
			total.last = failure.last
		}
	}
	if total.count > 0 {
		return fmt.Errorf("%d processing failures; last: %w", total.count, total.last)
	}
	return nil
}

// StepResult is committed independently, so a failed sibling does not erase success.
type StepResult struct {
	Stage         string
	PreviousStage string
	Prefilter     string
	Score         *int
	ScoreSlot     int
	Writing       *Writing
}

func (p *Processor) process(ctx context.Context, item WorkItem, save func(StepResult) error) (ProcessResult, error) {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.AttemptTimeout)
	defer cancel()
	var failure error
	if item.LoadError != nil {
		failure = retry.Permanent(item.LoadError)
	}
	var saveMu sync.Mutex
	commit := func(step StepResult) error {
		saveMu.Lock()
		defer saveMu.Unlock()
		next := item
		step.PreviousStage = item.Stage
		step.Stage = item.Stage
		var event string
		if step.Prefilter != "" {
			next.Prefilter = step.Prefilter
			event = StageEventPass
			if step.Prefilter == PrefilterBlock {
				event = StageEventBlock
			}
		}
		if step.Score != nil {
			if step.ScoreSlot == 1 {
				next.Score1 = step.Score
			} else {
				next.Score2 = step.Score
			}
		}
		if step.Writing != nil {
			next.Writing = step.Writing
		}
		if event == "" && next.Stage == StagePrefiltered && next.Score1 != nil && next.Score2 != nil && next.Writing != nil {
			event = StageEventAnalyze
		}
		if event != "" {
			stage, err := TransitionStage(item.Stage, event)
			if err != nil {
				return err
			}
			step.Stage = stage
		}
		if err := save(step); err != nil {
			return err
		}
		next.Stage = step.Stage
		item = next
		return nil
	}
	if failure == nil && item.Prefilter == "" {
		label, err := p.writer.Prefilter(ctx, item)
		if err == nil {
			err = commit(StepResult{Prefilter: label})
		}
		failure = err
	}
	if failure == nil && item.Prefilter == PrefilterBlock {
		return ProcessResult{PreviousStage: item.Stage, Stage: item.Stage, Attempts: item.Attempts}, nil
	}
	if failure == nil && item.Stage == StagePrefiltered && item.Score1 != nil && item.Score2 != nil && item.Writing != nil {
		failure = commit(StepResult{})
	}
	if failure == nil {
		input := item
		results := make(chan error, 3)
		var steps sync.WaitGroup
		for slot := range 2 {
			if (slot == 0 && input.Score1 != nil) || (slot == 1 && input.Score2 != nil) {
				continue
			}
			steps.Go(func() {
				score, err := p.writer.Score(ctx, input)
				if err == nil {
					err = commit(StepResult{Score: &score, ScoreSlot: slot + 1})
				}
				results <- err
			})
		}
		if input.Writing == nil {
			steps.Go(func() {
				writing, err := p.writer.Write(ctx, input)
				if errors.Is(err, ErrInvalidCategory) && input.Attempts+1 >= p.cfg.Retry.MaxAttempts {
					err = nil
				}
				if err == nil {
					err = commit(StepResult{Writing: &writing})
				}
				results <- err
			})
		}
		steps.Wait()
		close(results)
		for err := range results {
			failure = errors.Join(failure, err)
		}
	}

	if failure == nil {
		item.Selected, _ = Selection(item.Tier, *item.Score1, *item.Score2, p.thresholds)
		item.Selected = item.Selected && !item.Writing.Roundup
		groupNow := p.now()
		publishStage, err := TransitionStage(item.Stage, StageEventPublish)
		if err != nil {
			failure = err
		} else {
			failure = p.repo.Group(ctx, item, groupNow, PlanGrouping(item, groupNow, func(ctx context.Context, candidates []StoryCandidate) (int64, error) {
				if item.StoryID != 0 {
					return item.StoryID, nil
				}
				if item.Writing.Roundup || len(candidates) == 0 {
					return 0, nil
				}
				relations, err := p.writer.Group(ctx, item, candidates)
				if err != nil {
					return 0, err
				}
				return ChooseStory(candidates, relations), nil
			}))
			if failure == nil {
				item.Stage = publishStage
			}
		}
	}
	now := p.now()
	result := ProcessResult{PreviousStage: item.Stage, Stage: item.Stage, Attempts: item.Attempts, NextAttemptAt: now}
	if failure != nil {
		result.Attempts++
		if result.Attempts >= p.cfg.Retry.MaxAttempts || retry.IsPermanent(failure) {
			stage, err := TransitionStage(item.Stage, StageEventFail)
			if err != nil {
				failure = errors.Join(failure, err)
			} else {
				result.Stage = stage
			}
		}
		result.LastError = failure.Error()
		result.NextAttemptAt = now.Add(retry.Delay(p.cfg.Retry, result.Attempts))
		if until := retry.NotBefore(failure); until.After(result.NextAttemptAt) {
			result.NextAttemptAt = until
		}
	}
	return result, failure
}
