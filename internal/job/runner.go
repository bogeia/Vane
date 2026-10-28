package job

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/wire"

	"github.com/bogeia/vane/internal/biz"
)

type Runner struct {
	icons     *biz.SourceIconsUseCase
	alerts    *biz.SourceAlertsUseCase
	collector *biz.Collector
	processor *biz.Processor
	log       *slog.Logger
	reports   *biz.ReportsUseCase
	realtime  *biz.RealtimeUseCase
}

func NewRunner(collector *biz.Collector, processor *biz.Processor, log *slog.Logger, reports *biz.ReportsUseCase, realtime *biz.RealtimeUseCase, alerts *biz.SourceAlertsUseCase, icons *biz.SourceIconsUseCase) *Runner {
	return &Runner{icons: icons, alerts: alerts, collector: collector, processor: processor, log: log, reports: reports, realtime: realtime}
}

func (r *Runner) Run(ctx context.Context) {
	var tasks sync.WaitGroup
	tasks.Go(func() { r.completeIcons(ctx) })

	tasks.Go(func() { r.collect(ctx) })
	tasks.Go(func() { r.process(ctx) })
	tasks.Go(func() { r.publish(ctx) })
	tasks.Go(func() { r.pushRealtime(ctx) })
	tasks.Go(func() { r.alert(ctx) })
	tasks.Wait()
}

func (r *Runner) collect(ctx context.Context) {
	ticks := time.Tick(time.Minute)
	for {
		if err := r.collector.RunOnce(ctx); err != nil && ctx.Err() == nil {
			r.log.ErrorContext(ctx, "collect sources", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

func (r *Runner) process(ctx context.Context) {
	ticks := time.Tick(time.Second)
	for {
		if err := r.processor.RunOnce(ctx); err != nil && ctx.Err() == nil {
			r.log.ErrorContext(ctx, "process items", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

func (r *Runner) publish(ctx context.Context) {
	ticks := time.Tick(10 * time.Minute)
	for {
		if err := r.reports.RunOnce(ctx); err != nil && ctx.Err() == nil {
			r.log.ErrorContext(ctx, "publish report", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

var ProviderSet = wire.NewSet(NewRunner)

func (r *Runner) pushRealtime(ctx context.Context) {
	ticks := time.Tick(time.Minute)
	for {
		if err := r.realtime.RunOnce(ctx); err != nil && ctx.Err() == nil {
			r.log.ErrorContext(ctx, "realtime push", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

func (r *Runner) alert(ctx context.Context) {
	ticks := time.Tick(time.Minute)
	for {
		if err := r.alerts.RunOnce(ctx); err != nil && ctx.Err() == nil {
			r.log.ErrorContext(ctx, "check source alerts", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

// Failed discovery is retried six-hourly without coupling it to collection or request traffic.
func (r *Runner) completeIcons(ctx context.Context) {
	ticks := time.Tick(6 * time.Hour)
	for {
		if err := r.icons.Complete(ctx); err != nil && ctx.Err() == nil {
			r.log.ErrorContext(ctx, "complete source icons", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}
