package fetch_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/fetch"
	"github.com/bogeia/vane/internal/infra/retry"
)

func TestFetchRetriesTransientStatusesAndStopsPermanent(t *testing.T) {
	for _, status := range []int{429, 503, 401, 403, 404} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("If-None-Match") != "tag" {
					t.Error("headers lost")
				}
				if calls.Add(1) == 1 {
					w.WriteHeader(status)
					return
				}
				_, _ = io.WriteString(w, "ok")
			}))
			defer server.Close()
			result, err := fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}}).Get(t.Context(), server.URL, http.Header{"If-None-Match": []string{"tag"}})
			if retry.Status(status) {
				if err != nil || calls.Load() != 2 || string(result.Body) != "ok" {
					t.Fatalf("calls=%d err=%v", calls.Load(), err)
				}
			} else if !retry.IsPermanent(err) || calls.Load() != 1 {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestFetchRetryAfterExceedsBoundedBudget(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(503)
	}))
	defer server.Close()
	started := time.Now()
	_, err := fetch.New(true, config.Fetch{RequestTimeout: 10 * time.Millisecond, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}}).Get(t.Context(), server.URL, nil)
	if err == nil || calls.Load() != 1 || retry.NotBefore(err).Before(started.Add(59*time.Second)) || time.Since(started) > time.Second {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}}).Get(ctx, server.URL, nil)
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 || retry.IsPermanent(err) {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}
