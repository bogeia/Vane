package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/data"
	"github.com/bogeia/vane/internal/job"
	"github.com/bogeia/vane/internal/server"
	"github.com/bogeia/vane/internal/service"
	agentservice "github.com/bogeia/vane/internal/service/agent"
)

type testApp struct {
	Router    *echo.Echo
	Data      *data.Data
	Collector *biz.Collector
	Processor *biz.Processor
	Jobs      *job.Runner
	Reports   *biz.ReportsUseCase
	Realtime  *biz.RealtimeUseCase
}

// Test-only assembly mirrors the production Wire graph in cmd/vane.
func newTestApp(ctx context.Context, cfg config.Config, log *slog.Logger, now func() time.Time) (*testApp, func(), error) {
	return newTestAppWithFetcher(ctx, cfg, log, now, data.NewProductionFeedFetcher(cfg))
}

func newTestStatus(ctx context.Context, cfg config.Config) (*biz.OperationsUseCase, func(), error) {
	store, cleanup, err := data.Open(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	return biz.NewOperationsUseCase(data.NewOperationsRepo(store)), cleanup, nil
}

func newTestAppWithFetcher(ctx context.Context, cfg config.Config, log *slog.Logger, now func() time.Time, fetcher biz.FeedFetcher) (*testApp, func(), error) {
	dataData, cleanup, err := data.NewData(ctx, cfg, log)
	if err != nil {
		return nil, nil, err
	}
	serviceOperations := service.NewOperationsService(biz.NewOperationsUseCase(data.NewOperationsRepo(dataData)), log)
	items := biz.NewItemsUseCase(data.NewItemsRepo(dataData), now)
	serviceItems := service.NewItemsService(items, log)
	stories := biz.NewStoriesUseCase(data.NewStoriesRepo(dataData))
	serviceStories := service.NewStoriesService(stories, log)
	hot := biz.NewHotUseCase(data.NewHotRepo(dataData), now)
	serviceHot := service.NewHotService(hot, log)
	reports := biz.NewReportsUseCase(data.NewReportsRepo(dataData), now, data.NewPusher(cfg, data.NewPushClient(cfg)))
	serviceReports := service.NewReportsService(reports, log)
	agent := agentservice.NewAgentService(items, hot, stories, reports, cfg.Site, now, log)
	mcpServer := agentservice.NewMCPServer(agent)
	icons := biz.NewSourceIconsUseCase(biz.NewSources(cfg), data.NewSourceIconsRepo(dataData), data.NewSourceIconFetcher(cfg))
	echo := server.NewRouter(log, serviceOperations, cfg, serviceItems, serviceStories, serviceHot, serviceReports, service.NewSourcesService(biz.NewSourcesUseCase(biz.NewSources(cfg), fetcher, now), icons), agent, mcpServer)
	collector := biz.NewCollector(data.NewCollectionRepo(dataData), fetcher, biz.NewCollectorOptions(cfg), now)
	client := data.NewWriter(cfg)
	processor := biz.NewProcessor(data.NewWorkRepo(dataData), client, biz.NewProcessorOptions(cfg), now, log)
	realtime := biz.NewRealtimeUseCase(data.NewRealtimeRepo(dataData), data.NewPushClient(cfg), biz.NewRealtimeOptions(cfg), now, log)
	runner := job.NewRunner(collector, processor, log, reports, realtime, biz.NewSourceAlertsUseCase(data.NewSourceAlertsRepo(dataData), data.NewPushClient(cfg), biz.NewSourceAlertOptions(cfg)), icons)
	app := &testApp{Router: echo, Data: dataData, Collector: collector, Jobs: runner, Processor: processor, Reports: reports, Realtime: realtime}
	return app, func() {
		cleanup()
	}, nil
}

func testDB(t *testing.T, store *data.Data) *sql.DB {
	t.Helper()
	db, err := store.ORM.DB()
	if err != nil {
		t.Fatal(err)
	}
	return db
}

type itemPageResponse struct {
	Items      []service.Item `json:"items"`
	NextCursor string         `json:"next_cursor"`
}

func reportFixtureJSON(report biz.Report) ([]byte, error) {
	item := func(value biz.Item) map[string]any {
		return map[string]any{"id": value.ID, "title": value.Title, "storyId": value.StoryID}
	}
	var headline any
	if report.Headline != nil {
		headline = item(*report.Headline)
	}
	var sections []map[string]any
	if report.Sections != nil {
		sections = make([]map[string]any, 0, len(report.Sections))
		for _, section := range report.Sections {
			items := make([]map[string]any, 0, len(section.Items))
			for _, value := range section.Items {
				items = append(items, item(value))
			}
			sections = append(sections, map[string]any{"category": section.Category, "items": items})
		}
	}
	var briefs []map[string]any
	if report.Briefs != nil {
		briefs = make([]map[string]any, 0, len(report.Briefs))
		for _, value := range report.Briefs {
			briefs = append(briefs, item(value))
		}
	}
	return json.Marshal(map[string]any{"date": report.Date, "headline": headline, "highlights": []any{}, "sections": sections, "briefs": briefs})
}
