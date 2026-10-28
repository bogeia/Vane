package agent

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
)

func TestMCPServerExposesLatestAsReadOnlyToolAndReturnsServiceMarkdown(t *testing.T) {
	items := &latestAgentItems{page: biz.ItemPage{Items: []biz.Item{}}}
	agent := newTestAgentService(items, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return NewMCPServer(agent) }, nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	var latestTool *mcp.Tool
	for _, tool := range listed.Tools {
		if tool.Name == "vane_get_latest" {
			latestTool = tool
			break
		}
	}
	require.NotNil(t, latestTool, "ListTools() omitted vane_get_latest")
	assert.Equal(t, "查 Vane 过去 24 小时或最近 7 天的精选（或全部动态），可按公司、方向、内容形态主题筛选。用户问今天、最近 AI 圈有什么消息时用。", latestTool.Description)
	require.NotNil(t, latestTool.Annotations)
	assert.True(t, latestTool.Annotations.ReadOnlyHint)
	schema, ok := latestTool.InputSchema.(map[string]any)
	require.True(t, ok, "latest InputSchema has type %T", latestTool.InputSchema)
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "latest InputSchema properties have type %T", schema["properties"])
	topic, ok := properties["topic"].(map[string]any)
	require.True(t, ok, "topic schema has type %T", properties["topic"])
	values, ok := topic["enum"].([]any)
	require.True(t, ok, "topic enum has type %T", topic["enum"])
	wantTopics := []string{"openai", "anthropic", "google", "deepseek", "qwen", "kimi", "minimax", "zhipu", "xai", "meta", "microsoft", "nvidia", "hugging-face", "cursor", "openrouter", "agent", "coding", "reasoning", "multimodal", "voice", "video", "image-gen", "rag", "on-device", "data-training", "search", "engineering", "open-source", "embodied", "mcp", "model", "product", "industry", "paper", "tutorial", "opinion"}
	gotTopics := make([]string, len(values))
	for index, value := range values {
		gotTopics[index], ok = value.(string)
		require.Truef(t, ok, "topic enum[%d] has type %T", index, value)
	}
	assert.Equal(t, wantTopics, gotTopics)
	limit, ok := properties["limit"].(map[string]any)
	require.True(t, ok, "limit schema has type %T", properties["limit"])
	assert.Equal(t, float64(1), limit["minimum"])
	assert.Equal(t, float64(30), limit["maximum"])
	window, ok := properties["window"].(map[string]any)
	require.True(t, ok, "window schema has type %T", properties["window"])
	assert.Equal(t, []any{"24h", "7d"}, window["enum"])

	args := map[string]any{"window": "24h", "mode": "selected", "topic": "openai", "limit": float64(10)}
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: latestTool.Name, Arguments: args})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	want := agent.Latest(t.Context(), AgentLatestInput{Window: "24h", Mode: "selected", Topic: "openai", Limit: intPointer(10)}, AgentMCP).Markdown
	assert.Empty(t, cmp.Diff(want, mcpText(result)))
}

func TestMCPServerReturnsSafeErrorsForInvalidLatestTypesAndRanges(t *testing.T) {
	agent := newTestAgentService(&latestAgentItems{}, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return NewMCPServer(agent) }, nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"wrong type", map[string]any{"window": float64(24)}},
		{"out of range", map[string]any{"limit": float64(31)}},
		{"null value", map[string]any{"limit": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "vane_get_latest", Arguments: tc.args})
			require.NoError(t, err)
			body := mcpText(result)
			assert.True(t, result.IsError)
			assert.NotEmpty(t, body)
			assert.True(t, containsChineseParameterError(body), "invalid result text=%q", body)
		})
	}
}

func TestMCPServerExposesHotAndStoryToolsAndReturnsServiceMarkdown(t *testing.T) {
	agent := newTestAgentService(&latestAgentItems{}, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	story := biz.Story{ID: "42", Title: "OpenAI 企业客户动态", Items: []biz.Item{{ID: "item-42", Title: "进展", Source: biz.ItemSource{Name: "Vane"}}}}
	agent.hot = hotAgentUseCaseFake{stories: []biz.HotStory{{StoryID: "42", Title: story.Title}}}
	agent.stories = storyAgentUseCaseFake{story: story}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return NewMCPServer(agent) }, nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"vane_get_hot_topics", "vane_get_story"} {
		if !slices.ContainsFunc(listed.Tools, func(tool *mcp.Tool) bool {
			return tool.Name == name && tool.Annotations != nil && tool.Annotations.ReadOnlyHint
		}) {
			t.Errorf("ListTools() omitted read-only %s", name)
		}
	}
	for _, call := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"vane_get_hot_topics", map[string]any{}, agent.Hot(t.Context(), AgentHotInput{}, AgentMCP).Markdown},
		{"vane_get_story", map[string]any{"story_id": float64(42)}, agent.Story(t.Context(), AgentStoryInput{ID: "42"}, AgentMCP).Markdown},
	} {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError || mcpText(result) != call.want {
			t.Errorf("%s result: isError=%v text=%q want=%q", call.name, result.IsError, mcpText(result), call.want)
		}
	}
}

func TestMCPServerExposesSearchToolAndMarksMissingQueryAsError(t *testing.T) {
	items := &latestAgentItems{search: biz.SearchResult{ItemPage: biz.ItemPage{Items: []biz.Item{}}}}
	agent := newTestAgentService(items, config.Site{Name: "Vane", PublicURL: "https://vane.example.com"}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return NewMCPServer(agent) }, nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	var searchTool *mcp.Tool
	for _, tool := range listed.Tools {
		if tool.Name == "vane_search" {
			searchTool = tool
			break
		}
	}
	require.NotNil(t, searchTool, "vane_search missing from MCP tool list")
	assert.Contains(t, searchTool.Description, "搜索")
	require.NotNil(t, searchTool.Annotations)
	assert.True(t, searchTool.Annotations.ReadOnlyHint)
	schema, ok := searchTool.InputSchema.(map[string]any)
	require.True(t, ok, "InputSchema type is %T", searchTool.InputSchema)
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "properties type is %T", schema["properties"])
	assert.Contains(t, properties, "q")
	assert.Contains(t, properties, "window")
	assert.Contains(t, properties, "limit")
	assert.NotContains(t, properties, "topic")
	assert.NotContains(t, properties, "mode")

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "vane_search", Arguments: map[string]any{"q": "OpenAI"}})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	want := agent.Search(t.Context(), AgentSearchInput{Q: "OpenAI"}, AgentMCP).Markdown
	assert.Equal(t, want, mcpText(result))

	missing, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "vane_search", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.True(t, missing.IsError)
	for _, field := range []string{"q", "window", "limit", "topic", "mode"} {
		t.Run(field+" null", func(t *testing.T) {
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
				Name: "vane_search", Arguments: map[string]any{"q": "OpenAI", field: nil},
			})
			require.NoError(t, err)
			assert.True(t, result.IsError)
			assert.Contains(t, mcpText(result), "参数")
			assert.NotContains(t, mcpText(result), "must be")
		})
	}
}

func TestMCPServerExposesDailyToolAndReturnsServiceMarkdown(t *testing.T) {
	date := "2026-10-26"
	item := biz.Item{ID: "1", Title: "头条", Source: biz.ItemSource{Name: "信源"}}
	reports := &agentReportsFake{latest: []biz.ReportSummary{{Date: date}}, reports: map[string]biz.Report{date: {Date: date, Headline: &item}}}
	agent := newDailyTestAgent(reports)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return NewMCPServer(agent) }, nil)
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	var dailyTool *mcp.Tool
	for _, tool := range listed.Tools {
		if tool.Name == "vane_get_daily" {
			dailyTool = tool
			break
		}
	}
	require.NotNil(t, dailyTool, "vane_get_daily missing from MCP tools")
	require.NotNil(t, dailyTool.Annotations)
	assert.True(t, dailyTool.Annotations.ReadOnlyHint)
	for _, call := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{}, agent.Daily(t.Context(), AgentDailyInput{}, AgentMCP).Markdown},
		{map[string]any{"date": date}, agent.Daily(t.Context(), AgentDailyInput{Date: date}, AgentMCP).Markdown},
	} {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: dailyTool.Name, Arguments: call.args})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Equal(t, call.want, mcpText(result))
	}
	missing, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: dailyTool.Name, Arguments: map[string]any{"date": "2026-10-20"}})
	require.NoError(t, err)
	assert.True(t, missing.IsError)
	for _, date := range []any{nil, float64(20261026)} {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: dailyTool.Name, Arguments: map[string]any{"date": date}})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, mcpText(result), "参数")
		assert.NotContains(t, mcpText(result), "must be")
	}
}

func mcpText(result *mcp.CallToolResult) string {
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

func containsChineseParameterError(value string) bool {
	return strings.Contains(value, "参数有误") && !strings.Contains(value, "schema") && !strings.Contains(value, "must be")
}
