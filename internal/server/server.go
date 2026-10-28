package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/bogeia/vane/internal/config"
)

// Listen reserves the address before application initialization begins.
func Listen(ctx context.Context, config config.Server) (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(ctx, "tcp", config.Address)
}

func Run(ctx context.Context, router *echo.Echo, config config.Server, listener net.Listener) error {
	server := newHTTPServer(router, config)

	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	router.Logger.Info("server started", "address", listener.Addr().String())
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer cancel()
		err := server.Shutdown(shutdown)
		if err != nil {
			err = errors.Join(err, server.Close())
		}
		if serveErr := <-done; !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, serveErr)
		}
		router.Logger.Info("server stopped")
		return err
	}
}

func newHTTPServer(router *echo.Echo, config config.Server) *http.Server {
	return &http.Server{
		Handler:           router,
		ReadTimeout:       config.ReadTimeout,
		ReadHeaderTimeout: config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(router.Logger.Handler(), slog.LevelError),
	}
}
