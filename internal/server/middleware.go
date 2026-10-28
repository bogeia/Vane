package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func securityHeaders() echo.MiddlewareFunc {
	return middleware.SecureWithConfig(middleware.SecureConfig{
		ContentTypeNosniff:    "nosniff",
		XFrameOptions:         "DENY",
		ReferrerPolicy:        "no-referrer",
		ContentSecurityPolicy: "default-src 'self'; img-src 'self' data: http: https:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'",
	})
}

func requestLogger(log *slog.Logger) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		HandleError: true,
		LogMethod:   true, LogURIPath: true, LogStatus: true, LogLatency: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			level := slog.LevelInfo
			attrs := []slog.Attr{
				slog.String("method", v.Method), slog.String("path", v.URIPath),
				slog.Int("status", v.Status), slog.Duration("latency", v.Latency),
			}
			if v.Status >= http.StatusInternalServerError || v.Error != nil {
				level = slog.LevelError
			}
			if v.Error != nil {
				attrs = append(attrs, slog.String("error", v.Error.Error()))
			}
			log.LogAttrs(c.Request().Context(), level, "http request", attrs...)
			return nil
		},
	})
}

func cacheAPIResponses(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if strings.HasPrefix(c.Request().URL.Path, "/api/") {
			writer := &apiCacheWriter{ResponseWriter: c.Response(), method: c.Request().Method, icon: c.Path() == "/api/source-icons/:id"}
			c.SetResponse(writer)
			if err := next(c); err != nil {
				return err
			}
			if !writer.wroteHeader {
				writer.WriteHeader(http.StatusOK)
			}
			return nil
		}
		return next(c)
	}
}

type apiCacheWriter struct {
	http.ResponseWriter
	method      string
	icon        bool
	wroteHeader bool
}

func (w *apiCacheWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *apiCacheWriter) WriteHeader(status int) {
	if !w.wroteHeader && status >= 200 {
		w.wroteHeader = true
		cache := noStoreCacheControl
		if status < 300 && (w.method == http.MethodGet || w.method == http.MethodHead) {
			cache = publicCacheControl
		}
		if !w.icon || (status >= 400) || w.Header().Get("Cache-Control") == "" {
			w.Header().Set("Cache-Control", cache)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *apiCacheWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *apiCacheWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func requestDeadline(timeout time.Duration) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if timeout <= 0 {
				return next(c)
			}
			ctx, cancel := context.WithTimeout(c.Request().Context(), timeout)
			defer cancel()
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}
