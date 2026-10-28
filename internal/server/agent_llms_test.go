package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMSRouteUsesPlainTextCacheAndHeadSemantics(t *testing.T) {
	router := testRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), method, "/llms.txt", nil))
			require.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "text/plain; charset=utf-8", response.Header().Get("Content-Type"))
			assert.Equal(t, "public, max-age=60", response.Header().Get("Cache-Control"))
			if method == http.MethodHead {
				assert.Empty(t, response.Body.String())
			} else {
				assert.Contains(t, response.Body.String(), "# Vane\n")
				assert.Contains(t, response.Body.String(), "https://vane.test/api/agent")
				assert.Contains(t, response.Body.String(), "https://vane.test/agent")
			}
		})
	}
}
