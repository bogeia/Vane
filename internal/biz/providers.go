package biz

import (
	"github.com/google/wire"

	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/retry"
)

func workerOptions(c config.Worker) WorkerOptions {
	return WorkerOptions{AttemptTimeout: c.AttemptTimeout, Count: c.Count, Retry: retry.Policy(c.Retry)}
}

func selectionThresholds(c config.Thresholds) Thresholds {
	return Thresholds{Official: c.Official, Media: c.Media}
}

func NewCollectorOptions(c config.Config) CollectorOptions {
	options := CollectorOptions{Enabled: c.Collect.Enabled, Kinds: make(map[string]string, len(c.Sources)), Block: make(map[string][][]string, len(c.Sources))}
	for _, source := range c.Sources {
		options.Block[source.ID] = source.Block
		options.Kinds[source.ID] = source.Kind
	}
	return options
}

func NewProcessorOptions(c config.Config) ProcessorOptions {
	return ProcessorOptions{Enabled: c.LLM.Enabled, Worker: workerOptions(c.Worker), Thresholds: selectionThresholds(c.Selection.Thresholds)}
}

func NewEvaluationOptions(c config.Config) EvaluationOptions {
	return EvaluationOptions{Worker: workerOptions(c.Worker), Thresholds: selectionThresholds(c.Selection.Thresholds)}
}

var (
	EvaluationProviderSet = wire.NewSet(NewEvaluationUseCase, NewEvaluationOptions)
	EvalExportProviderSet = wire.NewSet(NewEvalExportUseCase)
)

func NewSources(c config.Config) Sources {
	sources := make(Sources, 0, len(c.Sources))
	for _, source := range c.Sources {
		iconURL := ""
		if source.IconURL != nil {
			iconURL = *source.IconURL
		}
		sources = append(sources, Source{IconURL: iconURL, Kind: source.Kind, ID: source.ID, Name: source.Name, Tier: source.Tier, FeedURL: source.FeedURL, Enabled: source.Enabled, IntervalMinutes: source.IntervalMinutes})
	}
	return sources
}

var SourcesProviderSet = wire.NewSet(NewSourcesUseCase, NewSources)

func NewRealtimeOptions(c config.Config) RealtimeOptions {
	options := RealtimeOptions{}
	for _, hook := range c.Push.Webhooks {
		options.Groups = append(options.Groups, RealtimeGroup{Name: hook.Name, Enabled: hook.Realtime.Enabled, Scope: hook.Realtime.Scope, Topics: hook.Realtime.Topics, Words: hook.Realtime.Words})
	}
	return options
}

var TopicsProviderSet = wire.NewSet(NewTopicsUseCase)

func NewSourceAlertOptions(c config.Config) SourceAlertOptions {
	var groups []string
	for _, webhook := range c.Push.Webhooks {
		if webhook.Alerts {
			groups = append(groups, webhook.Name)
		}
	}
	return SourceAlertOptions{Groups: groups}
}
