package agent

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/config"
)

func TestMCPKnownFieldTypeErrorsUseServiceValidationMessages(t *testing.T) {
	agent := newTestAgentService(&latestAgentItems{}, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return NewMCPServer(agent) }, nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	cases := []struct {
		name, tool, field, want string
		value                   any
		args                    map[string]any
	}{
		{name: "latest limit wrong type", tool: "vane_get_latest", field: "limit", value: "abc", want: "limit 只能是 1–30 的整数。"},
		{name: "latest limit null", tool: "vane_get_latest", field: "limit", value: nil, want: "limit 只能是 1–30 的整数。"},
		{name: "latest window wrong type", tool: "vane_get_latest", field: "window", value: float64(24), want: "window 只能是 24h 或 7d。"},
		{name: "latest window null", tool: "vane_get_latest", field: "window", value: nil, want: "window 只能是 24h 或 7d。"},
		{name: "latest mode wrong type", tool: "vane_get_latest", field: "mode", value: true, want: "mode 只能是 selected 或 all。"},
		{name: "latest mode null", tool: "vane_get_latest", field: "mode", value: nil, want: "mode 只能是 selected 或 all。"},
		{name: "latest topic wrong type", tool: "vane_get_latest", field: "topic", value: true, want: "topic 不是 Vane 的主题，可用的主题见使用说明。"},
		{name: "latest topic null", tool: "vane_get_latest", field: "topic", value: nil, want: "topic 不是 Vane 的主题，可用的主题见使用说明。"},
		{name: "search q wrong type", tool: "vane_search", field: "q", value: float64(1), want: "q 必须是 1–200 个字符，且不能含 NUL。"},
		{name: "search q null", tool: "vane_search", field: "q", value: nil, want: "q 必须是 1–200 个字符，且不能含 NUL。"},
		{name: "search window wrong type", tool: "vane_search", field: "window", value: float64(24), want: "window 只能是 24h 或 7d。"},
		{name: "search window null", tool: "vane_search", field: "window", value: nil, want: "window 只能是 24h 或 7d。"},
		{name: "search limit wrong type", tool: "vane_search", field: "limit", value: "abc", want: "limit 只能是 1–30 的整数。"},
		{name: "search limit null", tool: "vane_search", field: "limit", value: nil, want: "limit 只能是 1–30 的整数。"},
		{name: "hot limit wrong type", tool: "vane_get_hot_topics", field: "limit", value: "abc", want: "limit 只能是 1–20 的整数。"},
		{name: "hot limit null", tool: "vane_get_hot_topics", field: "limit", value: nil, want: "limit 只能是 1–20 的整数。"},
		{name: "story id wrong type", tool: "vane_get_story", field: "story_id", value: "42", want: "story_id 事件编号只能是正整数。"},
		{name: "story id null", tool: "vane_get_story", field: "story_id", value: nil, want: "story_id 事件编号只能是正整数。"},
		{name: "story limit wrong type", tool: "vane_get_story", field: "limit", value: "abc", want: "limit 只能是 1–50 的整数。"},
		{name: "story limit null", tool: "vane_get_story", field: "limit", value: nil, want: "limit 只能是 1–50 的整数。"},
		{name: "daily date wrong type", tool: "vane_get_daily", field: "date", value: float64(20261026), want: "date 必须是 YYYY-MM-DD 格式的有效日期。"},
		{name: "daily date null", tool: "vane_get_daily", field: "date", value: nil, want: "date 必须是 YYYY-MM-DD 格式的有效日期。"},
		{name: "search topic forbidden null", tool: "vane_search", field: "topic", value: nil, args: map[string]any{"q": "OpenAI"}, want: "搜索不接受 topic 或 mode 参数。"},
		{name: "search topic forbidden wrong type", tool: "vane_search", field: "topic", value: true, args: map[string]any{"q": "OpenAI"}, want: "搜索不接受 topic 或 mode 参数。"},
		{name: "search mode forbidden null", tool: "vane_search", field: "mode", value: nil, args: map[string]any{"q": "OpenAI"}, want: "搜索不接受 topic 或 mode 参数。"},
		{name: "search mode forbidden wrong type", tool: "vane_search", field: "mode", value: true, args: map[string]any{"q": "OpenAI"}, want: "搜索不接受 topic 或 mode 参数。"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.args
			if args == nil {
				args = map[string]any{}
			}
			args[tc.field] = tc.value
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.tool, Arguments: args})
			require.NoError(t, err)
			assert.True(t, result.IsError)
			want := "# 参数有误\n\n" + tc.want + "\n\n## 回答提示\n- 改正参数后再调用一次。\n"
			assert.Empty(t, cmp.Diff(want, mcpText(result)))
		})
	}

	t.Run("unknown field ignored", func(t *testing.T) {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "vane_search", Arguments: map[string]any{"q": "OpenAI", "ignored": "x"}})
		require.NoError(t, err)
		assert.False(t, result.IsError)
	})
	t.Run("non object uses generic error", func(t *testing.T) {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "vane_get_latest", Arguments: []any{"x"}})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, mcpText(result), "参数格式有误，请按工具说明填写。")
	})
}
