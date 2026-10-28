package biz

import (
	"time"

	"github.com/bogeia/vane/internal/infra/retry"
)

type WorkerOptions struct {
	AttemptTimeout time.Duration
	Retry          retry.Policy
	Count          int
}
type (
	Thresholds       struct{ Official, Media int }
	CollectorOptions struct {
		Enabled bool
		Block   map[string][][]string
		Kinds   map[string]string
	}
	ProcessorOptions struct {
		Enabled    bool
		Worker     WorkerOptions
		Thresholds Thresholds
	}
)
