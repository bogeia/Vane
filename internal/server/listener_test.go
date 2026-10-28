package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/bogeia/vane/internal/config"
)

func TestRunUsesReservedListenerAndReleasesOnShutdown(t *testing.T) {
	cfg := config.Server{Address: "127.0.0.1:0", ShutdownTimeout: time.Second}
	listener, err := Listen(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := listener.Addr().String()
	// The reservation remains owned until this exact listener is served.
	if duplicate, err := net.Listen("tcp", address); err == nil {
		duplicate.Close()
		t.Fatal("reservation lost")
	}
	router := echo.New()
	router.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	router.GET("/", func(c *echo.Context) error { return c.String(http.StatusOK, "ready") })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, router, cfg, listener) }()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + address + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || string(body) != "ready" {
		t.Fatalf("response: %s %v", body, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown blocked")
	}
	rebound, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("shutdown leaked port: %v", err)
	}
	rebound.Close()
}
