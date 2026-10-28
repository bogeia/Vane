package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v5"
)

func spaHandler(dir string) echo.HandlerFunc {
	root := http.Dir(dir)
	files := http.FileServer(root)
	return func(c *echo.Context) error {
		path := c.Request().URL.Path
		for _, prefix := range []string{"/api", "/healthz", "/docs", "/openapi.json", "/openapi.yaml", "/schemas", "/feed", "/feed.xml", "/users"} {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return echo.NewHTTPError(http.StatusNotFound, "Not Found")
			}
		}
		file, err := root.Open(path)
		if err == nil {
			info, statErr := file.Stat()
			_ = file.Close()
			if statErr != nil {
				return statErr
			}
			if info.Mode().IsRegular() {
				files.ServeHTTP(c.Response(), c.Request())
				return nil
			}
		} else if !os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "Not Found")
		}
		return c.FileFS("index.html", os.DirFS(dir))
	}
}
