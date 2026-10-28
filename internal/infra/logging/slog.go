package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/bogeia/vane/internal/config"
)

func New(config config.Log) (*slog.Logger, func() error, error) {
	if err := os.MkdirAll(filepath.Dir(config.Filename), 0o750); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	// lumberjack opens lazily; verify the destination before starting HTTP.
	file, err := os.OpenFile(config.Filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, nil, err
	}
	writer := &lumberjack.Logger{
		Filename:   config.Filename,
		MaxSize:    config.MaxSize,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAge,
		Compress:   config.Compress,
	}
	options := &slog.HandlerOptions{Level: config.Level}
	outputs := make(handlers, 0, 2)
	if config.Console {
		outputs = append(outputs, slog.NewTextHandler(os.Stdout, options))
	}
	outputs = append(outputs, slog.NewJSONHandler(writer, options))
	logger := slog.New(outputs)
	return logger, writer.Close, nil
}

type handlers []slog.Handler

func (h handlers) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h handlers) Handle(ctx context.Context, record slog.Record) error {
	var err error
	for _, handler := range h {
		if handler.Enabled(ctx, record.Level) {
			err = errors.Join(err, handler.Handle(ctx, record.Clone()))
		}
	}
	return err
}

func (h handlers) WithAttrs(attrs []slog.Attr) slog.Handler {
	result := make(handlers, len(h))
	for i, handler := range h {
		result[i] = handler.WithAttrs(attrs)
	}
	return result
}

func (h handlers) WithGroup(name string) slog.Handler {
	result := make(handlers, len(h))
	for i, handler := range h {
		result[i] = handler.WithGroup(name)
	}
	return result
}
