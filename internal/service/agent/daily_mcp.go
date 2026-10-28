package agent

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerAgentDailyTool(server *mcp.Server, agent *AgentService) {
	schema, err := jsonschema.For[AgentDailyInput](nil)
	if err != nil {
		panic(err)
	}
	schema.AdditionalProperties = nil
	server.AddTool(&mcp.Tool{
		Name:        "vane_get_daily",
		Description: "查 " + agent.site.Name + " 最新一期或指定日期的日报，包含头条、看点、分类栏目和快讯。用户问今天的日报或某天的日报时用；日报是每天 08:00 出刊的固定成品。",
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input AgentDailyInput
		if field, ok := decodeAgentArguments(request.Params.Arguments, &input, "date"); !ok {
			return agentToolResult(agent.invalidParameter(field, 0, AgentMCP)), nil
		}
		return agentToolResult(agent.Daily(ctx, input, AgentMCP)), nil
	})
}
