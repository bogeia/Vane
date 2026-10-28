package service

import (
	"github.com/google/wire"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/service/agent"
)

// ProviderSet is service providers.
var ProviderSet = wire.NewSet(NewSourcesService, NewOperationsService, NewItemsService, NewStoriesService, NewHotService, NewReportsService, agent.ProviderSet,
	wire.FieldsOf(new(config.Config), "Site"),
)
