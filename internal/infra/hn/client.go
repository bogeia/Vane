// Package hn samples public top-level discussions from the official HN API.
package hn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/infra/fetch"
)

const (
	apiURL               = "https://hacker-news.firebaseio.com/v0/"
	maxCommentCandidates = 100
)

type Client struct {
	get func(context.Context, string, http.Header) (fetch.Result, error)
}

func New(client *fetch.Client) *Client { return &Client{get: client.Get} }

type item struct {
	ID      int64   `json:"id"`
	Type    string  `json:"type"`
	URL     string  `json:"url"`
	Title   string  `json:"title"`
	Time    int64   `json:"time"`
	Parent  int64   `json:"parent"`
	Text    string  `json:"text"`
	Kids    []int64 `json:"kids"`
	Deleted bool    `json:"deleted"`
	Dead    bool    `json:"dead"`
}

func (c *Client) read(ctx context.Context, path string, value any) error {
	response, err := c.get(ctx, apiURL+path+".json", nil)
	if err != nil {
		return fmt.Errorf("HN API: %w", err)
	}
	if err := json.Unmarshal(response.Body, value); err != nil {
		return fmt.Errorf("invalid HN API JSON: %w", err)
	}
	return nil
}

func (c *Client) Recent(ctx context.Context, since time.Time) ([]biz.HNThread, error) {
	ids := make([]int64, 0, 100)
	seen := map[int64]bool{}
	var partial error
	for _, feed := range []string{"topstories", "newstories"} {
		var list []int64
		if err := c.read(ctx, feed, &list); err != nil {
			partial = errors.Join(partial, err)
			continue
		}
		if list == nil {
			partial = errors.Join(partial, fmt.Errorf("invalid HN feed"))
			continue
		}
		for _, id := range list[:min(len(list), 50)] {
			if id > 0 && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	// Preserve API order while keeping the entire scan at at most four requests in flight.
	records := make([]item, len(ids))
	failures := make([]error, len(ids))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(4, len(ids)) {
		workers.Go(func() {
			for i := range jobs {
				failures[i] = c.read(ctx, fmt.Sprintf("item/%d", ids[i]), &records[i])
			}
		})
	}
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	threads := make([]biz.HNThread, 0)
	for i, record := range records {
		if failures[i] != nil {
			partial = errors.Join(partial, failures[i])
			continue
		}
		published := time.Unix(record.Time, 0).UTC()
		if record.ID != ids[i] || record.Deleted || record.Dead || record.Type != "story" || record.URL == "" || published.Before(since) {
			continue
		}
		threads = append(threads, biz.HNThread{ID: record.ID, URL: record.URL, Title: record.Title, PublishedAt: published})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return threads, partial
}

func (c *Client) Comments(ctx context.Context, thread biz.HNThread) ([]biz.HNComment, error) {
	if thread.ID <= 0 {
		return nil, fmt.Errorf("invalid HN thread ID")
	}
	var parent item
	if err := c.read(ctx, fmt.Sprintf("item/%d", thread.ID), &parent); err != nil {
		return nil, err
	}
	comments := make([]biz.HNComment, 0, 10)
	if parent.Deleted || parent.Dead || parent.ID == 0 {
		return comments, nil
	}
	if parent.ID != thread.ID || parent.Type != "story" {
		return nil, fmt.Errorf("invalid HN thread")
	}
	discoveredURL, err := biz.NormalizeDiscussionURL(thread.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid discovered HN thread URL")
	}
	currentURL, err := biz.NormalizeDiscussionURL(parent.URL)
	if err != nil || currentURL != discoveredURL {
		// A confirmed link change invalidates this association rather than preserving stale points.
		return comments, nil
	}
	seen := map[int64]bool{}
	for i, id := range parent.Kids {
		if len(comments) == 10 {
			break
		}
		if i == maxCommentCandidates {
			return nil, fmt.Errorf("HN comment sampling limit exceeded")
		}
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		var comment item
		if err := c.read(ctx, fmt.Sprintf("item/%d", id), &comment); err != nil {
			return nil, err
		}
		if comment.ID != id || comment.Deleted || comment.Dead || comment.Type != "comment" || comment.Parent != thread.ID {
			continue
		}
		text := plainText(comment.Text)
		if text == "" {
			continue
		}
		comments = append(comments, biz.HNComment{ID: id, ParentID: thread.ID, Text: text})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return comments, nil
}

func plainText(raw string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(raw))
	var text strings.Builder
	hidden := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			runes := []rune(strings.Join(strings.Fields(text.String()), " "))
			return string(runes[:min(len(runes), 1200)])
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			switch string(name) {
			case "script", "style":
				hidden++
			case "p", "br", "div", "li", "pre":
				text.WriteByte(' ')
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			if (string(name) == "script" || string(name) == "style") && hidden > 0 {
				hidden--
			}
			text.WriteByte(' ')
		case html.TextToken:
			if hidden == 0 {
				text.Write(tokenizer.Text())
			}
		}
	}
}

var _ biz.HNDiscussionFetcher = (*Client)(nil)
