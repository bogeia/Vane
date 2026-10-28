package service

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/bogeia/vane/internal/biz"
)

func (s *SourcesService) Icon(c *echo.Context) error {
	icon, err := s.icons.Icon(c.Request().Context(), c.Param("id"))
	if errors.Is(err, biz.ErrSourceIconNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "not found")
	}
	if err != nil {
		c.Logger().ErrorContext(c.Request().Context(), "get source icon", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	version := icon.Version()
	if requested := c.QueryParam("v"); requested != "" && requested != version {
		return echo.NewHTTPError(http.StatusNotFound, "not found")
	}
	cache := "public, max-age=60"
	if c.QueryParam("v") == version {
		cache = "public, max-age=31536000, immutable"
	}
	c.Response().Header().Set("Cache-Control", cache)
	etag := `"` + version + `"`
	c.Response().Header().Set("ETag", etag)
	for _, tag := range strings.Split(c.Request().Header.Get("If-None-Match"), ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" || strings.TrimPrefix(tag, "W/") == etag {
			return c.NoContent(http.StatusNotModified)
		}
	}
	c.Response().Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'none'; sandbox")
	c.Response().Header().Set("Content-Type", icon.ContentType)
	c.Response().Header().Set("Content-Length", strconv.Itoa(len(icon.Content)))
	if c.Request().Method == http.MethodHead {
		return c.NoContent(http.StatusOK)
	}
	return c.Blob(http.StatusOK, icon.ContentType, icon.Content)
}
