package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/bogeia/vane/internal/config"
)

const (
	publicCacheControl  = "public, max-age=60"
	noStoreCacheControl = "no-store"
)

func RSSHandler(site config.Site, feed func(context.Context, config.Site) ([]byte, error)) echo.HandlerFunc {
	return func(c *echo.Context) error {
		body, err := feed(c.Request().Context(), site)
		return documentResponse(c, "application/rss+xml; charset=utf-8", body, err)
	}
}

func CategoryRSSHandler(site config.Site, items *ItemsService) echo.HandlerFunc {
	return func(c *echo.Context) error {
		body, err := items.CategoryFeed(c.Request().Context(), site, c.Param("filename"))
		if !errors.Is(err, ErrCategoryFeedNotFound) {
			return documentResponse(c, "application/rss+xml; charset=utf-8", body, err)
		}
		c.Response().Header().Set("Cache-Control", noStoreCacheControl)
		return echo.NewHTTPError(http.StatusNotFound, "Not Found")
	}
}

func OPMLHandler(site config.Site, sources *SourcesService) echo.HandlerFunc {
	return func(c *echo.Context) error {
		body, err := sources.OPML(c.Request().Context(), site)
		return documentResponse(c, "text/x-opml; charset=utf-8", body, err)
	}
}

func RobotsHandler(site config.Site) echo.HandlerFunc {
	body := []byte("User-agent: *\nAllow: /api/agent\nDisallow: /api/\nDisallow: /favorites\nSitemap: " + strings.TrimRight(site.PublicURL, "/") + "/sitemap.xml\n")
	return func(c *echo.Context) error {
		return documentResponse(c, "text/plain; charset=utf-8", body, nil)
	}
}

func SitemapHandler(site config.Site, items *ItemsService) echo.HandlerFunc {
	return func(c *echo.Context) error {
		body, err := items.Sitemap(c.Request().Context(), site)
		return documentResponse(c, "application/xml; charset=utf-8", body, err)
	}
}

func documentResponse(c *echo.Context, contentType string, body []byte, err error) error {
	if err != nil {
		c.Logger().ErrorContext(c.Request().Context(), "serve document", "error", err)
		c.Response().Header().Set("Cache-Control", noStoreCacheControl)
		return echo.NewHTTPError(http.StatusInternalServerError, "Internal Server Error")
	}
	c.Response().Header().Set("Content-Type", contentType)
	c.Response().Header().Set("Cache-Control", publicCacheControl)
	if c.Request().Method == http.MethodHead {
		return c.NoContent(http.StatusOK)
	}
	return c.Blob(http.StatusOK, contentType, body)
}
