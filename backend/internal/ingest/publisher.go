package ingest

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/utkrusht/market-platform/backend/internal/market"
	"github.com/utkrusht/market-platform/backend/internal/platform"
)

// Publisher drains normalized quotes into Redis:
//   - HSET market:quote:{exchange}:{symbol}  → latest state for the API
//   - XADD market:ticks (MAXLEN ~)           → durable stream for the engine
//
// Writes are pipelined per tick (2 commands, 1 round trip).
type Publisher struct {
	rdb *redis.Client
	log *slog.Logger
}

func NewPublisher(rdb *redis.Client, log *slog.Logger) *Publisher {
	return &Publisher{rdb: rdb, log: log.With("component", "publisher")}
}

func (p *Publisher) Run(ctx context.Context, in <-chan market.Quote) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case q := <-in:
			if err := p.publish(ctx, &q); err != nil {
				p.log.Error("publish failed", "instrument", q.Instrument, "err", err)
			}
		}
	}
}

func (p *Publisher) publish(ctx context.Context, q *market.Quote) error {
	payload, err := q.MarshalBinary()
	if err != nil {
		return err
	}
	pipe := p.rdb.Pipeline()
	key := platform.KeyQuote(string(q.Exchange), q.Instrument)
	pipe.Set(ctx, key, payload, 24*time.Hour) // latest quote, self-expiring
	pipe.XAdd(ctx, &redis.XAddArgs{
		Stream: platform.StreamTicks,
		MaxLen: platform.StreamMaxLen,
		Approx: true,
		Values: map[string]any{"q": payload},
	})
	_, err = pipe.Exec(ctx)
	return err
}
