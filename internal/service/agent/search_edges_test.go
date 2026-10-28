package agent

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
)

func TestAgentSearchLogsInternalErrorWithoutExposingIt(t *testing.T) {
	const private = "private database connection detail"
	var logs bytes.Buffer
	items := &latestAgentItems{searchErr: errors.New(private)}
	agent := newTestAgentService(items, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(&logs, nil)))
	answer := agent.Search(t.Context(), AgentSearchInput{Q: "OpenAI"}, AgentHTTP)
	require.Equal(t, 500, answer.Status)
	assert.NotContains(t, answer.Markdown, private)
	assert.Contains(t, logs.String(), private)
}

func TestAgentSearchEmptyResultsExplainTheWindowForHTTPAndMCP(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     AgentSearchInput
		transport AgentTransport
		phrase    string
	}{
		{"week default", AgentSearchInput{Q: "OpenAI"}, AgentHTTP, "如实告诉用户 Vane 最近 7 天没有这方面的报道；更早的内容这里查不到。"},
		{"day HTTP", AgentSearchInput{Q: "OpenAI", Window: "24h"}, AgentHTTP, "过去 24 小时没有这方面的报道（可以用 window=7d 看最近一周）。"},
		{"day MCP", AgentSearchInput{Q: "OpenAI", Window: "24h"}, AgentMCP, "过去 24 小时没有这方面的报道（可以把 window 设为 7d 看最近一周）。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := &latestAgentItems{search: biz.SearchResult{ItemPage: biz.ItemPage{Items: []biz.Item{}}}}
			agent := newTestAgentService(items, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
			answer := agent.Search(t.Context(), tc.input, tc.transport)
			require.Equal(t, 200, answer.Status)
			assert.Contains(t, answer.Markdown, "精选和全部动态里都没有相关报道。")
			assert.Contains(t, answer.Markdown, tc.phrase)
			assert.Equal(t, 1, items.searchCalls)
			if tc.input.Window == "" {
				assert.Equal(t, biz.Window7d, items.searchQuery.Window)
			}
		})
	}
}

func TestAgentSearchValidatesQueryWindowAndLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  AgentSearchInput
		status int
		phrase string
	}{
		{"whitespace only", AgentSearchInput{Q: " \t\n "}, 400, "q 必须是 1–200 个字符"},
		{"too long", AgentSearchInput{Q: strings.Repeat("界", 201)}, 400, "q 必须是 1–200 个字符"},
		{"NUL", AgentSearchInput{Q: "OpenAI\x00"}, 400, "q 必须是 1–200 个字符"},
		{"forbidden topic", AgentSearchInput{Q: "OpenAI", Topic: stringPointer("agent")}, 400, "搜索不接受 topic 或 mode 参数"},
		{"forbidden mode", AgentSearchInput{Q: "OpenAI", Mode: stringPointer("all")}, 400, "搜索不接受 topic 或 mode 参数"},
		{"bad window", AgentSearchInput{Q: "OpenAI", Window: "1d"}, 400, "window 只能是 24h 或 7d"},
		{"zero limit", AgentSearchInput{Q: "OpenAI", Limit: intPointer(0)}, 400, "limit 只能是 1–30"},
		{"too many results", AgentSearchInput{Q: "OpenAI", Limit: intPointer(31)}, 400, "limit 只能是 1–30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := &latestAgentItems{search: biz.SearchResult{ItemPage: biz.ItemPage{Items: []biz.Item{}}}}
			agent := newTestAgentService(items, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
			answer := agent.Search(t.Context(), tc.input, AgentHTTP)
			require.Equal(t, tc.status, answer.Status)
			assert.Contains(t, answer.Markdown, tc.phrase)
			assert.Zero(t, items.searchCalls, "invalid input must not call the use case")
		})
	}

	t.Run("200 runes after trimming", func(t *testing.T) {
		query := strings.Repeat("界", 200)
		items := &latestAgentItems{search: biz.SearchResult{ItemPage: biz.ItemPage{Items: []biz.Item{}}}}
		agent := newTestAgentService(items, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
		answer := agent.Search(t.Context(), AgentSearchInput{Q: " " + query + " "}, AgentHTTP)
		require.Equal(t, 200, answer.Status)
		assert.Equal(t, query, items.searchQuery.Q)
		assert.Equal(t, 200, utf8.RuneCountInString(items.searchQuery.Q))
	})
}

func TestAgentSearchNextPageHintsChangeAtMaximumLimit(t *testing.T) {
	for _, tc := range []struct {
		limit  int
		phrase string
	}{
		{1, "后面还有，调大 limit（最多 30）可以多看。"},
		{30, "后面还有，请换更具体的关键词。"},
	} {
		t.Run(strconv.Itoa(tc.limit), func(t *testing.T) {
			items := &latestAgentItems{search: biz.SearchResult{ItemPage: biz.ItemPage{
				Items: []biz.Item{{ID: "1", Title: "结果"}}, NextCursor: &biz.Cursor{},
			}}}
			agent := newTestAgentService(items, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
			answer := agent.Search(t.Context(), AgentSearchInput{Q: "OpenAI", Limit: intPointer(tc.limit)}, AgentHTTP)
			require.Equal(t, 200, answer.Status)
			assert.Contains(t, answer.Markdown, tc.phrase)
		})
	}
}

func stringPointer(value string) *string { return &value }
