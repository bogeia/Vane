package agent

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type agentStoryToolInput struct {
	StoryID int64 `json:"story_id" jsonschema:"事件编号，必填正整数，使用热点榜结果提供的编号，不要猜"`
	Limit   *int  `json:"limit,omitempty" jsonschema:"时间线最多显示多少篇，1–50，默认 20"`
}

func registerAgentStoryTool(server *mcp.Server, agent *AgentService) {
	schema, err := jsonschema.For[agentStoryToolInput](nil)
	if err != nil {
		panic(err)
	}
	schema.AdditionalProperties = nil
	schema.Properties["story_id"].Minimum = jsonschema.Ptr(1.0)
	limit := schema.Properties["limit"]
	limit.Type, limit.Types = "integer", nil
	limit.Minimum, limit.Maximum = jsonschema.Ptr(1.0), jsonschema.Ptr(50.0)
	limit.Default = json.RawMessage(`20`)
	server.AddTool(&mcp.Tool{
		Name: "vane_get_story", Description: "查 " + agent.site.Name + " 某个热点的来龙去脉、报道时间线和最新进展。用户追问热点时用，事件编号必须从热点榜结果中获取。",
		InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input agentStoryToolInput
		if field, ok := decodeAgentArguments(request.Params.Arguments, &input, "story_id", "limit"); !ok {
			return agentToolResult(agent.invalidParameter(field, 50, AgentMCP)), nil
		}
		return agentToolResult(agent.Story(ctx, AgentStoryInput{ID: strconv.FormatInt(input.StoryID, 10), Limit: input.Limit}, AgentMCP)), nil
	})
}
