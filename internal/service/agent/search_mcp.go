package agent

import (
	"context"
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerAgentSearchTool(server *mcp.Server, agent *AgentService) {
	schema, err := jsonschema.For[AgentSearchInput](nil)
	if err != nil {
		panic(err)
	}
	schema.AdditionalProperties = nil
	delete(schema.Properties, "topic")
	delete(schema.Properties, "mode")
	schema.Properties["window"].Enum = []any{"24h", "7d"}
	schema.Properties["window"].Default = json.RawMessage(`"7d"`)
	limit := schema.Properties["limit"]
	limit.Type, limit.Types = "integer", nil
	limit.Minimum, limit.Maximum = jsonschema.Ptr(1.0), jsonschema.Ptr(30.0)
	limit.Default = json.RawMessage(`10`)
	server.AddTool(&mcp.Tool{
		Name:        "vane_search",
		Description: "搜索 " + agent.site.Name + " 过去 24 小时或最近 7 天收录的资料，先查精选，没有再查全部动态。用户问某家公司、产品、模型、人物或话题最近有什么消息时用。",
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var fields map[string]json.RawMessage
		var input AgentSearchInput
		if field, ok := decodeAgentArguments(request.Params.Arguments, &fields); !ok {
			return agentToolResult(agent.invalidParameter(field, 30, AgentMCP)), nil
		}
		if _, found := fields["topic"]; found {
			value := ""
			input.Topic = &value
		}
		if _, found := fields["mode"]; found {
			value := ""
			input.Mode = &value
		}
		if input.Topic != nil || input.Mode != nil {
			return agentToolResult(agent.Search(ctx, input, AgentMCP)), nil
		}
		if field, ok := decodeAgentArguments(request.Params.Arguments, &input, "q", "window", "limit"); !ok {
			return agentToolResult(agent.invalidParameter(field, 30, AgentMCP)), nil
		}
		return agentToolResult(agent.Search(ctx, input, AgentMCP)), nil
	})
}
