package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/service"
)

type iconHTTPRepo struct {
	icon biz.SourceIcon
	err  error
}

func (r *iconHTTPRepo) IconState(context.Context, string) (biz.SourceIconState, error) {
	return biz.SourceIconState{}, nil
}

func (r *iconHTTPRepo) SaveIcon(context.Context, biz.Source, string, biz.SourceIcon) error {
	return nil
}
func (r *iconHTTPRepo) Icon(context.Context, string) (biz.SourceIcon, error) { return r.icon, r.err }
func TestSourceIconGETHEADAndErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{Server: config.Server{RequestTimeout: time.Second}, Site: config.Site{Name: "Vane", PublicURL: "https://vane.test"}}
	repo := &iconHTTPRepo{icon: biz.SourceIcon{Content: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), ContentType: "image/svg+xml"}}
	icons := biz.NewSourceIconsUseCase(nil, repo, nil)
	sources := service.NewSourcesService(biz.NewSourcesUseCase(nil, nil, time.Now), icons)
	agent, mcpServer := testAgentServices(logger, cfg.Site, fakeItems{})
	router := NewRouter(logger, nil, cfg, nil, nil, nil, nil, sources, agent, mcpServer)
	version := fmt.Sprintf("%x", sha256.Sum256(repo.icon.Content))[:24]
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), method, "/api/source-icons/publisher?v="+version, nil))
		if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "image/svg+xml" || recorder.Header().Get("Content-Length") != strconv.Itoa(len(repo.icon.Content)) || recorder.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "sandbox") {
			t.Fatalf("%s: status=%d headers=%v", method, recorder.Code, recorder.Header())
		}
		if method == http.MethodHead && recorder.Body.Len() != 0 {
			t.Fatal("HEAD contains body")
		}
		if method == http.MethodGet && recorder.Body.String() != string(repo.icon.Content) {
			t.Fatal("original icon was changed")
		}
	}

	for _, tc := range []struct {
		query, etag string
		status      int
		cache       string
	}{
		{"?v=" + version, "", 200, "immutable"},
		{"?v=old", "", 404, "no-store"},
		{"", "", 200, "max-age=60"},
		{"?v=" + version, `"` + version + `"`, 304, "immutable"},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/source-icons/publisher"+tc.query, nil)
		req.Header.Set("If-None-Match", tc.etag)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.status || !strings.Contains(rec.Header().Get("Cache-Control"), tc.cache) {
			t.Fatalf("%+v: %d %v", tc, rec.Code, rec.Header())
		}
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{biz.ErrSourceIconNotFound, 404}, {errors.New("private database details"), 500}} {
		repo.err = tc.err
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), method, "/api/source-icons/missing", nil))
			if recorder.Code != tc.status || strings.Contains(recorder.Body.String(), "private") || (method == http.MethodHead && recorder.Body.Len() != 0) {
				t.Fatalf("%s error=%d %s", method, recorder.Code, recorder.Body)
			}
		}
	}
}
