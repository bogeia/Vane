package biz

import (
	"context"
	"time"
)

type Window time.Duration

const (
	Window24h Window = Window(24 * time.Hour)
	Window7d  Window = Window(7 * 24 * time.Hour)
)

func (w Window) Since(now time.Time) time.Time { return now.Add(-time.Duration(w)) }

type LatestQuery struct {
	Window Window
	View   string
	Topic  string
	Limit  int
}

func (i *ItemsUseCase) Latest(ctx context.Context, query LatestQuery) (ItemPage, error) {
	return i.List(ctx, nil, query.Limit, ItemFilter{View: query.View, Topic: query.Topic, Since: query.Window.Since(i.now())})
}
