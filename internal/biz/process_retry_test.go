package biz

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/infra/retry"
)

type failingWriter struct{ err error }

func (w failingWriter) Prefilter(context.Context, WorkItem) (string, error) { return "", w.err }
func (w failingWriter) Score(context.Context, WorkItem) (int, error)        { panic("unexpected score") }
func (w failingWriter) Write(context.Context, WorkItem) (Writing, error)    { panic("unexpected write") }
func (w failingWriter) Group(context.Context, WorkItem, []StoryCandidate) ([]StoryRelation, error) {
	panic("unexpected group")
}

func TestProcessorPermanentFailureAndBoundedRetry(t *testing.T) {
	now := time.Now()
	cfg := WorkerOptions{Count: 1, AttemptTimeout: time.Second, Retry: retry.Policy{MaxAttempts: 5, InitialInterval: 2 * time.Minute, MaxInterval: 16 * time.Minute}}
	for _, test := range []struct {
		name     string
		err      error
		attempts int
		stage    string
	}{
		{"permanent", retry.Permanent(fmt.Errorf("HTTP 401")), 0, "failed"},
		{"invalid JSON", fmt.Errorf("invalid writing JSON"), 0, "new"},
		{"exhausted", fmt.Errorf("temporary"), 4, "failed"},
		{"canceled", context.Canceled, 0, "new"},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := NewProcessor(nil, failingWriter{test.err}, ProcessorOptions{Enabled: true, Worker: cfg, Thresholds: Thresholds{}}, func() time.Time { return now }, slog.Default())
			got, err := p.process(t.Context(), WorkItem{Stage: StageNew, Attempts: test.attempts}, func(StepResult) error { return nil })
			if err == nil || got.Stage != test.stage || got.Attempts != test.attempts+1 {
				t.Fatalf("result=%+v err=%v", got, err)
			}
			if test.attempts == 0 && got.NextAttemptAt != now.Add(2*time.Minute) {
				t.Fatal(got.NextAttemptAt)
			}
		})
	}
}

func TestProcessorPersistsRetryAfterNotBefore(t *testing.T) {
	now := time.Now()
	failure := retry.HTTPError(fmt.Errorf("busy"), 429, http.Header{"Retry-After": []string{"600"}})
	cfg := WorkerOptions{Count: 1, AttemptTimeout: time.Second, Retry: retry.Policy{MaxAttempts: 5, InitialInterval: time.Minute, MaxInterval: 16 * time.Minute}}
	p := NewProcessor(nil, failingWriter{failure}, ProcessorOptions{Enabled: true, Worker: cfg, Thresholds: Thresholds{}}, func() time.Time { return now }, slog.Default())
	result, err := p.process(t.Context(), WorkItem{Stage: StageNew}, func(StepResult) error { return nil })
	if err == nil || result.Stage != "new" || result.NextAttemptAt.Before(retry.NotBefore(failure)) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

type processQueueFake struct {
	remaining  int
	err        error
	beforeNext func()
}

func (f *processQueueFake) ProcessNext(context.Context, time.Time, func(context.Context, WorkItem, func(StepResult) error) (ProcessResult, error)) (bool, error) {
	if f.beforeNext != nil {
		f.beforeNext()
	}
	if f.remaining == 0 {
		return false, nil
	}
	f.remaining--
	return true, f.err
}

func (*processQueueFake) Group(ctx context.Context, _ WorkItem, _ time.Time, plan GroupingPlan) error {
	_, err := plan.Choose(ctx, nil)
	return err
}

func TestProcessorLogsEachFailureAndBoundsSummary(t *testing.T) {
	var logs bytes.Buffer
	failure := errors.New("queue failure")
	repo := &processQueueFake{remaining: 1000, err: failure}
	repo.beforeNext = func() {
		if got := strings.Count(logs.String(), "process item failed"); got != 1000-repo.remaining {
			t.Fatalf("logs=%d before processing next item", got)
		}
	}
	p := NewProcessor(repo, nil, ProcessorOptions{Enabled: true, Worker: WorkerOptions{Count: 1}, Thresholds: Thresholds{}}, time.Now, slog.New(slog.NewTextHandler(&logs, nil)))
	err := p.RunOnce(t.Context())
	if !errors.Is(err, failure) || len(err.Error()) > 100 || !strings.Contains(err.Error(), "1000 processing failures") {
		t.Fatalf("summary=%v", err)
	}
}

func TestProcessorSkipsModelForEmptyCandidates(t *testing.T) {
	score := 80
	item := WorkItem{Stage: StageAnalyzed, Prefilter: "PASS", Score1: &score, Score2: &score, Writing: &Writing{Title: "Ready", Summary: "Ready"}}
	cfg := WorkerOptions{Count: 1, AttemptTimeout: time.Second, Retry: retry.Policy{MaxAttempts: 3}}
	p := NewProcessor(&processQueueFake{}, failingWriter{}, ProcessorOptions{Enabled: true, Worker: cfg, Thresholds: Thresholds{}}, time.Now, slog.Default())
	result, err := p.process(t.Context(), item, func(StepResult) error { t.Fatal("unexpected save"); return nil })
	if err != nil || result.Stage != "done" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}
