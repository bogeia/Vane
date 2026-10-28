package data

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/fetch"
)

func TestHFModelsFetchAndModelCard(t *testing.T) {
	models, err := os.ReadFile("testdata/hf-models.json")
	require.NoError(t, err)
	card, err := os.ReadFile("testdata/hf-model-card.md")
	require.NoError(t, err)
	var modelsCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Error("HF must retry listings without validators")
		}
		switch r.URL.Path {
		case "/api/models":
			modelsCalls++
			if r.URL.Query().Get("author") != "Qwen" || r.URL.Query().Get("sort") != "createdAt" || r.URL.Query().Get("direction") != "-1" || r.URL.Query().Get("limit") != "50" {
				t.Error(r.URL)
			}
			w.Header().Set("ETag", `"old"`)
			w.Write(models)
		case "/Qwen/Qwen3-8B/raw/main/README.md":
			w.Write(card)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	source := biz.CollectSource{ID: "hf", Kind: biz.SourceKindHFModels, FeedURL: server.URL + "/Qwen", ETag: "old", LastModified: "old"}
	f := NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}}), nil)
	for range 2 {
		feed, err := f.Fetch(t.Context(), source)
		require.NoError(t, err)
		require.Len(t, feed.Items, 2)
		assert.Empty(t, feed.ETag)
		assert.Empty(t, feed.LastModified)
		assert.Equal(t, "Qwen/Qwen3-8B", feed.Items[0].Title)
		assert.Equal(t, server.URL+"/Qwen/Qwen3-8B", feed.Items[0].URL)
		assert.Empty(t, feed.Items[0].Body)
		require.NotNil(t, feed.Items[0].Published)
		assert.Equal(t, "2026-10-20T08:30:00Z", feed.Items[0].Published.Format(time.RFC3339))
		article, err := f.Article(t.Context(), source, feed.Items[0].URL)
		require.NoError(t, err)
		assert.Equal(t, "# Qwen3-8B\n\nA model card describing capabilities and how to use the model.", article)
	}
	assert.Equal(t, 2, modelsCalls)
}

func TestHFModelsRejectsMalformedResponse(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"models":[]}`, `[{"id":"Qwen/model"}]`, `[{"createdAt":"2026-10-20T00:00:00Z"}]`, `[{"id":"","createdAt":"2026-10-20T00:00:00Z"}]`, `[{"id":"Qwen/model","createdAt":"invalid"}]`, `[{"id":"Qwen/good","createdAt":"2026-10-20T00:00:00Z"},{"id":"Qwen/bad"}]`, `[] trailing`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			f := NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}}), nil)
			feed, err := f.Fetch(t.Context(), biz.CollectSource{Kind: biz.SourceKindHFModels, FeedURL: server.URL + "/Qwen"})
			assert.ErrorIs(t, err, biz.ErrFeedParse)
			assert.Empty(t, feed.Items)
		})
	}
}

func TestHFModelCardEmptyMissingAndMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{name: "empty", body: " \n", status: 200},
		{name: "metadata only", body: "---\nlicense: mit\n---\n", status: 200},
		{name: "metadata ending without newline", body: "---\nlicense: mit\n---", status: 200},
		{name: "no metadata", body: " \n# Model\n", want: "# Model", status: 200},
		{name: "windows newlines", body: "---\r\nlicense: mit\r\n---\r\n\r\n# Model\r\n", want: "# Model", status: 200},
		{name: "not frontmatter", body: "--- not yaml\n# Model", want: "--- not yaml\n# Model", status: 200},
		{name: "missing", status: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/raw/main/README.md") {
					t.Error(r.URL.Path)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			f := NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}}), nil)
			body, err := f.Article(t.Context(), biz.CollectSource{Kind: biz.SourceKindHFModels}, server.URL+"/Qwen/model")
			if tc.status == 404 {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, body)
			}
		})
	}
}

func TestHFModelsPreservesParseErrorCauses(t *testing.T) {
	f := NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}}), nil)
	_, err := f.Fetch(t.Context(), biz.CollectSource{Kind: biz.SourceKindHFModels, FeedURL: "https://huggingface.co/%zz"})
	var urlErr *url.Error
	assert.ErrorAs(t, err, &urlErr)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[{`) }))
	defer server.Close()
	_, err = f.Fetch(t.Context(), biz.CollectSource{Kind: biz.SourceKindHFModels, FeedURL: server.URL + "/Qwen"})
	assert.ErrorIs(t, err, biz.ErrFeedParse)
	var syntaxErr *json.SyntaxError
	assert.ErrorAs(t, err, &syntaxErr)
}
