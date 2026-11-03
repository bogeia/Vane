package hn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/infra/fetch"
)

func fakeClient(t *testing.T, values map[string]any) *Client {
	t.Helper()
	return &Client{get: func(_ context.Context, url string, _ http.Header) (fetch.Result, error) {
		if !strings.HasPrefix(url, apiURL) {
			t.Errorf("non-official API URL: %s", url)
		}
		key := strings.TrimSuffix(strings.TrimPrefix(url, apiURL), ".json")
		value, ok := values[key]
		if !ok {
			return fetch.Result{}, fmt.Errorf("unconfigured request: %s", key)
		}
		if err, ok := value.(error); ok {
			return fetch.Result{}, err
		}
		body, err := json.Marshal(value)
		return fetch.Result{Body: body}, err
	}}
}

func TestRecentDeduplicatesFiltersAndBoundsScan(t *testing.T) {
	now := time.Now().UTC()
	top := make([]int64, 51)
	for i := range top {
		top[i] = int64(i + 1)
	}
	values := map[string]any{"topstories": top, "newstories": []int64{1, 52, 53, 54, 55}}
	for i := range 50 {
		id := int64(i + 1)
		values[fmt.Sprintf("item/%d", id)] = item{ID: id, Type: "story", URL: "https://example.com/a", Time: now.Unix()}
	}
	values["item/52"] = item{ID: 52, Type: "story", URL: "https://example.com/old", Time: now.Add(-8 * 24 * time.Hour).Unix()}
	values["item/53"] = item{ID: 53, Type: "story", Deleted: true, Time: now.Unix()}
	values["item/54"] = item{ID: 54, Type: "job", URL: "https://example.com/job", Time: now.Unix()}
	values["item/55"] = nil
	client := fakeClient(t, values)
	original := client.get
	var active, peak, calls atomic.Int32
	client.get = func(ctx context.Context, url string, headers http.Header) (fetch.Result, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		calls.Add(1)
		return original(ctx, url, headers)
	}
	threads, err := client.Recent(t.Context(), now.Add(-7*24*time.Hour))
	if err != nil || len(threads) != 50 {
		t.Fatalf("threads=%d err=%v", len(threads), err)
	}
	if calls.Load() != 56 || peak.Load() > 4 {
		t.Fatalf("calls=%d peak=%d", calls.Load(), peak.Load())
	}
	for i, thread := range threads {
		if thread.ID != int64(i+1) {
			t.Fatalf("order=%v", threads)
		}
	}
}

func TestRecentRetainsSuccessfulSubmissionsAlongsideFailure(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	failure := errors.New("network unavailable")
	client := fakeClient(t, map[string]any{"topstories": []int64{1}, "newstories": []int64{2}, "item/1": item{ID: 1, Type: "story", URL: "https://example.com/one", Title: "A real story", Time: now.Unix()}, "item/2": failure})
	threads, err := client.Recent(t.Context(), now.Add(-time.Hour))
	if !errors.Is(err, failure) || len(threads) != 1 || threads[0].ID != 1 {
		t.Fatalf("threads=%v err=%v", threads, err)
	}
}

func TestCommentsUseLiveTopLevelPlainTextOnly(t *testing.T) {
	values := map[string]any{"item/1": item{ID: 1, Type: "story", URL: "https://example.com/article", Kids: []int64{2, 3, 4, 5, 6, 7, 8, 9, 9, 10}}}
	values["item/2"] = item{ID: 2, Type: "comment", Parent: 1, Deleted: true, Text: "deleted"}
	values["item/3"] = item{ID: 3, Type: "comment", Parent: 1, Dead: true, Text: "dead"}
	values["item/4"] = item{ID: 4, Type: "comment", Parent: 2, Text: "reply"}
	values["item/5"] = item{ID: 5, Type: "story", Parent: 1, Text: "wrong type"}
	values["item/6"] = item{ID: 6, Type: "comment", Parent: 1, Text: "<p> </p>"}
	values["item/7"] = nil
	values["item/8"] = item{ID: 8, Type: "comment", Parent: 1, Text: "<p>观点 &amp; 体验</p><script>evil()</script><p>仅供参考</p>", Kids: []int64{999}}
	values["item/9"] = item{ID: 9, Type: "comment", Parent: 1, Text: strings.Repeat("界", 1300)}
	values["item/10"] = item{ID: 11, Type: "comment", Parent: 1, Text: "wrong ID"}
	comments, err := fakeClient(t, values).Comments(t.Context(), biz.HNThread{ID: 1, URL: "https://example.com/article"})
	if err != nil || len(comments) != 2 {
		t.Fatalf("comments=%v err=%v", comments, err)
	}
	if comments[0].ID != 8 || comments[0].ParentID != 1 || comments[0].Text != "观点 & 体验 仅供参考" {
		t.Fatalf("comment=%+v", comments[0])
	}
	if utf8.RuneCountInString(comments[1].Text) != 1200 || !utf8.ValidString(comments[1].Text) {
		t.Fatal("unsafe truncation")
	}
}

func TestCommentsStopAtTenAndFailIncompleteSamples(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
		fail  bool
	}{{"ten", true, false}, {"request failure", false, true}, {"bounded scan", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			kids := make([]int64, 101)
			values := map[string]any{}
			for i := range kids {
				id := int64(i + 2)
				kids[i] = id
				values[fmt.Sprintf("item/%d", id)] = item{ID: id, Type: "comment", Parent: 1, Text: "", Deleted: !tc.valid}
				if tc.valid {
					values[fmt.Sprintf("item/%d", id)] = item{ID: id, Type: "comment", Parent: 1, Text: "有效评论"}
				}
			}
			if tc.fail {
				values["item/2"] = errors.New("failed request")
			}
			values["item/1"] = item{ID: 1, Type: "story", URL: "https://example.com/article", Kids: kids}
			comments, err := fakeClient(t, values).Comments(t.Context(), biz.HNThread{ID: 1, URL: "https://example.com/article"})
			if tc.valid {
				if err != nil || len(comments) != 10 {
					t.Fatalf("comments=%d err=%v", len(comments), err)
				}
			} else if err == nil || comments != nil {
				t.Fatalf("incomplete sample accepted: %v %v", comments, err)
			}
		})
	}
}

func TestCommentsSuccessfulDeletionAndCancellation(t *testing.T) {
	comments, err := fakeClient(t, map[string]any{"item/1": item{ID: 1, Deleted: true}}).Comments(t.Context(), biz.HNThread{ID: 1, URL: "https://example.com/article"})
	if err != nil || len(comments) != 0 {
		t.Fatalf("comments=%v err=%v", comments, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := &Client{get: func(ctx context.Context, _ string, _ http.Header) (fetch.Result, error) {
		return fetch.Result{}, ctx.Err()
	}}
	if _, err := client.Comments(ctx, biz.HNThread{ID: 1, URL: "https://example.com/article"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestCommentsRechecksDiscoveredThreadURLBeforeSampling(t *testing.T) {
	const discovered = "https://example.com/Path?version=1"
	for _, test := range []struct {
		name, current string
		valid         bool
	}{
		{"unchanged", discovered, true},
		{"equivalent normalization", "https://EXAMPLE.com:443/Path?utm_source=hn&version=1#comments", true},
		{"different path", "https://example.com/Other?version=1", false},
		{"path case", "https://example.com/path?version=1", false},
		{"different query", "https://example.com/Path?version=2", false},
		{"different protocol", "http://example.com/Path?version=1", false},
		{"different host", "https://other.example/Path?version=1", false},
		{"removed link", "", false},
		{"invalid link", "javascript:alert(1)", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fakeClient(t, map[string]any{
				"item/1": item{ID: 1, Type: "story", URL: test.current, Kids: []int64{2}},
				"item/2": item{ID: 2, Type: "comment", Parent: 1, Text: "用户评论"},
			})
			original := client.get
			commentRequests := 0
			client.get = func(ctx context.Context, url string, headers http.Header) (fetch.Result, error) {
				if strings.HasSuffix(url, "/item/2.json") {
					commentRequests++
				}
				return original(ctx, url, headers)
			}
			comments, err := client.Comments(t.Context(), biz.HNThread{ID: 1, URL: discovered})
			if test.valid {
				if err != nil || len(comments) != 1 || commentRequests != 1 {
					t.Fatalf("valid URL rejected: comments=%v requests=%d error=%v", comments, commentRequests, err)
				}
			} else if err != nil || len(comments) != 0 || commentRequests != 0 {
				t.Fatalf("confirmed changed URL did not invalidate sample: comments=%v requests=%d error=%v", comments, commentRequests, err)
			}
		})
	}
}
