package agent

import (
	"context"
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerAgentHotTool(server *mcp.Server, agent *AgentService) {
	schema, err := jsonschema.For[AgentHotInput](nil)
	if err != nil {
		panic(err)
	}
	schema.AdditionalProperties = nil
	limit := schema.Properties["limit"]
	limit.Type, limit.Types = "integer", nil
	limit.Minimum, limit.Maximum = jsonschema.Ptr(1.0), jsonschema.Ptr(20.0)
	limit.Default = json.RawMessage(`10`)
	server.AddTool(&mcp.Tool{
		Name: "vane_get_hot_topics", Description: "查 " + agent.site.Name + " 当前热点榜。用户问现在什么最热、大家在讨论什么时用，按名次返回热点及来龙去脉的查询方式。",
		InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input AgentHotInput
		if field, ok := decodeAgentArguments(request.Params.Arguments, &input, "limit"); !ok {
			return agentToolResult(agent.invalidParameter(field, 20, AgentMCP)), nil
		}
		return agentToolResult(agent.Hot(ctx, input, AgentMCP)), nil
	})
}
