package data

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/fetch"
)

func webListFetcher(t *testing.T, body string, selectors WebListSelectors) (*FeedFetcher, biz.CollectSource) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Error("web list must be fetched again so missing articles can retry")
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}}), map[string]WebListSelectors{"web": selectors}), biz.CollectSource{ID: "web", Kind: biz.SourceKindWebList, FeedURL: server.URL + "/list", ETag: "old", LastModified: "old"}
}

func TestWebListDatesAndMissingFields(t *testing.T) {
	dates := []string{"2026-10-27T13:14:15+08:00", "2026-10-27", "2026/10/27", "2026.10.27", "Oct 27, 2026", "October 27, 2026", "2026年10月27日"}
	var body strings.Builder
	for i, date := range dates {
		fmt.Fprintf(&body, `<article><a href="/item/%d#part"><h2> Test   title %d </h2></a><time datetime="%s">ignored</time></article>`, i, i, date)
	}
	body.WriteString(`<article><a href="/item/0"><h2>duplicate</h2></a><time>2026-10-27</time></article><article><a href="/relative"><h2>relative</h2></a><time>2天前</time></article><article><a href="/missing"><h2>missing date</h2></a></article><article><h2>missing link</h2><time>2026-10-27</time></article><article><a href="/no-title"></a><time>2026-10-27</time></article>`)
	f, source := webListFetcher(t, body.String(), WebListSelectors{Item: "article", Link: "a", Title: "h2", Date: "time"})
	feed, err := f.Fetch(t.Context(), source)
	require.NoError(t, err)
	require.Len(t, feed.Items, 7)
	for i, item := range feed.Items {
		assert.Equal(t, strings.TrimSuffix(source.FeedURL, "/list")+fmt.Sprintf("/item/%d", i), item.URL)
		assert.Equal(t, fmt.Sprintf("Test title %d", i), item.Title)
		assert.Empty(t, item.Body)
		require.NotNil(t, item.Published)
	}
	for _, item := range feed.Items[1:] {
		assert.Equal(t, "2026-10-27T00:00:00+08:00", item.Published.Format(time.RFC3339))
	}
	assert.Equal(t, "2026-10-27T13:14:15+08:00", feed.Items[0].Published.Format(time.RFC3339))
}

func TestWebListEmptyErrorsAndSelfMatch(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`<main/>`, "web list: no items matched"},
		{`<a href="/x"><time>2天前</time></a>`, "web list: no item had link, title and date"},
		{`<a href="/x"><span>Title</span><time>2026年1月2日</time></a>`, ""},
	} {
		f, s := webListFetcher(t, tc.body, WebListSelectors{Item: "a", Link: "a", Title: "span", Date: "time"})
		feed, err := f.Fetch(t.Context(), s)
		if tc.want != "" {
			assert.EqualError(t, err, tc.want)
		} else {
			require.NoError(t, err)
			require.Len(t, feed.Items, 1)
			assert.Equal(t, "2026-01-02T00:00:00+08:00", feed.Items[0].Published.Format(time.RFC3339))
		}
	}
}

func TestWebListOfficialSamplesUseExampleSelectors(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"config", "sources"} {
		b, err := os.ReadFile("../../configs/" + name + ".example.yaml")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yaml"), b, 0o600))
	}
	cfg, err := config.Load(filepath.Join(dir, "config.yaml"))
	require.NoError(t, err)
	for _, tc := range []struct {
		id, file, title, path, date string
		count                       int
	}{
		{"web-anthropic-news", "anthropic-news.html", "Anthropic files S-1 with the SEC for proposed initial public offering", "/news/s1-sec", "2026-10-26T00:00:00+08:00", 3},
		{"web-kimi-blog", "kimi-blog.html", "Kimi K3.1 Pro", "/en/blog/kimi-k3-1-pro", "2026-10-24T00:00:00+08:00", 2},
	} {
		t.Run(tc.id, func(t *testing.T) {
			body, err := os.ReadFile("testdata/" + tc.file)
			require.NoError(t, err)
			for _, source := range cfg.Sources {
				if source.ID != tc.id {
					continue
				}
				w := source.WebList
				f, s := webListFetcher(t, string(body), WebListSelectors{Item: w.Item, Link: w.Link, Title: w.Title, Date: w.Date})
				feed, err := f.Fetch(t.Context(), s)
				require.NoError(t, err)
				require.Len(t, feed.Items, tc.count)
				assert.Equal(t, tc.title, feed.Items[0].Title)
				assert.True(t, strings.HasSuffix(feed.Items[0].URL, tc.path))
				assert.Equal(t, tc.date, feed.Items[0].Published.Format(time.RFC3339))
			}
		})
	}
}

func TestWebListResolvesRedirectAndPreservesInlineTitle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/news/list", http.StatusFound)
			return
		}
		fmt.Fprint(w, `<article><a href="release#section"><h2>模<b>型</b>发布</h2></a><time>2026-10-27</time></article>`)
	}))
	defer server.Close()
	f := NewFeedFetcher(fetch.New(true, config.Fetch{RequestTimeout: time.Second, Retry: config.Retry{MaxAttempts: 1}}), map[string]WebListSelectors{"web": {Item: "article", Link: "a", Title: "h2", Date: "time"}})
	feed, err := f.Fetch(t.Context(), biz.CollectSource{ID: "web", Kind: biz.SourceKindWebList, FeedURL: server.URL + "/start"})
	require.NoError(t, err)
	require.Len(t, feed.Items, 1)
	assert.Equal(t, server.URL+"/news/release", feed.Items[0].URL)
	assert.Equal(t, "模型发布", feed.Items[0].Title)
}
