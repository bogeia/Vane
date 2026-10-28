package agent

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/labstack/echo/v5"
)

func (a *AgentService) LLMSHTTP(c *echo.Context) error {
	const contentType = "text/plain; charset=utf-8"
	c.Response().Header().Set("Content-Type", contentType)
	c.Response().Header().Set("Cache-Control", "public, max-age=60")
	if c.Request().Method == http.MethodHead {
		return c.NoContent(http.StatusOK)
	}
	return c.Blob(http.StatusOK, contentType, []byte(a.LLMS()))
}

func (a *AgentService) GuideHTTP(c *echo.Context) error {
	return agentResponse(c, a.Guide())
}

func (a *AgentService) LatestHTTP(c *echo.Context) error {
	query := c.QueryParams()
	input := AgentLatestInput{Window: query.Get("window"), Mode: query.Get("mode"), Topic: query.Get("topic"), Limit: agentQueryLimit(query)}
	return agentResponse(c, a.Latest(c.Request().Context(), input, AgentHTTP))
}

func (a *AgentService) SearchHTTP(c *echo.Context) error {
	query := c.QueryParams()
	input := AgentSearchInput{Q: query.Get("q"), Window: query.Get("window"), Limit: agentQueryLimit(query)}
	if query.Has("topic") {
		value := query.Get("topic")
		input.Topic = &value
	}
	if query.Has("mode") {
		value := query.Get("mode")
		input.Mode = &value
	}
	return agentResponse(c, a.Search(c.Request().Context(), input, AgentHTTP))
}

func (a *AgentService) HotHTTP(c *echo.Context) error {
	return agentResponse(c, a.Hot(c.Request().Context(), AgentHotInput{Limit: agentQueryLimit(c.QueryParams())}, AgentHTTP))
}

func (a *AgentService) StoryHTTP(c *echo.Context) error {
	return agentResponse(c, a.Story(c.Request().Context(), AgentStoryInput{ID: c.Param("id"), Limit: agentQueryLimit(c.QueryParams())}, AgentHTTP))
}

func (a *AgentService) DailyHTTP(c *echo.Context) error {
	return agentResponse(c, a.Daily(c.Request().Context(), AgentDailyInput{Date: c.Param("date")}, AgentHTTP))
}

func (a *AgentService) NotFoundHTTP(c *echo.Context) error {
	return agentResponse(c, a.NotFound())
}

func agentResponse(c *echo.Context, answer AgentAnswer) error {
	const contentType = "text/markdown; charset=utf-8"
	c.Response().Header().Set("Content-Type", contentType)
	if c.Request().Method == http.MethodHead {
		return c.NoContent(answer.Status)
	}
	return c.Blob(answer.Status, contentType, []byte(answer.Markdown))
}

func agentQueryLimit(query url.Values) *int {
	if !query.Has("limit") {
		return nil
	}
	value, err := strconv.Atoi(query.Get("limit"))
	if err != nil {
		value = 0
	}
	return &value
}
