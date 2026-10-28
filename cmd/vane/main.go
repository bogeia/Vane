package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/urfave/cli/v3"

	"github.com/bogeia/vane/internal/biz"
	"github.com/bogeia/vane/internal/config"
	"github.com/bogeia/vane/internal/infra/logging"
	"github.com/bogeia/vane/internal/server"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := &cli.Command{Name: "vane", Usage: "AI 行业热点聚合站"}
	for _, name := range []string{"serve", "report", "status", "push"} {
		flags := []cli.Flag{&cli.StringFlag{Name: "conf", Required: true, Usage: "YAML configuration file"}}
		if name == "report" || name == "push" {
			flags = append(flags, &cli.StringFlag{Name: "date", Required: true, Usage: "YYYY-MM-DD (Asia/Shanghai)"})
		}
		if name == "status" {
			flags = append(flags, &cli.IntFlag{Name: "days", Value: 7, Usage: "source health window in days (1-90)"})
		}
		command.Commands = append(command.Commands, &cli.Command{
			Name:  name,
			Flags: flags,
			Action: func(ctx context.Context, cmd *cli.Command) error {
				if cmd.NArg() != 0 {
					return fmt.Errorf("%s does not accept positional arguments", cmd.Name)
				}
				switch cmd.Name {
				case "serve":
					return serve(ctx, cmd.String("conf"))
				case "push":
					return pushReport(ctx, cmd.String("conf"), cmd.String("date"))
				case "report":
					return report(ctx, cmd.String("conf"), cmd.String("date"), cmd.Root().Writer)
				default:
					return status(ctx, cmd.String("conf"), cmd.Int("days"), cmd.Root().Writer)
				}
			},
		})
	}
	command.Commands = append(command.Commands, evalCommand(), sourcesCommand(), topicsCommand())
	return command.Run(context.Background(), args)
}

func serve(ctx context.Context, path string) (err error) {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	listener, err := server.Listen(ctx, cfg.Server)
	if err != nil {
		return err
	}
	defer listener.Close()
	logger, closeLog, err := logging.New(cfg.Log)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeLog()) }()
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	application, cleanup, err := NewServe(ctx, cfg, logger, time.Now)
	if err != nil {
		return err
	}
	defer cleanup()
	return application.Run(ctx, cfg.Server, listener)
}

func status(ctx context.Context, path string, days int, output io.Writer) error {
	if days < 1 || days > 90 {
		return fmt.Errorf("days must be between 1 and 90")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	operations, cleanup, err := NewStatus(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()
	result, err := operations.Status(ctx, days)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(toOutputStatus(result))
}

func report(ctx context.Context, path, date string, output io.Writer) error {
	if _, _, err := biz.ReportWindow(date); err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	reports, cleanup, err := NewReport(ctx, cfg, slog.Default(), time.Now)
	if err != nil {
		return err
	}
	defer cleanup()
	result, err := reports.Generate(ctx, date)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(toOutputReport(result))
}

func pushReport(ctx context.Context, path, date string) error {
	if _, _, err := biz.ReportWindow(date); err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	reports, cleanup, err := NewReport(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)
	if err != nil {
		return err
	}
	defer cleanup()
	return reports.Push(ctx, date)
}

func topicsCommand() *cli.Command {
	return &cli.Command{Name: "topics", Commands: []*cli.Command{{
		Name: "rebuild", Usage: "按当前规则重算已完成资料的主题",
		Flags: []cli.Flag{&cli.StringFlag{Name: "conf", Required: true, Usage: "YAML configuration file"}},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 0 {
				return fmt.Errorf("topics rebuild does not accept positional arguments")
			}
			cfg, err := config.Load(cmd.String("conf"))
			if err != nil {
				return err
			}
			topics, cleanup, err := NewTopicsRebuild(ctx, cfg)
			if err != nil {
				return err
			}
			defer cleanup()
			result, err := topics.Rebuild(ctx)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.Root().Writer).Encode(struct {
				Checked int64 `json:"checked"`
				Updated int64 `json:"updated"`
			}{result.Checked, result.Updated})
		},
	}}}
}
