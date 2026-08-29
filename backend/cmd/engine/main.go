// engine consumes the tick stream, evaluates strategies, persists
// signals and 1-minute candles.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/utkrusht/market-platform/backend/internal/config"
	"github.com/utkrusht/market-platform/backend/internal/engine"
	"github.com/utkrusht/market-platform/backend/internal/platform"
)

func main() {
	if err := run(); err != nil && err != context.Canceled {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg)

	if err := cfg.Require(map[string]string{
		"DATABASE_URL": cfg.DatabaseURL,
	}); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb, err := platform.NewRedis(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()

	db, err := platform.NewPostgres(ctx, cfg.DirectDatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	eng := engine.New(rdb, db, log, cfg.EngineConsumerGroup, cfg.EngineConsumerName, cfg.CandleFlushInterval)
	log.Info("engine running", "group", cfg.EngineConsumerGroup, "consumer", cfg.EngineConsumerName)
	return eng.Run(ctx)
}

func newLogger(cfg *config.Config) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(cfg.LogLevel))
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})).With("service", "engine")
	slog.SetDefault(log)
	return log
}
