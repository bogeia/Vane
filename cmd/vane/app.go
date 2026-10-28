package main

import (
	"context"
	"net"

	"github.com/labstack/echo/v5"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/job"
	"github.com/bogeia/vane/internal/server"
)

type App struct {
	Router *echo.Echo
	Jobs   *job.Runner
}

func New(router *echo.Echo, jobs *job.Runner) *App {
	return &App{Router: router, Jobs: jobs}
}

func (a *App) Run(ctx context.Context, cfg config.Server, listener net.Listener) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); a.Jobs.Run(ctx) }()
	err := server.Run(ctx, a.Router, cfg, listener)
	cancel()
	<-done
	return err
}
