package llm

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/retry"
)

type timeoutTransport struct{ calls int }

func (transport *timeoutTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	<-request.Context().Done()
	return nil, request.Context().Err()
}

func TestModelRequestTimeoutAndOuterBudget(t *testing.T) {
	// Virtual time excludes network and scheduler overhead from the tight retry budget.
	synctest.Test(t, func(t *testing.T) {
		transport := &timeoutTransport{}
		cfg := config.LLM{Enabled: true, BaseURL: "https://model.test", APIKey: "secret", Model: "fake", RequestTimeout: 10 * time.Millisecond, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}}
		client := New(cfg)
		client.http.Transport = transport
		started := time.Now()
		_, err := client.Prefilter(t.Context(), biz.WorkItem{})
		elapsed := time.Since(started)
		if !errors.Is(err, context.DeadlineExceeded) || transport.calls != 3 || retry.IsPermanent(err) {
			t.Fatalf("calls=%d err=%v", transport.calls, err)
		}
		if elapsed < 30*time.Millisecond || elapsed > 32*time.Millisecond {
			t.Fatalf("request attempts took %s, want 30–32ms", elapsed)
		}
		before := transport.calls
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Millisecond)
		defer cancel()
		cfg.RequestTimeout = time.Second
		client = New(cfg)
		client.http.Transport = transport
		started = time.Now()
		_, err = client.Prefilter(ctx, biz.WorkItem{})
		if !errors.Is(err, context.DeadlineExceeded) || transport.calls != before+1 {
			t.Fatalf("calls=%d err=%v", transport.calls, err)
		}
		if elapsed := time.Since(started); elapsed != 15*time.Millisecond {
			t.Fatalf("outer cancellation took %s, want 15ms", elapsed)
		}
	})
}
