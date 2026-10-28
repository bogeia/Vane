package llm_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/llm"
	"github.com/bogeia/vane/internal/infra/retry"
)

func TestModelRejectsOversizedResponseWithoutRetry(t *testing.T) {
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprint(extra), func(t *testing.T) {
			const envelope = `{"choices":[{"message":{"content":"{\"label\":\"PASS\"}"}}]}`
			body := envelope + strings.Repeat(" ", (1<<20)-len(envelope)+extra)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			cfg := config.LLM{Enabled: true, BaseURL: server.URL, APIKey: "secret", Model: "fake", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}}
			_, err := llm.New(cfg).Prefilter(t.Context(), biz.WorkItem{})
			if extra == 0 && err != nil {
				t.Fatalf("exactly 1 MiB rejected: %v", err)
			}
			if extra == 1 && (err == nil || !retry.IsPermanent(err)) {
				t.Fatalf("1 MiB + 1: want permanent error, got %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("requests=%d, want 1", calls.Load())
			}
		})
	}
}

func TestModelFailuresDoNotExposeSecrets(t *testing.T) {
	const secret = "private-api-key-marker"
	const responseSecret = "private-response-marker"
	for _, test := range []struct {
		name        string
		status      int
		body        string
		closeServer bool
	}{
		{name: "network failure", closeServer: true},
		{name: "non 200", status: http.StatusBadRequest, body: responseSecret},
		{name: "oversized response", status: http.StatusOK, body: responseSecret + strings.Repeat("x", 1<<20)},
		{name: "invalid envelope JSON", status: http.StatusOK, body: responseSecret},
		{name: "invalid model JSON", status: http.StatusOK, body: `{"choices":[{"message":{"content":"` + responseSecret + `"}}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			if test.closeServer {
				server.Close()
			}
			cfg := config.LLM{Enabled: true, BaseURL: server.URL + "/private-model-endpoint", APIKey: secret, Model: "fake", RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}}
			_, err := llm.New(cfg).Prefilter(t.Context(), biz.WorkItem{})
			if err == nil {
				t.Fatal("request unexpectedly succeeded")
			}
			for _, sensitive := range []string{cfg.BaseURL, server.URL, secret, responseSecret} {
				if strings.Contains(err.Error(), sensitive) {
					t.Fatalf("error exposes sensitive data: %v", err)
				}
			}
		})
	}
}
