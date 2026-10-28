package data

import (
	"github.com/google/wire"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/infra/llm"
	"github.com/bogeia/vane/internal/infra/push"
)

var ProviderSet = wire.NewSet(
	NewData,
	NewSourceIconsRepo,
	NewSourceIconFetcher,
	NewSourceAlertsRepo,
	NewOperationsRepo,
	NewItemsRepo,
	NewStoriesRepo,
	NewHotRepo,
	NewReportsRepo,
	NewRealtimeRepo,
	NewCollectionRepo,
	NewWorkRepo,
	NewEvalRepo,
	NewWriter,
	wire.Bind(new(biz.Writer), new(*llm.Client)),
	NewPushClient,
	wire.Bind(new(biz.RealtimeSender), new(*push.Client)),
	wire.Bind(new(biz.MessageSender), new(*push.Client)),
	NewPusher,
	NewProductionFeedFetcher,
	wire.Bind(new(biz.FeedFetcher), new(*FeedFetcher)),
)

// StatusProviderSet opens existing storage without migrating or synchronizing sources.
var StatusProviderSet = wire.NewSet(Open, NewOperationsRepo)

var (
	EvalExportProviderSet = wire.NewSet(Open, NewEvalRepo)
	EvaluationProviderSet = wire.NewSet(NewWriter, wire.Bind(new(biz.EvaluationModel), new(*llm.Client)))
)

var SourcesImportProviderSet = wire.NewSet(NewProductionFeedFetcher, wire.Bind(new(biz.FeedFetcher), new(*FeedFetcher)))

var TopicsProviderSet = wire.NewSet(Open, NewTopicsRepo)
