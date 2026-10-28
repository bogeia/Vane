package integration

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestWebListCollectionRetriesArticlesAndRecordsSelectorFailure(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	var articleReady, selectorBroken atomic.Bool
	var listRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/article" {
			if !articleReady.Load() {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			fmt.Fprintf(w, "<html><head><title>Official announcement</title></head><body><article><h1>Official announcement</h1><p>%s</p></article></body></html>", strings.Repeat("This official announcement explains a new AI model with practical examples and detailed results. ", 20))
			return
		}
		listRequests.Add(1)
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"same-list"`)
		if selectorBroken.Load() {
			fmt.Fprint(w, "<main>Site changed</main>")
			return
		}
		fmt.Fprint(w, `<article><a href="/article"><h2>Official announcement</h2></a><time>2026-10-27</time></article>`)
	}))
	defer server.Close()
	cfg := loadTestConfig(t, databaseURL)
	cfg.Collect.Enabled = true
	cfg.Sources = []config.Source{{ID: "web", Kind: biz.SourceKindWebList, Name: "Official web", Tier: "official", FeedURL: server.URL + "/news", Enabled: true, IntervalMinutes: 120, WebList: &config.WebList{Item: "article", Link: "a", Title: "h2", Date: "time"}}}
	fetcher := data.NewFeedFetcher(fetch.New(true, cfg.Fetch), map[string]data.WebListSelectors{"web": {Item: "article", Link: "a", Title: "h2", Date: "time"}})
	app, cleanup, err := newTestAppWithFetcher(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return now }, fetcher)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	db := testDB(t, app.Data)
	require.NoError(t, app.Collector.RunOnce(t.Context()))
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&count))
	assert.Zero(t, count)
	articleReady.Store(true)
	now = now.Add(121 * time.Minute)
	require.NoError(t, app.Collector.RunOnce(t.Context()))
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM items").Scan(&count))
	assert.Equal(t, 1, count)
	assert.EqualValues(t, 2, listRequests.Load())
	var stage, body string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT stage, body FROM items").Scan(&stage, &body))
	assert.Equal(t, "new", stage)
	assert.Contains(t, body, "practical examples")
	selectorBroken.Store(true)
	now = now.Add(121 * time.Minute)
	assert.ErrorContains(t, app.Collector.RunOnce(t.Context()), "web list: no items matched")
	var lastError string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT last_error FROM sources WHERE id='web'").Scan(&lastError))
	assert.Equal(t, "web list: no items matched", lastError)
}
