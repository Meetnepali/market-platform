// api is the HTTP service: REST endpoints + SSE live-quote stream.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/utkrusht/market-platform/backend/internal/api"
	"github.com/utkrusht/market-platform/backend/internal/config"
	"github.com/utkrusht/market-platform/backend/internal/platform"
)

func main() {
	if err := run(); err != nil {
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
		"SUPABASE_URL": cfg.SupabaseURL,
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

	db, err := platform.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	auth, err := api.NewAuthenticator(ctx, cfg.SupabaseJWKSURL, cfg.SupabaseJWTSecret, log)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: api.NewServer(rdb, db, auth, log).Router(cfg.CORSOrigin),
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func newLogger(cfg *config.Config) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(cfg.LogLevel))
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	log := slog.New(h).With("service", "api")
	slog.SetDefault(log)
	return log
}
