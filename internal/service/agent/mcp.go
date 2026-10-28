package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bogeia/vane/internal/biz/taxonomy"
)

func NewMCPServer(agent *AgentService) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "vane", Version: "0.1.0"}, nil)
	schema, err := jsonschema.For[AgentLatestInput](nil)
	if err != nil {
		panic(err)
	}
	schema.AdditionalProperties = nil
	schema.Properties["window"].Enum = []any{"24h", "7d"}
	schema.Properties["window"].Default = json.RawMessage(`"24h"`)
	schema.Properties["mode"].Enum = []any{"selected", "all"}
	schema.Properties["mode"].Default = json.RawMessage(`"selected"`)
	for _, topic := range taxonomy.Topics {
		schema.Properties["topic"].Enum = append(schema.Properties["topic"].Enum, topic.Slug)
	}
	limit := schema.Properties["limit"]
	limit.Type, limit.Types = "integer", nil
	limit.Minimum, limit.Maximum = jsonschema.Ptr(1.0), jsonschema.Ptr(30.0)
	limit.Default = json.RawMessage(`10`)
	// AddTool's automatic schema errors are English; the handler keeps service errors in Chinese Markdown.
	server.AddTool(&mcp.Tool{
		Name:        "vane_get_latest",
		Description: "查 " + agent.site.Name + " 过去 24 小时或最近 7 天的精选（或全部动态），可按公司、方向、内容形态主题筛选。用户问今天、最近 AI 圈有什么消息时用。",
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input AgentLatestInput
		if field, ok := decodeAgentArguments(request.Params.Arguments, &input, "window", "mode", "topic", "limit"); !ok {
			return agentToolResult(agent.invalidParameter(field, 30, AgentMCP)), nil
		}
		return agentToolResult(agent.Latest(ctx, input, AgentMCP)), nil
	})
	registerAgentSearchTool(server, agent)
	registerAgentHotTool(server, agent)
	registerAgentStoryTool(server, agent)
	registerAgentDailyTool(server, agent)
	return server
}

func agentToolResult(answer AgentAnswer) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: answer.Status >= 400, Content: []mcp.Content{&mcp.TextContent{Text: answer.Markdown}}}
}

func decodeAgentArguments(arguments json.RawMessage, input any, nonNullFields ...string) (string, bool) {
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(arguments, &fields) != nil || fields == nil {
		return "", false
	}
	for _, name := range nonNullFields {
		if bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			return name, false
		}
	}
	if err := json.Unmarshal(arguments, input); err != nil {
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) {
			return typeError.Field, false
		}
		return "", false
	}
	return "", true
}
