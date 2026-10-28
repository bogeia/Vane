package biz

import "github.com/google/wire"

var (
	ProviderSet       = wire.NewSet(NewSourceIconsUseCase, NewSourceAlertsUseCase, NewSourceAlertOptions, NewRealtimeUseCase, NewRealtimeOptions, SourcesProviderSet, NewItemsUseCase, NewHotUseCase, NewReportsUseCase, NewStoriesUseCase, NewOperationsUseCase, NewCollector, NewProcessor, NewCollectorOptions, NewProcessorOptions)
	StatusProviderSet = wire.NewSet(NewOperationsUseCase)
)
