package biz

import (
	"context"
	"errors"
	"fmt"

	"github.com/looplab/fsm"
)

const (
	StageEventPass    = "pass"
	StageEventBlock   = "block"
	StageEventAnalyze = "analyze"
	StageEventPublish = "publish"
	StageEventFail    = "fail"
)

var (
	ErrInvalidStageTransition = errors.New("invalid processing stage transition")
	ErrStageConflict          = errors.New("processing stage changed")
)

// TransitionStage validates one processing stage transition without side effects.
func TransitionStage(stage, event string) (string, error) {
	machine := fsm.NewFSM(stage, fsm.Events{
		{Name: StageEventPass, Src: []string{StageNew}, Dst: StagePrefiltered},
		{Name: StageEventBlock, Src: []string{StageNew}, Dst: StageBlocked},
		{Name: StageEventAnalyze, Src: []string{StagePrefiltered}, Dst: StageAnalyzed},
		{Name: StageEventPublish, Src: []string{StageAnalyzed}, Dst: StageDone},
		{Name: StageEventFail, Src: []string{StageNew, StagePrefiltered, StageAnalyzed}, Dst: StageFailed},
	}, nil)
	if err := machine.Event(context.Background(), event); err != nil {
		return "", fmt.Errorf("%w: %s / %s", ErrInvalidStageTransition, stage, event)
	}
	return machine.Current(), nil
}
