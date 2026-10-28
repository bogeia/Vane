package server

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/service"
	agentservice "github.com/bogeia/vane/internal/service/agent"
)

// NewRouter constructs the transport, then registers its API, documents and SPA.
func NewRouter(log *slog.Logger, operations *service.OperationsService, cfg config.Config, items *service.ItemsService, stories *service.StoriesService, hot *service.HotService, reports *service.ReportsService, sources *service.SourcesService, agent *agentservice.AgentService, mcpServer *mcp.Server) *echo.Echo {
	router := echo.New()
	router.Logger = log
	router.HTTPErrorHandler = httpErrorHandler
	router.Use(httpMiddleware(log, cfg.Server.RequestTimeout)...)
	registerAPIRoutes(router, operations, cfg.Site, items, stories, hot, reports)
	registerDocumentRoutes(router, cfg.Site, items, reports, sources)
	registerAgentRoutes(router, agent, mcpServer, log)
	registerStaticRoutes(router, cfg.Web.Dir)
	return router
}

func httpMiddleware(log *slog.Logger, timeout time.Duration) []echo.MiddlewareFunc {
	return []echo.MiddlewareFunc{
		securityHeaders(),
		// Logger handles the recovered error once, including the final 500 status.
		requestLogger(log),
		cacheAPIResponses,
		middleware.Recover(),
		requestDeadline(timeout),
	}
}
