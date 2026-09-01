// ingestion consumes the provider market-data feed and publishes
// normalized quotes into Redis.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/utkrusht/market-platform/backend/internal/config"
	"github.com/utkrusht/market-platform/backend/internal/ingest"
	"github.com/utkrusht/market-platform/backend/internal/market"
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

	// FEED selects the market data source:
	//   kite  (default) — live broker WebSocket, needs credentials
	//   yahoo           — real delayed prices, free, no credentials
	//   sim             — random-walk generator, fully offline
	feedKind := os.Getenv("FEED")
	simMode := feedKind == "sim"

	if feedKind == "kite" || feedKind == "" {
		if err := cfg.Require(map[string]string{
			"DATABASE_URL":      cfg.DatabaseURL,
			"KITE_API_KEY":      cfg.KiteAPIKey,
			"KITE_ACCESS_TOKEN": cfg.KiteAccessToken,
		}); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb, err := platform.NewRedis(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()

	var feed ingest.Feed
	var db *pgxpool.Pool
	switch feedKind {
	case "sim":
		log.Warn("running with SIMULATED market data (FEED=sim)")
		feed = ingest.NewSimFeed()
	case "yahoo":
		log.Warn("running with Yahoo Finance data (FEED=yahoo) — real but delayed, dev use only")
		feed = ingest.NewYahooFeed(log, 5*time.Second)
	default:
		feed = ingest.NewKiteFeed(cfg.KiteAPIKey, cfg.KiteAccessToken, log)
	}

	// Load the instrument universe from Supabase when a database is
	// configured (kite and yahoo need real token/symbol mappings; sim
	// falls back to its built-in universe without one).
	if cfg.DatabaseURL != "" && !simMode {
		db, err = platform.NewPostgres(ctx, cfg.DirectDatabaseURL)
		if err != nil {
			return err
		}
		defer db.Close()
		// Initial subscription = every active instrument referenced by an
		// enabled strategy or a watchlist.
		if err := subscribeActive(ctx, db, feed, log); err != nil {
			return err
		}
	}
	pub := ingest.NewPublisher(rdb, log)

	quotes := make(chan market.Quote, 4096)
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return feed.Run(ctx, quotes) })
	g.Go(func() error { return pub.Run(ctx, quotes) })

	// React to universe changes signalled by the api.
	if db != nil {
		g.Go(func() error {
			sub := rdb.Subscribe(ctx, platform.ChanResubscribe)
			defer sub.Close()
			ch := sub.Channel()
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ch:
					if err := subscribeActive(ctx, db, feed, log); err != nil {
						log.Error("resubscribe failed", "err", err)
					}
				}
			}
		})
	}

	log.Info("ingestion running")
	return g.Wait()
}

// subscribeActive loads the active instrument universe from Supabase and
// pushes it to the feed adapter.
func subscribeActive(ctx context.Context, db *pgxpool.Pool, feed ingest.Feed, log *slog.Logger) error {
	rows, err := db.Query(ctx, `
		select distinct i.id, i.exchange, i.symbol, coalesce(i.provider_token, 0)
		from instruments i
		where i.active
		  and (i.provider_token is not null or i.kind = 'INDEX')
		  and (
			exists (select 1 from strategy_instruments si
			        join strategies s on s.id = si.strategy_id and s.enabled
			        where si.instrument_id = i.id)
			or exists (select 1 from watchlist_items wi where wi.instrument_id = i.id)
		)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var universe []ingest.Instrument
	for rows.Next() {
		var in ingest.Instrument
		var token int64
		if err := rows.Scan(&in.ID, &in.Exchange, &in.Symbol, &token); err != nil {
			return err
		}
		in.Token = uint32(token)
		universe = append(universe, in)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	log.Info("instrument universe loaded", "count", len(universe))
	return feed.Subscribe(universe)
}

func newLogger(cfg *config.Config) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(cfg.LogLevel))
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})).With("service", "ingestion")
	slog.SetDefault(log)
	return log
}
