package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func TestAPICacheUsesCommittedStatus(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		status       int
		panic        bool
		cache        string
	}{
		{"success", "GET", 200, false, "public, max-age=60"},
		{"head", "HEAD", 200, false, "public, max-age=60"},
		{"post", "POST", 200, false, "no-store"},
		{"missing", "GET", 404, false, "no-store"},
		{"backend", "GET", 500, false, "no-store"},
		{"panic", "GET", 500, true, "no-store"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			e.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			e.HTTPErrorHandler = httpErrorHandler
			e.Use(cacheAPIResponses, middleware.Recover())
			e.Any("/api/test", func(c *echo.Context) error {
				if tc.panic {
					panic("private")
				}
				return c.NoContent(tc.status)
			})
			recorder := httptest.NewRecorder()
			e.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), tc.method, "/api/test", nil))
			response := recorder.Result()
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != tc.status || response.Header.Get("Cache-Control") != tc.cache {
				t.Fatalf("status=%d cache=%q", response.StatusCode, response.Header.Get("Cache-Control"))
			}
		})
	}
}

func TestRequestDeadlineCancelsHandlerContext(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = httpErrorHandler
	e.Use(cacheAPIResponses, requestDeadline(time.Millisecond))
	e.GET("/api/test", func(c *echo.Context) error {
		if _, ok := c.Request().Context().Deadline(); !ok {
			t.Error("request context lacks deadline")
		}
		<-c.Request().Context().Done()
		if !errors.Is(c.Request().Context().Err(), context.DeadlineExceeded) {
			t.Error("deadline did not cancel context")
		}
		return c.Request().Context().Err()
	})
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/test", nil))
	if recorder.Code != 500 || recorder.Result().Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("response=%v", recorder.Result())
	}
}

func TestAPICacheHumaFailureAndImplicitHead(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = httpErrorHandler
	e.Use(cacheAPIResponses)
	api := humaecho.New(e, huma.DefaultConfig("test", "1"))
	huma.Get(api, "/api/failure", func(context.Context, *struct{}) (*struct{}, error) {
		return nil, huma.Error500InternalServerError("internal error")
	})
	e.HEAD("/api/empty", func(*echo.Context) error { return nil })
	for _, tc := range []struct {
		method, path, cache string
		status              int
	}{
		{"GET", "/api/failure", "no-store", 500},
		{"HEAD", "/api/empty", "public, max-age=60", 200},
	} {
		recorder := httptest.NewRecorder()
		e.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil))
		response := recorder.Result()
		_ = response.Body.Close()
		if response.StatusCode != tc.status || response.Header.Get("Cache-Control") != tc.cache {
			t.Fatalf("%s status=%d cache=%q", tc.path, response.StatusCode, response.Header.Get("Cache-Control"))
		}
	}
}

func TestRequestDeadlinePreservesParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	e := echo.New()
	e.HTTPErrorHandler = httpErrorHandler
	e.Use(httpMiddleware(slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour)...)
	e.GET("/api/canceled", func(c *echo.Context) error {
		if !errors.Is(c.Request().Context().Err(), context.Canceled) {
			t.Error("parent cancellation did not reach handler")
		}
		return c.Request().Context().Err()
	})
	response := httptest.NewRecorder()
	e.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/canceled", nil))
	if response.Code != http.StatusInternalServerError || response.Header().Get("Cache-Control") != noStoreCacheControl {
		t.Fatalf("status=%d headers=%v", response.Code, response.Header())
	}
}
