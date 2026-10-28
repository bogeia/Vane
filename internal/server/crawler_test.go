package server

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRobotsGetAndHeadDescribePrivateRoutesAndPublicSitemap(t *testing.T) {
	router := feedTestRouter(feedItemsRepository{}, feedReportsRepository{})
	const want = "User-agent: *\nAllow: /api/agent\nDisallow: /api/\nDisallow: /favorites\nSitemap: https://vane.test/sitemap.xml\n"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), method, "/robots.txt", nil))
		if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain") {
			t.Fatalf("%s robots returned %d %s", method, response.Code, response.Header())
		}
		body := want
		if method == http.MethodHead {
			body = ""
		}
		if response.Body.String() != body {
			t.Fatalf("%s robots body=%q", method, response.Body.String())
		}
		if response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatal("security headers missing")
		}
	}
}

func TestSitemapGetAndHeadUseXMLCachingAndKeepErrorsUncached(t *testing.T) {
	for _, failure := range []error{nil, fmt.Errorf("private database detail")} {
		router := feedTestRouter(feedItemsRepository{err: failure}, feedReportsRepository{})
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), method, "/sitemap.xml", nil))
			status, cache, contentType := http.StatusOK, "public, max-age=60", "application/xml"
			if failure != nil {
				status, cache, contentType = http.StatusInternalServerError, "no-store", "application/problem+json"
			}
			if response.Code != status || response.Header().Get("Cache-Control") != cache || !strings.HasPrefix(response.Header().Get("Content-Type"), contentType) {
				t.Fatalf("%s sitemap response=%d %v", method, response.Code, response.Header())
			}
			if method == http.MethodHead && response.Body.Len() != 0 {
				t.Fatal("HEAD has a body")
			}
			if strings.Contains(response.Body.String(), "private database detail") {
				t.Fatal("error leaks private details")
			}
			for header, want := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer"} {
				if response.Header().Get(header) != want {
					t.Fatalf("missing %s", header)
				}
			}
			if response.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("CSP missing")
			}
		}
	}
}

func TestDocumentErrorsLogUnderlyingCauseOnceAndStayGeneric(t *testing.T) {
	const privateError = "private document repository failure"
	for _, test := range []struct {
		path       string
		itemsError error
		reportErr  error
	}{
		{path: "/feed.xml", itemsError: errors.New(privateError)},
		{path: "/feed/daily.xml", reportErr: errors.New(privateError)},
		{path: "/sitemap.xml", itemsError: errors.New(privateError)},
	} {
		t.Run(test.path, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			router := feedTestRouterWithLogger(logger, feedItemsRepository{err: test.itemsError}, feedReportsRepository{listErr: test.reportErr})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, nil))

			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", response.Code)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/problem+json") {
				t.Fatalf("Content-Type = %q", response.Header().Get("Content-Type"))
			}
			if strings.Contains(response.Body.String(), privateError) {
				t.Fatal("error response exposed the private repository failure")
			}
			if count := strings.Count(logs.String(), privateError); count != 1 {
				t.Fatalf("underlying error was logged %d times, want once; logs=%s", count, logs.String())
			}
		})
	}
}
