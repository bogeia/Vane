package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v5"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/service"
)

func registerAPIRoutes(router *echo.Echo, operations *service.OperationsService, site config.Site, items *service.ItemsService, stories *service.StoriesService, hot *service.HotService, reports *service.ReportsService) {
	config := huma.DefaultConfig("Vane API", "0.1.0")
	config.Info.Description = "Vane AI 行业热点聚合站 API。"
	api := humaecho.New(router, config)
	huma.Register(api, huma.Operation{OperationID: "list-reports", Method: http.MethodGet, Path: "/api/reports", Summary: "最近60期日报"}, reports.List)
	huma.Register(api, huma.Operation{OperationID: "get-report", Method: http.MethodGet, Path: "/api/reports/{date}", Summary: "日报快照"}, reports.Get)
	huma.Register(api, huma.Operation{OperationID: "hot-ranking", Method: http.MethodGet, Path: "/api/hot", Summary: "热点榜"}, hot.List)
	huma.Register(api, huma.Operation{OperationID: "get-item", Method: http.MethodGet, Path: "/api/items/{id}", Summary: "资料详情及同事件资料"}, items.Detail)
	huma.Register(api, huma.Operation{OperationID: "get-story", Method: http.MethodGet, Path: "/api/stories/{id}", Summary: "事件资料时间线"}, stories.Get)
	huma.Register(api, huma.Operation{OperationID: "site-meta", Method: http.MethodGet, Path: "/api/meta", Summary: "站点元信息"}, service.Meta(site))
	huma.Register(api, huma.Operation{OperationID: "list-topics", Method: http.MethodGet, Path: "/api/topics", Summary: "主题目录与精选资料数"}, items.Topics)
	huma.Register(api, huma.Operation{OperationID: "list-items", Method: http.MethodGet, Path: "/api/items", Summary: "资料列表"}, items.List)
	huma.Register(api, huma.Operation{OperationID: "health", Method: http.MethodGet, Path: "/healthz", Summary: "数据库健康检查"}, operations.Health)
}

func registerDocumentRoutes(router *echo.Echo, site config.Site, items *service.ItemsService, reports *service.ReportsService, sources *service.SourcesService) {
	router.GET("/api/source-icons/:id", sources.Icon)
	router.HEAD("/api/source-icons/:id", sources.Icon)
	router.GET("/sources.opml", service.OPMLHandler(site, sources))
	router.HEAD("/sources.opml", service.OPMLHandler(site, sources))
	router.GET("/feed.xml", service.RSSHandler(site, items.SelectedFeed))
	router.GET("/feed/all.xml", service.RSSHandler(site, items.AllFeed))
	router.GET("/feed/daily.xml", service.RSSHandler(site, reports.DailyFeed))
	router.GET("/feed/category/:filename", service.CategoryRSSHandler(site, items))

	router.GET("/sitemap.xml", service.SitemapHandler(site, items))
	router.HEAD("/sitemap.xml", service.SitemapHandler(site, items))
	router.GET("/robots.txt", service.RobotsHandler(site))
	router.HEAD("/robots.txt", service.RobotsHandler(site))
}

func registerStaticRoutes(router *echo.Echo, directory string) {
	static := spaHandler(directory)
	router.GET("/*", static)
	router.HEAD("/*", static)
}
