package fetch_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/fetch"
)

func TestClientAllowsLoopbackOnlyWhenConfigured(t *testing.T) {
	const responseBody = "feed response"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("If-None-Match"); got != `"feed-v1"` {
			t.Errorf("If-None-Match = %q, want %q", got, `"feed-v1"`)
		}
		w.Header().Set("ETag", `"feed-v2"`)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, responseBody)
	}))
	defer server.Close()

	ctx := t.Context()
	headers := http.Header{"If-None-Match": []string{`"feed-v1"`}}
	result, err := fetch.New(true, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(ctx, server.URL, headers)
	if err != nil {
		t.Fatalf("test client rejected loopback URL: %v", err)
	}
	if string(result.Body) != responseBody {
		t.Errorf("response body = %q, want %q", result.Body, responseBody)
	}
	if result.StatusCode != http.StatusPartialContent {
		t.Errorf("response status = %d, want %d", result.StatusCode, http.StatusPartialContent)
	}
	if got := result.Header.Get("ETag"); got != `"feed-v2"` {
		t.Errorf("response ETag = %q, want %q", got, `"feed-v2"`)
	}
	if result.URL != server.URL {
		t.Errorf("response URL = %q, want %q", result.URL, server.URL)
	}

	if _, err := fetch.New(false, config.Fetch{RequestTimeout: 20 * time.Second, Retry: config.Retry{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: 16 * time.Millisecond}}).Get(ctx, server.URL, headers); err == nil {
		t.Fatal("production client accepted a loopback URL")
	}
}
