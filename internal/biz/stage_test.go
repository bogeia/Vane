package biz

import (
	"errors"
	"testing"
)

func TestProcessingStageTransitions(t *testing.T) {
	for _, test := range []struct{ from, event, want string }{
		{"new", "pass", "prefiltered"},
		{"new", "block", "blocked"},
		{"prefiltered", "analyze", "analyzed"},
		{"analyzed", "publish", "done"},
		{"new", "fail", "failed"},
		{"prefiltered", "fail", "failed"},
		{"analyzed", "fail", "failed"},
	} {
		t.Run(test.from+"/"+test.event, func(t *testing.T) {
			got, err := TransitionStage(test.from, test.event)
			if err != nil || got != test.want {
				t.Fatalf("stage=%q error=%v, want %q", got, err, test.want)
			}
		})
	}
	for _, test := range []struct{ from, event string }{
		{"done", "fail"}, {"new", "publish"}, {"blocked", "analyze"}, {"failed", "pass"}, {"unknown", "pass"}, {"new", "unknown"},
	} {
		t.Run(test.from+"/"+test.event, func(t *testing.T) {
			_, err := TransitionStage(test.from, test.event)
			if !errors.Is(err, ErrInvalidStageTransition) {
				t.Fatalf("error=%v, want illegal transition", err)
			}
		})
	}
}
