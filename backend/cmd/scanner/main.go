// scanner runs the EOD pullback-in-uptrend buy scan once and exits.
// Schedule it after market close (e.g. cron at 18:30 IST on weekdays):
//
//	go run ./cmd/scanner
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/utkrusht/market-platform/backend/internal/config"
	"github.com/utkrusht/market-platform/backend/internal/platform"
	"github.com/utkrusht/market-platform/backend/internal/scanner"
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

	db, err := platform.NewPostgres(ctx, cfg.DirectDatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	sc := scanner.New(
		db,
		scanner.NewYahooHistory(),
		log,
		scanner.DefaultConfig(),
		envInt("SCANNER_TOP_N", 10),
		envInt("SCANNER_WORKERS", 4),
	)
	log.Info("scan starting")
	return sc.Run(ctx)
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func newLogger(cfg *config.Config) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(cfg.LogLevel))
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})).With("service", "scanner")
	slog.SetDefault(log)
	return log
}
