package integration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/infra/retry"
)

type stageWriter struct {
	calls       atomic.Int32
	prefilter   func(context.Context) (string, error)
	beforeSave  func()
	beforeScore func()
	beforeWrite func()
	writing     func() (biz.Writing, error)
	group       func(context.Context, biz.WorkItem, []biz.StoryCandidate) ([]biz.StoryRelation, error)
}

func (w *stageWriter) Prefilter(ctx context.Context, _ biz.WorkItem) (string, error) {
	w.calls.Add(1)
	if w.prefilter != nil {
		return w.prefilter(ctx)
	}
	if w.beforeSave != nil {
		w.beforeSave()
	}
	return biz.PrefilterPass, nil
}

func (w *stageWriter) Score(context.Context, biz.WorkItem) (int, error) {
	w.calls.Add(1)
	if w.beforeScore != nil {
		w.beforeScore()
	}
	return 80, nil
}

func (w *stageWriter) Write(context.Context, biz.WorkItem) (biz.Writing, error) {
	if w.writing != nil {
		return w.writing()
	}
	w.calls.Add(1)
	if w.beforeWrite != nil {
		w.beforeWrite()
	}
	return biz.Writing{Title: "OpenAI Atlas release", Summary: "OpenAI Atlas release details", Tags: []string{}, Subjects: []string{}}, nil
}

func (w *stageWriter) Group(ctx context.Context, item biz.WorkItem, candidates []biz.StoryCandidate) ([]biz.StoryRelation, error) {
	return w.group(ctx, item, candidates)
}

func stageProcessor(store *data.Data, writer biz.Writer, now time.Time, logger *slog.Logger) *biz.Processor {
	return biz.NewProcessor(data.NewWorkRepo(store), writer, biz.ProcessorOptions{Enabled: true, Worker: biz.WorkerOptions{Count: 1, AttemptTimeout: time.Second * 5, Retry: retry.Policy{MaxAttempts: 3, InitialInterval: time.Minute, MaxInterval: time.Minute}}}, func() time.Time { return now }, logger)
}

func TestProcessingStageIsAnalyzedBeforeGrouping(t *testing.T) {
	for _, completed := range []bool{false, true} {
		name := "last-model-step"
		if completed {
			name = "resume-prefiltered-complete"
		}
		t.Run(name, func(t *testing.T) {
			store, now := groupStore(t)
			seed := groupItem(t, store, now.Add(-time.Hour), 1)
			repo := data.NewWorkRepo(store)
			if err := repo.Group(t.Context(), seed, now, biz.PlanGrouping(seed, now, func(context.Context, []biz.StoryCandidate) (int64, error) { return 0, nil })); err != nil {
				t.Fatal(err)
			}
			item := groupItem(t, store, now, 2)
			query := `UPDATE items SET stage='prefiltered',prefilter='PASS' WHERE id=$1`
			if !completed {
				query = `UPDATE items SET stage='prefiltered',prefilter='PASS',score1=NULL,score2=NULL,title='',summary='' WHERE id=$1`
			}
			if _, err := testDB(t, store).ExecContext(t.Context(), query, item.ID); err != nil {
				t.Fatal(err)
			}
			writer := &stageWriter{}
			writer.group = func(ctx context.Context, item biz.WorkItem, candidates []biz.StoryCandidate) ([]biz.StoryRelation, error) {
				var stage string
				if err := testDB(t, store).QueryRowContext(ctx, `SELECT stage FROM items WHERE id=$1`, item.ID).Scan(&stage); err != nil {
					return nil, err
				}
				if stage != "analyzed" {
					t.Errorf("stage before grouping=%q, want analyzed", stage)
				}
				return []biz.StoryRelation{{StoryID: candidates[0].StoryID, Relation: biz.RelationSameOccurrence, Confidence: 1}}, nil
			}
			if err := stageProcessor(store, writer, now, slog.New(slog.NewTextHandler(io.Discard, nil))).RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			var stage string
			if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT stage FROM items WHERE id=$1`, item.ID).Scan(&stage); err != nil {
				t.Fatal(err)
			}
			if stage != "done" {
				t.Fatalf("published stage=%q", stage)
			}
			want := int32(3)
			if completed {
				want = 0
			}
			if writer.calls.Load() != want {
				t.Fatalf("completed model steps called %d times, want %d", writer.calls.Load(), want)
			}
		})
	}
}

func TestProcessingStageCASPreservesExternalTerminal(t *testing.T) {
	for _, boundary := range []string{"prefilter", "score1", "score2", "writing", "group"} {
		t.Run(boundary, func(t *testing.T) {
			store, now := groupStore(t)
			seed := groupItem(t, store, now.Add(-time.Hour), 1)
			if err := data.NewWorkRepo(store).Group(t.Context(), seed, now, biz.PlanGrouping(seed, now, func(context.Context, []biz.StoryCandidate) (int64, error) { return 0, nil })); err != nil {
				t.Fatal(err)
			}
			item := groupItem(t, store, now, 2)
			query := `UPDATE items SET prefilter='PASS' WHERE id=$1`
			switch boundary {
			case "prefilter":
				query = `UPDATE items SET stage='new' WHERE id=$1`
			case "score1":
				query = `UPDATE items SET stage='prefiltered',prefilter='PASS',score1=NULL WHERE id=$1`
			case "score2":
				query = `UPDATE items SET stage='prefiltered',prefilter='PASS',score2=NULL WHERE id=$1`
			case "writing":
				query = `UPDATE items SET stage='prefiltered',prefilter='PASS',title='',summary='' WHERE id=$1`
			}
			if _, err := testDB(t, store).ExecContext(t.Context(), query, item.ID); err != nil {
				t.Fatal(err)
			}

			change := func() {
				if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET stage='blocked' WHERE id=$1`, item.ID); err != nil {
					t.Error(err)
				}
			}
			writer := &stageWriter{}
			if boundary == "prefilter" {
				writer.beforeSave = change
			}
			if boundary == "score1" || boundary == "score2" {
				writer.beforeScore = change
			}
			if boundary == "writing" {
				writer.beforeWrite = change
			}
			writer.group = func(context.Context, biz.WorkItem, []biz.StoryCandidate) ([]biz.StoryRelation, error) {
				change()
				return nil, nil
			}
			var logs strings.Builder
			// Capture data's conflict log as well as the processor's failure log.
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			old := slog.Default()
			slog.SetDefault(logger)
			t.Cleanup(func() { slog.SetDefault(old) })
			err := stageProcessor(store, writer, now, logger).RunOnce(t.Context())
			if !errors.Is(err, biz.ErrStageConflict) {
				t.Fatalf("error=%v, want stage conflict", err)
			}
			var stage string
			var attempts int
			var storyID *int64
			if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT stage,attempts,story_id FROM items WHERE id=$1`, item.ID).Scan(&stage, &attempts, &storyID); err != nil {
				t.Fatal(err)
			}
			if stage != "blocked" || attempts != 0 || storyID != nil {
				t.Fatalf("external terminal overwritten: stage=%s attempts=%d story=%v", stage, attempts, storyID)
			}
			if !strings.Contains(logs.String(), "compare-and-swap failed") {
				t.Fatal("missing stage conflict error log")
			}
		})
	}
}

func TestPublishedItemSurvivesAccountingFailure(t *testing.T) {
	store, now := groupStore(t)
	item := groupItem(t, store, now, 1)
	if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET prefilter='PASS' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `CREATE FUNCTION reject_published_accounting() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.stage='done' THEN RAISE EXCEPTION 'accounting fixture failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_published_accounting BEFORE UPDATE ON items FOR EACH ROW EXECUTE FUNCTION reject_published_accounting()`); err != nil {
		t.Fatal(err)
	}
	err := stageProcessor(store, &stageWriter{}, now, slog.New(slog.NewTextHandler(io.Discard, nil))).RunOnce(t.Context())
	if err == nil {
		t.Fatal("accounting failure was not reported")
	}
	var stage string
	var published bool
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT stage,story_id IS NOT NULL AND processed_at IS NOT NULL AND selected FROM items WHERE id=$1`, item.ID).Scan(&stage, &published); err != nil {
		t.Fatal(err)
	}
	if stage != "done" || !published {
		t.Fatalf("published item changed: stage=%s published=%v", stage, published)
	}
}

func TestCorruptStoredFactFailsOnlyAffectedItem(t *testing.T) {
	store, now := groupStore(t)
	broken := groupItem(t, store, now, 1)
	healthy := groupItem(t, store, now, 2)
	if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET prefilter='PASS',fact=CASE WHEN id=$1 THEN '[]'::jsonb ELSE fact END`, broken.ID); err != nil {
		t.Fatal(err)
	}
	writer := &stageWriter{}
	err := stageProcessor(store, writer, now, slog.New(slog.NewTextHandler(io.Discard, nil))).RunOnce(t.Context())
	if err == nil {
		t.Fatal("corrupt stored fact was not reported")
	}
	for _, test := range []struct {
		id    int64
		stage string
	}{{broken.ID, "failed"}, {healthy.ID, "done"}} {
		var stage string
		if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT stage FROM items WHERE id=$1`, test.id).Scan(&stage); err != nil {
			t.Fatal(err)
		}
		if stage != test.stage {
			t.Fatalf("item %d stage=%q, want %s", test.id, stage, test.stage)
		}
	}
	var lastError string
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT last_error FROM items WHERE id=$1`, broken.ID).Scan(&lastError); err != nil {
		t.Fatal(err)
	}
	if lastError != "stored fact is invalid" {
		t.Fatalf("last_error=%q", lastError)
	}
	if writer.calls.Load() != 0 {
		t.Fatal("completed steps repeated")
	}
}

func TestTerminalProcessingStagesNeverCallModels(t *testing.T) {
	store, now := groupStore(t)
	for n, stage := range []string{biz.StageDone, biz.StageBlocked, biz.StageFailed} {
		item := groupItem(t, store, now, n+1)
		if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET stage=$2,prefilter=NULL,score1=NULL,score2=NULL,title='',summary='',attempts=1,last_error='retained' WHERE id=$1`, item.ID, stage); err != nil {
			t.Fatal(err)
		}
	}
	writer := &stageWriter{}
	for round := range 3 {
		if err := stageProcessor(store, writer, now.Add(time.Duration(round)*time.Hour), slog.New(slog.NewTextHandler(io.Discard, nil))).RunOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if writer.calls.Load() != 0 {
		t.Fatalf("terminal items called models %d times", writer.calls.Load())
	}
	var unchanged int
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT count(*) FROM items WHERE attempts=1 AND last_error='retained'`).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged != 3 {
		t.Fatalf("terminal accounting changed: unchanged=%d", unchanged)
	}
}

func TestScoreSaveFailureRetriesOnlyUnsavedScores(t *testing.T) {
	store, now := groupStore(t)
	item := groupItem(t, store, now, 1)
	if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET stage='prefiltered',prefilter='PASS',score1=NULL,score2=NULL,title='',summary='' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `CREATE FUNCTION reject_score_save() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'score save fixture failure'; END $$; CREATE TRIGGER reject_score_save BEFORE UPDATE OF score1 ON items FOR EACH ROW EXECUTE FUNCTION reject_score_save()`); err != nil {
		t.Fatal(err)
	}
	writer := &stageWriter{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := stageProcessor(store, writer, now, logger).RunOnce(t.Context()); err == nil {
		t.Fatal("score persistence failure was not reported")
	}
	var stage, title string
	var attempts int
	var first, second *int
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT stage,title,attempts,score1,score2 FROM items WHERE id=$1`, item.ID).Scan(&stage, &title, &attempts, &first, &second); err != nil {
		t.Fatal(err)
	}
	if stage != biz.StagePrefiltered || title == "" || attempts != 1 || first != nil || second == nil {
		t.Fatalf("saved siblings lost: stage=%s title=%q attempts=%d scores=%v,%v", stage, title, attempts, first, second)
	}
	if writer.calls.Load() != 3 {
		t.Fatalf("first round calls=%d", writer.calls.Load())
	}
	if _, err := testDB(t, store).ExecContext(t.Context(), `DROP TRIGGER reject_score_save ON items`); err != nil {
		t.Fatal(err)
	}
	if err := stageProcessor(store, writer, now.Add(time.Minute), logger).RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if writer.calls.Load() != 4 {
		t.Fatalf("saved steps repeated: total model calls=%d, want 4", writer.calls.Load())
	}
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT stage FROM items WHERE id=$1`, item.ID).Scan(&stage); err != nil {
		t.Fatal(err)
	}
	if stage != biz.StageDone {
		t.Fatalf("retry stage=%s", stage)
	}
}

func TestCancelledProcessingSkipsAccountingAndReleasesLock(t *testing.T) {
	store, now := groupStore(t)
	item := groupItem(t, store, now, 1)
	if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET stage='new',prefilter=NULL,attempts=1,last_error='retained' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := &stageWriter{prefilter: func(ctx context.Context) (string, error) { cancel(); return "", ctx.Err() }}
	if err := stageProcessor(store, writer, now, slog.New(slog.NewTextHandler(io.Discard, nil))).RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
	var stage, lastError string
	var attempts int
	var next time.Time
	if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT stage,attempts,last_error,next_attempt_at FROM items WHERE id=$1`, item.ID).Scan(&stage, &attempts, &lastError, &next); err != nil {
		t.Fatal(err)
	}
	if stage != biz.StageNew || attempts != 1 || lastError != "retained" || !next.Equal(now) {
		t.Fatalf("cancellation wrote accounting: stage=%s attempts=%d error=%q next=%s", stage, attempts, lastError, next)
	}
	claimed := false
	processed, err := data.NewWorkRepo(store).ProcessNext(t.Context(), now, func(_ context.Context, got biz.WorkItem, _ func(biz.StepResult) error) (biz.ProcessResult, error) {
		claimed = got.ID == item.ID
		return biz.ProcessResult{PreviousStage: got.Stage, Stage: got.Stage, Attempts: got.Attempts, NextAttemptAt: now}, nil
	})
	if err != nil || !processed || !claimed {
		t.Fatalf("cancelled item could not be reclaimed: processed=%v claimed=%v err=%v", processed, claimed, err)
	}
}

func TestPermanentAndExhaustedProcessingNeverReclaimed(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		name := "exhausted"
		if permanent {
			name = "permanent"
		}
		t.Run(name, func(t *testing.T) {
			store, now := groupStore(t)
			item := groupItem(t, store, now, 1)
			attempts := 2
			if permanent {
				attempts = 0
			}
			if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET stage='new',prefilter=NULL,attempts=$2 WHERE id=$1`, item.ID, attempts); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("model fixture failure")
			if permanent {
				failure = retry.Permanent(failure)
			}
			writer := &stageWriter{prefilter: func(context.Context) (string, error) { return "", failure }}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			if err := stageProcessor(store, writer, now, logger).RunOnce(t.Context()); !errors.Is(err, failure) {
				t.Fatalf("failure=%v", err)
			}
			for round := range 3 {
				if err := stageProcessor(store, writer, now.Add(time.Duration(round+1)*time.Hour), logger).RunOnce(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			var stage, lastError string
			var gotAttempts int
			if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT stage,attempts,last_error FROM items WHERE id=$1`, item.ID).Scan(&stage, &gotAttempts, &lastError); err != nil {
				t.Fatal(err)
			}
			if stage != biz.StageFailed || gotAttempts != attempts+1 || lastError != "model fixture failure" || writer.calls.Load() != 1 {
				t.Fatalf("failed item reclaimed: stage=%s attempts=%d error=%q calls=%d", stage, gotAttempts, lastError, writer.calls.Load())
			}
		})
	}
}

func TestWritingInvalidCategoryRetriesThenPublishes(t *testing.T) {
	for _, categories := range [][]string{{"工程技术", "", ""}, {"工程技术", "教程"}} {
		finalCategory := categories[len(categories)-1]
		t.Run("final-"+finalCategory, func(t *testing.T) {
			store, now := groupStore(t)
			item := groupItem(t, store, now, 1)
			if _, err := testDB(t, store).ExecContext(t.Context(), `UPDATE items SET stage='prefiltered',prefilter='PASS',title='',summary='',score1=NULL,score2=NULL WHERE id=$1`, item.ID); err != nil {
				t.Fatal(err)
			}
			var writes atomic.Int32
			writer := &stageWriter{writing: func() (biz.Writing, error) {
				category := categories[writes.Add(1)-1]
				return biz.NormalizeWriting(biz.Writing{Title: " 中文标题 ", Summary: " 中文摘要 ", Category: category})
			}}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			for attempt := range len(categories) {
				err := stageProcessor(store, writer, now.Add(time.Duration(attempt)*2*time.Minute), logger).RunOnce(t.Context())
				var stage, lastError, title, category string
				var attempts int
				if err := testDB(t, store).QueryRowContext(t.Context(), `SELECT stage,last_error,title,category,attempts FROM items WHERE id=$1`, item.ID).Scan(&stage, &lastError, &title, &category, &attempts); err != nil {
					t.Fatal(err)
				}
				if attempt < len(categories)-1 {
					if err == nil || stage != biz.StagePrefiltered || lastError != "分类不合法" || title != "" || attempts != attempt+1 {
						t.Fatalf("attempt %d: err=%v stage=%s last=%q title=%q attempts=%d", attempt, err, stage, lastError, title, attempts)
					}
				} else if err != nil || stage != biz.StageDone || title != "中文标题" || category != finalCategory {
					t.Fatalf("final err=%v stage=%s title=%q category=%q", err, stage, title, category)
				}
				if writer.calls.Load() != 2 {
					t.Fatalf("saved scores repeated: %d calls", writer.calls.Load())
				}
			}
			if writes.Load() != int32(len(categories)) {
				t.Fatalf("writing calls=%d", writes.Load())
			}
		})
	}
}
