package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/bogeia/vane/internal/biz"
)

type workRepo struct{ data *Data }

func NewWorkRepo(data *Data) biz.WorkRepository {
	return &workRepo{data: data}
}

func (r *workRepo) ProcessNext(ctx context.Context, now time.Time, process func(context.Context, biz.WorkItem, func(biz.StepResult) error) (biz.ProcessResult, error)) (bool, error) {
	db, err := r.data.ORM.DB()
	if err != nil {
		return false, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	orm, err := openORM(conn)
	if err != nil {
		return false, err
	}
	tx := orm.WithContext(ctx).Begin()
	if err := tx.Error; err != nil {
		return false, err
	}
	defer tx.Rollback()
	skipped := make([]int64, 0)
	var item biz.WorkItem
	for {
		var record struct {
			Item itemModel `gorm:"embedded"`
			Tier string
		}
		query := tx.Model(&itemModel{}).Table("items i").Select("i.*,s.tier").Joins("JOIN sources s ON s.id=i.source_id").Where("i.stage IN ? AND i.next_attempt_at <= ?", []string{biz.StageNew, biz.StagePrefiltered, biz.StageAnalyzed}, now).Order("i.next_attempt_at,i.id").Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "i"}, Options: "SKIP LOCKED"})
		if len(skipped) > 0 {
			query = query.Where("i.id NOT IN ?", skipped)
		}
		if err := query.Take(&record).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		stored := record.Item
		var locked bool
		// Negative keys belong to processing; the lock remains on this dedicated connection after commit.
		if err := tx.Raw("SELECT pg_try_advisory_lock(-?::bigint)", stored.ID).Scan(&locked).Error; err != nil {
			return false, err
		}
		if !locked {
			skipped = append(skipped, stored.ID)
			continue
		}
		defer releaseItemLock(conn, stored.ID)
		item = biz.WorkItem{Stage: stored.Stage, ID: stored.ID, OriginalTitle: stored.OriginalTitle, Body: stored.Body, PublishedAt: stored.PublishedAt, Attempts: int(stored.Attempts), Tier: record.Tier, Score1: stored.Score1, Score2: stored.Score2}
		if stored.Prefilter != nil {
			item.Prefilter = *stored.Prefilter
		}
		if stored.StoryID != nil {
			item.StoryID = *stored.StoryID
		}
		if stored.Title != "" && stored.Summary != "" {
			item.Writing = &biz.Writing{Roundup: stored.Roundup, Title: stored.Title, Summary: stored.Summary, Reason: stored.Reason, Category: stored.Category, Tags: append([]string{}, stored.Tags...), Subjects: slices.Clone(stored.Subjects)}
			if len(stored.Fact) > 0 {
				if fact, err := decodeFact(stored.Fact); err != nil {
					item.LoadError = fmt.Errorf("stored fact is invalid")
				} else {
					item.Writing.Fact = fact
				}
			}
		}
		break
	}
	if err := tx.Commit().Error; err != nil {
		return false, err
	}
	var saveMu sync.Mutex
	save := func(step biz.StepResult) error {
		saveMu.Lock()
		defer saveMu.Unlock()
		updates := map[string]any{"stage": step.Stage}
		switch {
		case step.Prefilter != "":
			updates["prefilter"] = step.Prefilter
		case step.Score != nil:
			if step.ScoreSlot == 1 {
				updates["score1"] = *step.Score
			} else {
				updates["score2"] = *step.Score
			}
		case step.Writing != nil:
			w := step.Writing
			var fact any
			if w.Fact != nil {
				content, err := encodeFact(w.Fact)
				if err != nil {
					return err
				}
				fact = string(content)
			}
			updates["roundup"], updates["title"], updates["summary"], updates["reason"] = w.Roundup, w.Title, w.Summary, w.Reason
			updates["category"], updates["tags"], updates["subjects"], updates["fact"] = w.Category, gorm.Expr("?::text[]", textArray(w.Tags)), gorm.Expr("?::text[]", textArray(w.Subjects)), fact
		}
		return checkStageUpdate(ctx, orm.WithContext(ctx).Model(&itemModel{}).Where("id = ? AND stage = ?", item.ID, step.PreviousStage).Updates(updates), item.ID, step.PreviousStage)
	}
	result, processErr := process(ctx, item, save)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	updates := map[string]any{"attempts": result.Attempts, "next_attempt_at": result.NextAttemptAt, "last_error": result.LastError}
	if result.Stage != result.PreviousStage {
		updates["stage"] = result.Stage
	}
	err = checkStageUpdate(ctx, orm.WithContext(ctx).Model(&itemModel{}).Where("id = ? AND stage = ?", item.ID, result.PreviousStage).Updates(updates), item.ID, result.PreviousStage)
	if err != nil {
		return false, errors.Join(processErr, err)
	}
	return true, processErr
}

func checkStageUpdate(ctx context.Context, result *gorm.DB, id int64, stage string) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		slog.ErrorContext(ctx, "processing stage compare-and-swap failed", "item_id", id, "expected_stage", stage)
		return fmt.Errorf("%w: item %d expected %s", biz.ErrStageConflict, id, stage)
	}
	return nil
}

func releaseItemLock(conn *sql.Conn, id int64) {
	// Cleanup must release the session lock even when the model context was cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var unlocked bool
	err := conn.QueryRowContext(ctx, `SELECT pg_advisory_unlock(-$1::bigint)`, id).Scan(&unlocked)
	if err != nil || !unlocked {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
}

// A Valuer keeps GORM from expanding a PostgreSQL array into separate SQL arguments.
type textArray []string

func (a textArray) Value() (driver.Value, error) {
	encoded, err := pgtype.NewMap().Encode(pgtype.TextArrayOID, pgtype.TextFormatCode, []string(a), nil)
	if err != nil {
		return nil, err
	}
	if encoded == nil {
		return nil, nil //nolint:nilnil // A nil driver.Value intentionally represents SQL NULL.
	}
	return string(encoded), nil
}

func (a *textArray) Scan(value any) error {
	switch value := value.(type) {
	case nil:
		*a = nil
		return nil
	case string:
		return pgtype.NewMap().Scan(pgtype.TextArrayOID, pgtype.TextFormatCode, []byte(value), (*[]string)(a))
	case []byte:
		return pgtype.NewMap().Scan(pgtype.TextArrayOID, pgtype.TextFormatCode, value, (*[]string)(a))
	default:
		return fmt.Errorf("unsupported PostgreSQL text array type %T", value)
	}
}
