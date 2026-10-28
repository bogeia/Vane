package biz

import (
	"context"
	"time"
)

type GoldItem struct {
	Selected      bool
	ID            int64
	URL           string
	Tier          string
	Source        string
	Title         string
	OriginalTitle string
	PublishedAt   time.Time
	Score1        int
	Score2        int
	Label         string
	Body          string
}

type GoldSet struct {
	Items []GoldItem
}

// EvalRepository reads completed items for an editable evaluation set.
type EvalRepository interface {
	ExportGold(context.Context, time.Time, time.Time, int) (GoldSet, error)
}

type EvalExportUseCase struct{ repo EvalRepository }

func NewEvalExportUseCase(repo EvalRepository) *EvalExportUseCase {
	return &EvalExportUseCase{repo: repo}
}

func (u *EvalExportUseCase) Export(ctx context.Context, since, until time.Time, n int) (GoldSet, error) {
	set, err := u.repo.ExportGold(ctx, since, until, n)
	for index := range set.Items {
		set.Items[index].Label = LabelReject
		if set.Items[index].Selected {
			set.Items[index].Label = LabelSelect
		}
	}
	return set, err
}

type EvaluationUseCase struct {
	model   EvaluationModel
	options EvaluationOptions
}

func NewEvaluationUseCase(model EvaluationModel, options EvaluationOptions) *EvaluationUseCase {
	return &EvaluationUseCase{model: model, options: options}
}

func (u *EvaluationUseCase) Run(ctx context.Context, gold GoldSet) (Evaluation, error) {
	return Evaluate(ctx, gold, u.model, u.options)
}
