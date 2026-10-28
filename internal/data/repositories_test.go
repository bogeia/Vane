package data_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/data"
)

func TestRepositoriesReturnUnavailablePoolError(t *testing.T) {
	store := &data.Data{ORM: &gorm.DB{Config: &gorm.Config{}}}
	if err := data.NewOperationsRepo(store).Ping(t.Context()); !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("Ping error=%v, want unavailable pool", err)
	}
	processed, err := data.NewWorkRepo(store).ProcessNext(t.Context(), time.Now(), func(context.Context, biz.WorkItem, func(biz.StepResult) error) (biz.ProcessResult, error) {
		t.Error("processing started without a database pool")
		return biz.ProcessResult{}, nil
	})
	if processed || !errors.Is(err, gorm.ErrInvalidDB) {
		t.Fatalf("ProcessNext processed=%v error=%v, want false/unavailable pool", processed, err)
	}
}
