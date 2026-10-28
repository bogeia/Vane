package job

import (
	"context"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
)

func TestSourceIconRetryStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	runner := &Runner{icons: biz.NewSourceIconsUseCase(nil, nil, nil)}
	done := make(chan struct{})
	go func() { runner.completeIcons(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("icon retry did not stop on cancellation")
	}
}
