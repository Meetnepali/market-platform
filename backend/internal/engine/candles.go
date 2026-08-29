package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/utkrusht/market-platform/backend/internal/market"
)

// CandleAggregator folds the live tick stream into 1-minute OHLCV bars
// and batch-upserts them into Supabase once per flush interval. This is
// the only routine market-data write path into Postgres — the reason the
// database survives a high-frequency feed.
type CandleAggregator struct {
	db         *pgxpool.Pool
	log        *slog.Logger
	flushEvery time.Duration

	mu   sync.Mutex
	open map[candleKey]*market.Candle
	// symbol -> instrument_id, filled lazily from ticks the engine has
	// already resolved; unresolved symbols are skipped.
	ids map[string]int64
}

type candleKey struct {
	symbol string
	minute int64
}

func NewCandleAggregator(db *pgxpool.Pool, log *slog.Logger, flushEvery time.Duration) *CandleAggregator {
	return &CandleAggregator{
		db:         db,
		log:        log.With("component", "candles"),
		flushEvery: flushEvery,
		open:       map[candleKey]*market.Candle{},
		ids:        map[string]int64{},
	}
}

// SetInstrumentID lets the engine register symbol→id mappings it learns
// from strategy loading; candles for unmapped symbols are dropped.
func (c *CandleAggregator) SetInstrumentID(symbol string, id int64) {
	c.mu.Lock()
	c.ids[symbol] = id
	c.mu.Unlock()
}

func (c *CandleAggregator) Observe(q *market.Quote) {
	minute := q.EventTime.Truncate(time.Minute)
	key := candleKey{symbol: q.Instrument, minute: minute.Unix()}

	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.open[key]
	if !ok {
		b = &market.Candle{
			InstrumentID: c.ids[q.Instrument],
			Time:         minute.UTC(),
			Open:         q.LTP, High: q.LTP, Low: q.LTP, Close: q.LTP,
		}
		c.open[key] = b
	}
	if q.LTP > b.High {
		b.High = q.LTP
	}
	if q.LTP < b.Low {
		b.Low = q.LTP
	}
	b.Close = q.LTP
	b.Volume = q.Volume // cumulative day volume; per-bar delta derived at query time
}

func (c *CandleAggregator) Run(ctx context.Context) {
	ticker := time.NewTicker(c.flushEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.flush(context.Background()) // final flush with a fresh ctx
			return
		case <-ticker.C:
			c.flush(ctx)
		}
	}
}

func (c *CandleAggregator) flush(ctx context.Context) {
	now := time.Now().Truncate(time.Minute).Unix()

	c.mu.Lock()
	var done []*market.Candle
	for key, b := range c.open {
		// Flush bars whose minute has closed; keep the current bar open.
		if key.minute < now && b.InstrumentID != 0 {
			done = append(done, b)
			delete(c.open, key)
		}
	}
	c.mu.Unlock()

	if len(done) == 0 {
		return
	}

	batch := &pgx.Batch{}
	for _, b := range done {
		batch.Queue(`
			insert into candles_1m (instrument_id, candle_time, open, high, low, close, volume)
			values ($1, $2, $3, $4, $5, $6, $7)
			on conflict (instrument_id, candle_time) do update
			set high = greatest(candles_1m.high, excluded.high),
			    low  = least(candles_1m.low,  excluded.low),
			    close = excluded.close,
			    volume = excluded.volume`,
			b.InstrumentID, b.Time, b.Open, b.High, b.Low, b.Close, b.Volume)
	}
	if err := c.db.SendBatch(ctx, batch).Close(); err != nil {
		c.log.Error("candle flush failed", "bars", len(done), "err", err)
		return
	}
	c.log.Debug("flushed candles", "bars", len(done))
}
