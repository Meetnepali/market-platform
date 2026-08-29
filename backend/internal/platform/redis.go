// Package platform holds shared infrastructure clients (Redis, Postgres)
// and the single source of truth for Redis key names. Key layout lives
// here so no service invents its own strings.
package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis key & stream layout. Hot state only — Redis is never the
// database of record.
const (
	// StreamTicks is the durable tick stream consumed by the engine via
	// a consumer group (at-least-once, replayable).
	StreamTicks = "market:ticks"

	// ChanSignals is the pub/sub channel the api's quote gateway and
	// alerting listen on.
	ChanSignals = "signals:live"

	// ChanResubscribe tells the ingestion service that the instrument
	// universe changed (strategy enabled/disabled, watchlist edited).
	ChanResubscribe = "market:resubscribe"

	// StreamMaxLen caps market:ticks (~a few minutes of buffer at full
	// rate). XADD uses MAXLEN ~ so trimming is amortized.
	StreamMaxLen = 200_000
)

// KeyQuote is the latest-quote hash for one instrument.
func KeyQuote(exchange, instrument string) string {
	return fmt.Sprintf("market:quote:%s:%s", exchange, instrument)
}

// KeyMetrics holds rolling metrics (avg volume, prev-day high/low, MA,
// RSI state) maintained by the engine.
func KeyMetrics(instrument string) string {
	return fmt.Sprintf("market:metrics:%s", instrument)
}

// KeyCooldown is the signal-debounce key: while it exists, the same
// (strategy, instrument, signal_type) will not fire again.
func KeyCooldown(strategyID, instrument, signalType string) string {
	return fmt.Sprintf("signal:cooldown:%s:%s:%s", strategyID, instrument, signalType)
}

// KeyLastSignalPrice stores the price at which a signal last fired, so
// an unchanged price never re-fires the same signal (e.g. static quotes
// outside market hours, or a stock pinned at one level).
func KeyLastSignalPrice(strategyID, instrument, signalType string) string {
	return fmt.Sprintf("signal:lastprice:%s:%s:%s", strategyID, instrument, signalType)
}

// NewRedis connects and pings with a bounded timeout.
func NewRedis(ctx context.Context, url string) (*redis.Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	// Sane production defaults; override via URL params if needed.
	opt.DialTimeout = 5 * time.Second
	opt.ReadTimeout = 3 * time.Second
	opt.WriteTimeout = 3 * time.Second
	opt.MaxRetries = 3

	client := redis.NewClient(opt)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return client, nil
}
