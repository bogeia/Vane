package server

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	agentservice "github.com/bogeia/vane/internal/service/agent"
)

func registerAgentRoutes(router *echo.Echo, agent *agentservice.AgentService, mcpServer *mcp.Server, log *slog.Logger) {
	router.GET("/llms.txt", agent.LLMSHTTP)
	router.HEAD("/llms.txt", agent.LLMSHTTP)
	router.GET("/api/agent", agent.GuideHTTP)
	router.HEAD("/api/agent", agent.GuideHTTP)
	router.GET("/api/agent/latest", agent.LatestHTTP)
	router.HEAD("/api/agent/latest", agent.LatestHTTP)
	router.GET("/api/agent/search", agent.SearchHTTP)
	router.HEAD("/api/agent/search", agent.SearchHTTP)
	router.GET("/api/agent/hot", agent.HotHTTP)
	router.HEAD("/api/agent/hot", agent.HotHTTP)
	router.GET("/api/agent/stories/:id", agent.StoryHTTP)
	router.HEAD("/api/agent/stories/:id", agent.StoryHTTP)
	router.GET("/api/agent/daily", agent.DailyHTTP)
	router.HEAD("/api/agent/daily", agent.DailyHTTP)
	router.GET("/api/agent/daily/:date", agent.DailyHTTP)
	router.HEAD("/api/agent/daily/:date", agent.DailyHTTP)
	router.GET("/api/agent/*", agent.NotFoundHTTP)
	router.HEAD("/api/agent/*", agent.NotFoundHTTP)

	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, Logger: log, PropagateRequestCancellation: true,
	})
	handler := echo.WrapHandler(http.MaxBytesHandler(http.NewCrossOriginProtection().Handler(streamable), 64<<10))
	router.POST("/api/mcp", handler)
	router.GET("/api/mcp", handler)
	router.DELETE("/api/mcp", handler)
}
