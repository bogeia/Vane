package integration

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/infra/fetch"
)

func TestHFModelsCollectionCreatedAtDiscoveryAndRetry(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	firstCreated, secondCreated := now.Add(-10*24*time.Hour), now.Add(-20*24*time.Hour)
	var phase atomic.Int32
	var listRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models":
			listRequests.Add(1)
			if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
				w.WriteHeader(304)
				return
			}
			if r.URL.Query().Get("author") != "org" {
				t.Error("wrong organization")
			}
			w.Header().Set("ETag", `"same-list"`)
			if phase.Load() == 3 {
				fmt.Fprint(w, `[{"id":"org/missing-date"}]`)
				return
			}
			fmt.Fprintf(w, `[{"id":"org/model-8B","createdAt":%q},{"id":"org/model-FP8","createdAt":%q}`, firstCreated.Format(time.RFC3339), firstCreated.Format(time.RFC3339))
			if phase.Load() > 0 {
				fmt.Fprintf(w, `,{"id":"org/model-32B","createdAt":%q}`, secondCreated.Format(time.RFC3339))
			}
			fmt.Fprint(w, `]`)
		case "/org/model-8B/raw/main/README.md":
			fmt.Fprint(w, "---\nlicense: mit\n---\n# Model 8B\n\nA public model card.")
		case "/org/model-32B/raw/main/README.md":
			if phase.Load() == 1 {
				w.WriteHeader(404)
				return
			}
			fmt.Fprint(w, "# Model 32B\n\nA public model card.")
		default:
			t.Error("unexpected model card request", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{ID: "hf", Kind: biz.SourceKindHFModels, Name: "HF models", Tier: "official", FeedURL: server.URL + "/org", Enabled: true, IntervalMinutes: 120, Block: [][]string{{"/-(FP8|FP4|NVFP4|BF16|Int4|Int8|AWQ|GPTQ|GGUF|MLX|hf)$/"}}}}
	fetcher := data.NewFeedFetcher(fetch.New(true, cfg.Fetch), nil)
	app, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, fetcher)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	db := testDB(t, app.Data)
	require.NoError(t, app.Collector.RunOnce(t.Context()))
	var published time.Time
	var body string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT published_at,body FROM items WHERE original_title='org/model-8B'").Scan(&published, &body))
	assert.Equal(t, firstCreated, published.UTC())
	assert.Equal(t, "# Model 8B\n\nA public model card.", body)
	phase.Store(1)
	now = now.Add(121 * time.Minute)
	require.NoError(t, app.Collector.RunOnce(t.Context()))
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&count))
	assert.Equal(t, 1, count)
	phase.Store(2)
	now = now.Add(121 * time.Minute)
	require.NoError(t, app.Collector.RunOnce(t.Context()))
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT published_at FROM items WHERE original_title='org/model-32B'").Scan(&published))
	assert.Equal(t, now, published.UTC())
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&count))
	assert.Equal(t, 2, count)
	assert.EqualValues(t, 3, listRequests.Load())
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM blocked_contents").Scan(&count))
	assert.Equal(t, 1, count)
	phase.Store(3)
	now = now.Add(121 * time.Minute)
	assert.ErrorIs(t, app.Collector.RunOnce(t.Context()), biz.ErrFeedParse)
	var lastError string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT last_error FROM sources WHERE id='hf'").Scan(&lastError))
	assert.Contains(t, lastError, "missing id or createdAt")
}
