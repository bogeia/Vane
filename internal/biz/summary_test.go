package biz

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/infra/retry"
)

type summaryWriter struct {
	failingWriter
	calls   int
	summary string
	err     error
	cancel  context.CancelFunc
}

func (w *summaryWriter) EnhanceSummary(context.Context, WorkItem) (string, error) {
	w.calls++
	if w.cancel != nil {
		w.cancel()
	}
	return w.summary, w.err
}

type summaryRepo struct {
	processQueueFake
	grouped  WorkItem
	groupErr error
	calls    int
}

func (r *summaryRepo) Group(_ context.Context, item WorkItem, _ time.Time, _ GroupingPlan) error {
	r.calls++
	r.grouped = item
	return r.groupErr
}

func summaryItem() WorkItem {
	score := 90
	return WorkItem{ID: 42, Stage: StageAnalyzed, Prefilter: PrefilterPass, Tier: "official", Score1: &score, Score2: &score, Writing: &Writing{Title: "研究提出新方法", Summary: "研究通过对照实验验证一种新的训练方法。", Category: "论文", Reason: "提供可复用的研究结果", Tags: []string{"推理"}, Subjects: []string{}, Fact: &Fact{Title: "研究提出训练方法", Subject: "研究团队", Action: "提出", Object: "训练方法"}}}
}

func summaryProcessor(repo WorkRepository, writer Writer) *Processor {
	return NewProcessor(repo, writer, ProcessorOptions{Worker: WorkerOptions{AttemptTimeout: time.Second, Retry: retry.Policy{MaxAttempts: 3}}, Thresholds: Thresholds{Official: 70, Media: 70}}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestSummaryType(t *testing.T) {
	for _, test := range []struct {
		name, category, action, want string
		selected, roundup            bool
	}{
		{"product release", "产品", "发布", SummaryProductRelease, true, false},
		{"product update", "产品", "更新", SummaryProductRelease, true, false},
		{"not released", "产品", "尚未发布", "", true, false},
		{"cancelled launch", "产品", "取消上线", "", true, false},
		{"stopped updates", "产品", "停止更新", "", true, false},
		{"expected release", "产品", "预计发布", "", true, false},
		{"planned release", "产品", "plans to release", "", true, false},
		{"product usage", "产品", "评测", "", true, false},
		{"product without fact", "产品", "", "", true, false},
		{"paper", "论文", "", SummaryPaper, true, false},
		{"tutorial", "教程", "", SummaryTutorial, true, false},
		{"model", "模型", "发布", "", true, false},
		{"opinion", "观点", "", "", true, false},
		{"unknown", "", "", "", true, false},
		{"not selected", "论文", "", "", false, false},
		{"roundup", "论文", "", "", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := summaryItem()
			item.Selected = test.selected
			item.Writing.Category = test.category
			item.Writing.Roundup = test.roundup
			item.Writing.Fact = nil
			if test.action != "" {
				item.Writing.Fact = &Fact{Action: test.action}
			}
			if got := SummaryType(item); got != test.want {
				t.Fatalf("type=%q want %q", got, test.want)
			}
		})
	}
}

func TestProcessorSummaryCheckpointAndFallback(t *testing.T) {
	for _, test := range []struct {
		name, summary string
		err           error
		enhanced      bool
	}{
		{"success", "研究解决训练稳定性问题，通过对照实验验证新方法，并报告了实验条件。", nil, true},
		{"missing information", "", nil, false},
		{"timeout", "", context.DeadlineExceeded, false},
		{"failed", "", errors.New("private upstream detail"), false},
		{"refusal", "抱歉，我无法处理这篇资料", nil, false},
		{"degenerate", "研究提出新方法", nil, false},
		{"repeated", "结果结果结果结果", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := summaryItem()
			base := *item.Writing
			writer := &summaryWriter{summary: test.summary, err: test.err}
			repo := &summaryRepo{groupErr: errors.New("retry grouping")}
			p := summaryProcessor(repo, writer)
			var saved StepResult
			_, err := p.process(t.Context(), item, func(step StepResult) error { saved = step; return nil })
			if err == nil || writer.calls != 1 || !saved.SummaryEnhancementDone || saved.BaseSummary != base.Summary {
				t.Fatalf("checkpoint=%+v calls=%d err=%v", saved, writer.calls, err)
			}
			want := base
			if test.enhanced {
				want.Summary = test.summary
			}
			if !reflect.DeepEqual(*saved.Writing, want) || !reflect.DeepEqual(*repo.grouped.Writing, want) {
				t.Fatalf("writing changed beyond summary or grouping used old summary: %+v", saved.Writing)
			}
			item.Writing, item.BaseSummary, item.SummaryEnhancementDone = saved.Writing, saved.BaseSummary, true
			repo.groupErr = nil
			result, err := p.process(t.Context(), item, func(StepResult) error { t.Fatal("checkpoint repeated"); return nil })
			if err != nil || result.Stage != StageDone || writer.calls != 1 {
				t.Fatalf("retry result=%+v calls=%d err=%v", result, writer.calls, err)
			}
		})
	}
}

func TestProcessorSummaryCancellationAndSaveFailure(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		writer := &summaryWriter{summary: "研究验证了改进训练流程的有效方法。", cancel: cancel}
		repo := &summaryRepo{}
		_, err := summaryProcessor(repo, writer).process(ctx, summaryItem(), func(StepResult) error { t.Fatal("saved after cancellation"); return nil })
		if !errors.Is(err, context.Canceled) || repo.calls != 0 {
			t.Fatalf("groups=%d err=%v", repo.calls, err)
		}
	})
	t.Run("save failure", func(t *testing.T) {
		repo := &summaryRepo{}
		saveErr := errors.New("checkpoint unavailable")
		_, err := summaryProcessor(repo, &summaryWriter{}).process(t.Context(), summaryItem(), func(StepResult) error { return saveErr })
		if !errors.Is(err, saveErr) || repo.calls != 0 {
			t.Fatalf("groups=%d err=%v", repo.calls, err)
		}
	})
}

func TestProcessorSkipsUnsupportedSummary(t *testing.T) {
	for _, test := range []struct {
		name, category string
		score          int
		roundup        bool
	}{
		{"other", "行业", 90, false}, {"model", "模型", 90, false}, {"unknown", "", 90, false}, {"not selected", "论文", 10, false}, {"roundup", "论文", 90, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := summaryItem()
			item.Writing.Category = test.category
			item.Writing.Roundup = test.roundup
			item.Score1, item.Score2 = &test.score, &test.score
			writer := &summaryWriter{}
			repo := &summaryRepo{}
			result, err := summaryProcessor(repo, writer).process(t.Context(), item, func(StepResult) error { t.Fatal("unexpected enhancement checkpoint"); return nil })
			if err != nil || result.Stage != StageDone || writer.calls != 0 {
				t.Fatalf("result=%+v calls=%d err=%v", result, writer.calls, err)
			}
		})
	}
}

func TestProcessorEnhancesThreeSummaryTypes(t *testing.T) {
	for _, category := range []string{"产品", "论文", "教程"} {
		t.Run(category, func(t *testing.T) {
			item := summaryItem()
			item.Writing.Category = category
			item.Writing.Fact.Action = "发布"
			writer := &summaryWriter{summary: "资料说明新方案解决的问题、适用条件以及原文报告的主要结果。"}
			repo := &summaryRepo{}
			result, err := summaryProcessor(repo, writer).process(t.Context(), item, func(step StepResult) error {
				if !step.SummaryEnhancementDone || step.BaseSummary != item.Writing.Summary {
					t.Fatal("missing checkpoint")
				}
				return nil
			})
			if err != nil || result.Stage != StageDone || writer.calls != 1 || repo.grouped.Writing.Summary != writer.summary {
				t.Fatalf("result=%+v calls=%d err=%v", result, writer.calls, err)
			}
		})
	}
}
