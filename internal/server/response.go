package server

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"
)

func httpErrorHandler(c *echo.Context, err error) {
	response, status := echo.ResolveResponseStatus(c.Response(), err)
	if response != nil && response.Committed {
		return
	}
	problem := huma.ErrorModel{
		Type: "about:blank", Title: http.StatusText(status), Status: status,
		Detail: http.StatusText(status),
	}
	var httpError *echo.HTTPError
	if status < http.StatusInternalServerError && errors.As(err, &httpError) && httpError.Message != "" {
		problem.Detail = httpError.Message
	}
	c.Response().Header().Set(echo.HeaderContentType, "application/problem+json")
	var writeErr error
	if c.Request().Method == http.MethodHead {
		writeErr = c.NoContent(status)
	} else {
		writeErr = c.JSON(status, &problem)
	}
	if writeErr != nil {
		c.Logger().ErrorContext(c.Request().Context(), "write error response", "error", writeErr)
	}
}

const (
	publicCacheControl  = "public, max-age=60"
	noStoreCacheControl = "no-store"
)
