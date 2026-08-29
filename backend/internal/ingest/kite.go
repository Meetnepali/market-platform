package ingest

import (
	"context"
	"log/slog"
	"sync"
	"time"

	kitemodels "github.com/zerodha/gokiteconnect/v4/models"
	kiteticker "github.com/zerodha/gokiteconnect/v4/ticker"

	"github.com/utkrusht/market-platform/backend/internal/market"
)

// KiteFeed streams live ticks from Zerodha Kite Connect. It normalizes
// kite ticks into market.Quote and server-stamps IngestTime on receipt.
//
// gokiteconnect's ticker already auto-reconnects with backoff; we layer
// observability (connect/close/error logs, stale detection hooks) and
// token→instrument mapping on top.
type KiteFeed struct {
	apiKey      string
	accessToken string
	log         *slog.Logger

	mu      sync.RWMutex
	byToken map[uint32]Instrument
	ticker  *kiteticker.Ticker

	lastTick time.Time // updated on every tick; used for staleness checks
}

func NewKiteFeed(apiKey, accessToken string, log *slog.Logger) *KiteFeed {
	return &KiteFeed{
		apiKey:      apiKey,
		accessToken: accessToken,
		log:         log.With("component", "kite_feed"),
		byToken:     map[uint32]Instrument{},
	}
}

func (k *KiteFeed) Run(ctx context.Context, out chan<- market.Quote) error {
	t := kiteticker.New(k.apiKey, k.accessToken)
	t.SetAutoReconnect(true)
	t.SetReconnectMaxRetries(300) // effectively "keep trying all session"
	if err := t.SetReconnectMaxDelay(60 * time.Second); err != nil {
		k.log.Warn("set reconnect delay", "err", err)
	}

	k.mu.Lock()
	k.ticker = t
	k.mu.Unlock()

	t.OnConnect(func() {
		k.log.Info("connected")
		k.resubscribeLocked()
	})
	t.OnReconnect(func(attempt int, delay time.Duration) {
		k.log.Warn("reconnecting", "attempt", attempt, "delay", delay)
	})
	t.OnError(func(err error) {
		k.log.Error("ticker error", "err", err)
	})
	t.OnClose(func(code int, reason string) {
		k.log.Warn("connection closed", "code", code, "reason", reason)
	})
	t.OnNoReconnect(func(attempt int) {
		k.log.Error("gave up reconnecting", "attempts", attempt)
	})

	t.OnTick(func(tick kitemodels.Tick) {
		k.mu.RLock()
		inst, ok := k.byToken[tick.InstrumentToken]
		k.mu.RUnlock()
		if !ok {
			return // tick for an instrument we no longer track
		}

		q := market.Quote{
			Instrument:      inst.Symbol,
			Exchange:        inst.Exchange,
			InstrumentToken: tick.InstrumentToken,
			LTP:             tick.LastPrice,
			Open:            tick.OHLC.Open,
			High:            tick.OHLC.High,
			Low:             tick.OHLC.Low,
			PrevClose:       tick.OHLC.Close,
			Volume:          int64(tick.VolumeTraded),
			EventTime:       tick.Timestamp.Time,
			IngestTime:      time.Now(),
		}
		if len(tick.Depth.Buy) > 0 {
			q.Bid = tick.Depth.Buy[0].Price
		}
		if len(tick.Depth.Sell) > 0 {
			q.Ask = tick.Depth.Sell[0].Price
		}
		if err := q.Validate(); err != nil {
			k.log.Warn("dropping invalid tick", "instrument", inst.Symbol, "err", err)
			return
		}
		k.lastTick = time.Now()

		select {
		case out <- q:
		case <-ctx.Done():
		default:
			// Publisher is saturated: drop the oldest semantics are handled
			// by the stream MAXLEN; here we drop this tick rather than block
			// the socket reader. Latest-state is repaired by the next tick.
			k.log.Warn("publish channel full, dropping tick", "instrument", inst.Symbol)
		}
	})

	// ServeWithContext blocks until ctx is cancelled.
	t.ServeWithContext(ctx)
	return ctx.Err()
}

// Subscribe replaces the active instrument set (called at boot and on
// market:resubscribe notifications).
func (k *KiteFeed) Subscribe(instruments []Instrument) error {
	k.mu.Lock()
	k.byToken = make(map[uint32]Instrument, len(instruments))
	for _, in := range instruments {
		k.byToken[in.Token] = in
	}
	k.mu.Unlock()

	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.ticker != nil {
		k.resubscribeLocked()
	}
	return nil
}

func (k *KiteFeed) resubscribeLocked() {
	tokens := make([]uint32, 0, len(k.byToken))
	for tok := range k.byToken {
		tokens = append(tokens, tok)
	}
	if len(tokens) == 0 {
		return
	}
	if err := k.ticker.Subscribe(tokens); err != nil {
		k.log.Error("subscribe failed", "err", err)
		return
	}
	// Full mode gives OHLC + depth, which the rule engine needs.
	if err := k.ticker.SetMode(kiteticker.ModeFull, tokens); err != nil {
		k.log.Error("set mode failed", "err", err)
	}
	k.log.Info("subscribed", "instruments", len(tokens))
}
