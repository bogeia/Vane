package data

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/bogeia/vane/internal/config"
)

type Data struct {
	ORM *gorm.DB
}

func openORM(pool gorm.ConnPool) (*gorm.DB, error) {
	return gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
}

func Open(ctx context.Context, cfg config.Config) (*Data, func(), error) {
	orm, err := gorm.Open(postgres.Open(cfg.Database.URL), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	db, err := orm.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("get database pool: %w", err)
	}
	db.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)
	cleanup := func() { _ = db.Close() }
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("connect database: %w", err)
	}
	return &Data{ORM: orm}, cleanup, nil
}

func NewData(ctx context.Context, cfg config.Config, log *slog.Logger) (*Data, func(), error) {
	store, cleanup, err := Open(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	if err := Migrate(ctx, store); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := SyncSources(ctx, store, cfg.Sources); err != nil {
		cleanup()
		return nil, nil, err
	}
	names := make([]string, 0, len(cfg.Push.Webhooks))
	for _, hook := range cfg.Push.Webhooks {
		names = append(names, hook.Name)
	}
	if err := NewRealtimeRepo(store).SyncTargets(ctx, names, time.Now()); err != nil {
		cleanup()
		return nil, nil, err
	}
	log.Info("database initialized", "sources", len(cfg.Sources))
	return store, cleanup, nil
}
