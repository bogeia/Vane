package server

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
)

func TestNewRouterSecurityHeaders(t *testing.T) {
	webDir := t.TempDir()
	for name, content := range map[string]string{"index.html": "<!doctype html><title>Vane</title>", "app.js": "export {};"} {
		if err := os.WriteFile(filepath.Join(webDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), webDir)
	router.GET("/server-error", func(*echo.Context) error { return errors.New("fixture failure") })
	router.GET("/panic-security", func(*echo.Context) error { panic("fixture panic") })
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/", http.StatusOK},
		{"/items/1", http.StatusOK},
		{"/app.js", http.StatusOK},
		{"/api/meta", http.StatusOK},
		{"/api/items?limit=0", http.StatusBadRequest},
		{"/api/missing", http.StatusNotFound},
		{"/users", http.StatusNotFound},
		{"/server-error", http.StatusInternalServerError},
		{"/panic-security", http.StatusInternalServerError},
		{"/feed.xml", http.StatusOK},
		{"/feed/all.xml", http.StatusOK},
		{"/feed/daily.xml", http.StatusOK},
		{"/docs", http.StatusOK},
	} {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://vane.test"+test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status=%d, want %d: %s", response.Code, test.status, response.Body)
			}
			const csp = "default-src 'self'; img-src 'self' data: http: https:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'"
			for header, want := range map[string]string{
				"X-Content-Type-Options":    "nosniff",
				"X-Frame-Options":           "DENY",
				"Referrer-Policy":           "no-referrer",
				"X-XSS-Protection":          "",
				"Strict-Transport-Security": "",
			} {
				if got := response.Header().Get(header); got != want {
					t.Errorf("%s=%q, want %q", header, got, want)
				}
			}
			gotCSP := response.Header().Get("Content-Security-Policy")
			if test.path == "/docs" {
				if gotCSP == csp || !strings.Contains(gotCSP, "https://unpkg.com/@stoplight/elements@") {
					t.Errorf("docs CSP=%q, want Huma's Stoplight CSP", gotCSP)
				}
			} else if gotCSP != csp {
				t.Errorf("CSP=%q, want %q", gotCSP, csp)
			}
		})
	}
}

func TestHeadKeepsSecurityHeadersAndEmptyBody(t *testing.T) {
	webDir := t.TempDir()
	for name, content := range map[string]string{"index.html": "<!doctype html><title>Vane</title>", "app.js": "export {};"} {
		if err := os.WriteFile(filepath.Join(webDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), webDir)
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/", http.StatusOK},
		{"/app.js", http.StatusOK},
		{"/robots.txt", http.StatusOK},
		{"/sitemap.xml", http.StatusOK},
		{"/api/missing", http.StatusNotFound},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodHead, tc.path, nil))
			if response.Code != tc.status || response.Body.Len() != 0 {
				t.Fatalf("status=%d body=%q", response.Code, response.Body)
			}
			for header, want := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer"} {
				if got := response.Header().Get(header); got != want {
					t.Errorf("%s=%q want=%q", header, got, want)
				}
			}
			if response.Header().Get("Content-Security-Policy") == "" {
				t.Error("missing CSP")
			}
		})
	}
}
