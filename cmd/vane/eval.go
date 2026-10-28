package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/urfave/cli/v3"
	"go.yaml.in/yaml/v3"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/biz/taxonomy"
	"github.com/bogeia/vane/internal/config"
)

func evalCommand() *cli.Command {
	return &cli.Command{Name: "eval", Usage: "精选评测", Commands: []*cli.Command{{
		Name: "export", Usage: "导出评测集",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "conf", Required: true, Usage: "YAML configuration file"},
			&cli.StringFlag{Name: "out", Required: true, Usage: "Output YAML file (must not exist)"},
			&cli.IntFlag{Name: "days", Value: 7, Usage: "Discovery window in days"},
			&cli.IntFlag{Name: "n", Value: 200, Usage: "Maximum items"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 0 {
				return fmt.Errorf("eval export does not accept positional arguments")
			}
			return exportGold(ctx, cmd.String("conf"), cmd.String("out"), cmd.Int("days"), cmd.Int("n"))
		},
	}, {
		Name: "run", Usage: "用当前模型重跑评测集",
		Flags: []cli.Flag{&cli.StringFlag{Name: "conf", Required: true, Usage: "YAML configuration file"}, &cli.StringFlag{Name: "gold", Required: true, Usage: "Gold YAML file"}},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 0 {
				return fmt.Errorf("eval run does not accept positional arguments")
			}
			return runGold(ctx, cmd.String("conf"), cmd.String("gold"), cmd.Root().Writer)
		},
	}}}
}

func exportGold(ctx context.Context, path, out string, days, n int) error {
	if days <= 0 || n <= 0 {
		return fmt.Errorf("days and n must be positive")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	store, cleanup, err := NewEvalExport(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()
	now := time.Now().UTC()
	gold, err := store.Export(ctx, now.AddDate(0, 0, -days), now, n)
	if err != nil {
		return err
	}
	content, err := yaml.Marshal(toOutputGoldSet(gold))
	if err != nil {
		return err
	}
	file, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create gold file: %w", err)
	}
	_, writeErr := file.Write(content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(out)
		return err
	}
	return nil
}

func runGold(ctx context.Context, path, goldPath string, output io.Writer) error {
	cfg, err := config.LoadEvaluation(path)
	if err != nil {
		return err
	}
	if !cfg.LLM.Enabled {
		return fmt.Errorf("model is disabled")
	}
	content, err := os.ReadFile(goldPath)
	if err != nil {
		return fmt.Errorf("read gold file: %w", err)
	}
	gold, err := ParseGold(content)
	if err != nil {
		return err
	}
	usecase := NewEvalRun(cfg)
	result, err := usecase.Run(ctx, gold)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(toOutputEvaluation(result))
}

func ParseGold(content []byte) (biz.GoldSet, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return biz.GoldSet{}, fmt.Errorf("invalid gold YAML")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return biz.GoldSet{}, fmt.Errorf("gold must contain one YAML document")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return biz.GoldSet{}, fmt.Errorf("gold must contain an items list")
	}
	root := document.Content[0]
	if len(root.Content) != 2 || root.Content[0].Value != "items" || root.Content[1].Kind != yaml.SequenceNode {
		return biz.GoldSet{}, fmt.Errorf("gold must contain an items list")
	}
	result := biz.GoldSet{Items: []biz.GoldItem{}}
	required := []string{"id", "url", "tier", "source", "title", "original_title", "published_at", "score1", "score2", "label", "body"}
	for index, node := range root.Content[1].Content {
		fail := func(rule string) (biz.GoldSet, error) {
			return biz.GoldSet{}, fmt.Errorf("gold item %d: %s", index+1, rule)
		}
		if node.Kind != yaml.MappingNode {
			return fail("expected object")
		}
		present := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i].Value, node.Content[i+1]
			if !slices.Contains(required, key) || present[key] {
				return fail("unknown or duplicate field")
			}
			if value.Tag == "!!null" {
				return fail(key + " must not be null")
			}
			switch key {
			case "id", "score1", "score2":
				if value.Tag != "!!int" {
					return fail(key + " must be an integer")
				}
			case "published_at":
				if value.Tag != "!!str" && value.Tag != "!!timestamp" {
					return fail("published_at must be a timestamp")
				}
			default:
				if value.Tag != "!!str" {
					return fail(key + " must be a string")
				}
			}
			present[key] = true
		}
		for _, key := range required {
			if !present[key] {
				return fail("missing " + key)
			}
		}
		var decoded outputGoldItem
		var item biz.GoldItem
		if err := node.Decode(&decoded); err != nil {
			return fail("invalid field types or timestamp")
		}
		item = fromOutputGoldItem(decoded)
		if item.ID <= 0 {
			return fail("id must be positive")
		}
		if !slices.Contains([]string{taxonomy.TierOfficial, taxonomy.TierMedia}, item.Tier) {
			return fail("invalid tier")
		}
		if !slices.Contains([]string{biz.LabelSelect, biz.LabelReject, biz.LabelEither}, item.Label) {
			return fail("invalid label")
		}
		if item.Score1 < 0 || item.Score1 > 100 || item.Score2 < 0 || item.Score2 > 100 {
			return fail("scores must be between 0 and 100")
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}
