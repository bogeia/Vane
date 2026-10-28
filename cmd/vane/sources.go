package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"go.yaml.in/yaml/v3"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/biz/taxonomy"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/service/opml"
)

func sourcesCommand() *cli.Command {
	return &cli.Command{Name: "sources", Commands: []*cli.Command{{Name: "import", Flags: []cli.Flag{
		&cli.StringFlag{Name: "conf", Required: true}, &cli.StringFlag{Name: "opml", Required: true}, &cli.StringFlag{Name: "tier", Required: true}, &cli.IntFlag{Name: "interval", Required: true},
	}, Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.NArg() != 0 {
			return fmt.Errorf("sources import does not accept positional arguments")
		}
		tier, interval := cmd.String("tier"), cmd.Int("interval")
		if (tier != taxonomy.TierOfficial && tier != taxonomy.TierMedia) || interval <= 0 || int64(interval) > int64((1<<63-1)/time.Minute) {
			return fmt.Errorf("invalid tier or interval")
		}
		cfg, err := config.Load(cmd.String("conf"))
		if err != nil {
			return err
		}
		file, err := os.Open(cmd.String("opml"))
		if err != nil {
			return fmt.Errorf("open OPML: %w", err)
		}
		defer file.Close()
		candidates, err := opml.Parse(file)
		if err != nil {
			return err
		}
		importer := NewSourcesImport(cfg, time.Now)
		return importSources(ctx, importer, candidates, tier, interval, cmd.Root().Writer, cmd.Root().ErrWriter)
	}}, {
		Name: "preview", Flags: []cli.Flag{&cli.StringFlag{Name: "conf", Required: true}, &cli.StringFlag{Name: "id", Required: true}},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 0 {
				return fmt.Errorf("sources preview does not accept positional arguments")
			}
			cfg, err := config.Load(cmd.String("conf"))
			if err != nil {
				return err
			}
			return previewSources(ctx, NewSourcesImport(cfg, time.Now), cmd.String("id"), cmd.Root().Writer)
		},
	}}}
}

type sourceOutput struct {
	Kind            string     `yaml:"kind"`
	Block           [][]string `yaml:"block"`
	ID              string     `yaml:"id"`
	Name            string     `yaml:"name"`
	FeedURL         string     `yaml:"feed_url"`
	Enabled         bool       `yaml:"enabled"`
	IntervalMinutes int        `yaml:"interval_minutes"`
}

func importSources(ctx context.Context, importer *biz.SourcesUseCase, candidates []biz.SourceCandidate, tier string, interval int, output, diagnostics io.Writer) error {
	result, err := importer.Import(ctx, candidates, tier, interval)
	if err != nil {
		return err
	}
	for _, skip := range result.Skipped {
		if _, err := fmt.Fprintf(diagnostics, "%s: %s\n", skip.URL, skip.Reason); err != nil {
			return err
		}
	}
	if len(result.Sources) == 0 {
		return nil
	}
	sources := make([]sourceOutput, 0, len(result.Sources))
	for _, source := range result.Sources {
		sources = append(sources, sourceOutput{Kind: source.Kind, Block: [][]string{}, ID: source.ID, Name: source.Name, FeedURL: source.FeedURL, Enabled: source.Enabled, IntervalMinutes: source.IntervalMinutes})
	}
	var document bytes.Buffer
	encoder := yaml.NewEncoder(&document)
	encoder.SetIndent(2)
	defer encoder.Close()
	if err := encoder.Encode(sources); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "# 粘贴到 sources.%s 下\n    %s\n", tier, strings.ReplaceAll(strings.TrimSuffix(document.String(), "\n"), "\n", "\n    "))
	return err
}

func previewSources(ctx context.Context, sources *biz.SourcesUseCase, id string, output io.Writer) error {
	preview, err := sources.Preview(ctx, id)
	if err != nil {
		return err
	}
	type itemOutput struct {
		Title       string    `json:"title"`
		URL         string    `json:"url"`
		PublishedAt time.Time `json:"published_at"`
	}
	result := struct {
		ID    string       `json:"id"`
		Kind  string       `json:"kind"`
		Items []itemOutput `json:"items"`
	}{ID: preview.ID, Kind: preview.Kind, Items: make([]itemOutput, 0, len(preview.Items))}
	for _, item := range preview.Items {
		result.Items = append(result.Items, itemOutput{Title: item.Title, URL: item.URL, PublishedAt: item.PublishedAt})
	}
	return json.NewEncoder(output).Encode(result)
}
