package engine

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/utkrusht/market-platform/backend/internal/market"
	"github.com/utkrusht/market-platform/backend/internal/platform"
)

// Strategy is an enabled strategy loaded from Supabase with its parsed
// rule and instrument set.
type Strategy struct {
	ID          string
	UserID      string
	Rule        *Rule
	Instruments map[string]int64 // symbol -> instrument_id
}

// Engine consumes market:ticks via a Redis consumer group, maintains
// rolling metrics, evaluates enabled strategies, and emits signals.
type Engine struct {
	rdb   *redis.Client
	db    *pgxpool.Pool
	log   *slog.Logger
	group string
	name  string

	mu         sync.RWMutex
	strategies []*Strategy               // refreshed periodically
	state      map[string]*rollingState  // per instrument symbol

	candles *CandleAggregator
}

func New(rdb *redis.Client, db *pgxpool.Pool, log *slog.Logger, group, name string, flushEvery time.Duration) *Engine {
	return &Engine{
		rdb:     rdb,
		db:      db,
		log:     log.With("component", "engine"),
		group:   group,
		name:    name,
		state:   map[string]*rollingState{},
		candles: NewCandleAggregator(db, log, flushEvery),
	}
}

func (e *Engine) Run(ctx context.Context) error {
	// Create the consumer group idempotently ("$" = only new messages;
	// use "0" to replay history in tests).
	err := e.rdb.XGroupCreateMkStream(ctx, platform.StreamTicks, e.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}

	go e.refreshStrategiesLoop(ctx)
	go e.candles.Run(ctx)

	for {
		res, err := e.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    e.group,
			Consumer: e.name,
			Streams:  []string{platform.StreamTicks, ">"},
			Count:    256,
			Block:    2 * time.Second,
		}).Result()
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, redis.Nil) {
				continue // no new messages within Block
			}
			e.log.Error("xreadgroup failed", "err", err)
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range res {
			ids := make([]string, 0, len(stream.Messages))
			for _, msg := range stream.Messages {
				e.handleMessage(ctx, msg)
				ids = append(ids, msg.ID)
			}
			if len(ids) > 0 {
				if err := e.rdb.XAck(ctx, platform.StreamTicks, e.group, ids...).Err(); err != nil {
					e.log.Error("xack failed", "err", err)
				}
			}
		}
	}
}

func (e *Engine) handleMessage(ctx context.Context, msg redis.XMessage) {
	raw, ok := msg.Values["q"].(string)
	if !ok {
		return
	}
	var q market.Quote
	if err := q.UnmarshalBinary([]byte(raw)); err != nil {
		e.log.Warn("bad tick payload", "err", err)
		return
	}

	// 1. Rolling metrics + candle aggregation.
	st, ok := e.state[q.Instrument]
	if !ok {
		st = &rollingState{}
		e.state[q.Instrument] = st
	}
	metrics := st.update(&q)
	e.candles.Observe(&q)

	// 2. Evaluate every enabled strategy watching this instrument.
	e.mu.RLock()
	strategies := e.strategies
	e.mu.RUnlock()

	for _, s := range strategies {
		instrumentID, watches := s.Instruments[q.Instrument]
		if !watches || !s.Rule.Eval(metrics) {
			continue
		}
		e.emitSignal(ctx, s, instrumentID, &q, metrics)
	}
}

func (e *Engine) emitSignal(ctx context.Context, s *Strategy, instrumentID int64, q *market.Quote, metrics map[string]float64) {
	// Dedup: if the price hasn't moved since this signal last fired,
	// re-firing adds no information — skip regardless of cooldown.
	// (Catches static quotes outside market hours and pinned prices.)
	lastKey := platform.KeyLastSignalPrice(s.ID, q.Instrument, s.Rule.SignalType)
	if last, err := e.rdb.Get(ctx, lastKey).Float64(); err == nil && last == q.LTP {
		return
	}

	// Debounce: SET NX with the rule's cooldown. If the key exists we
	// already fired recently; skip. This also makes at-least-once stream
	// delivery idempotent for signals.
	cd := platform.KeyCooldown(s.ID, q.Instrument, s.Rule.SignalType)
	ok, err := e.rdb.SetNX(ctx, cd, 1, time.Duration(s.Rule.CooldownSeconds)*time.Second).Result()
	if err != nil {
		e.log.Error("cooldown check failed", "err", err)
		return
	}
	if !ok {
		return
	}

	sig := market.Signal{
		StrategyID:   s.ID,
		InstrumentID: instrumentID,
		Instrument:   q.Instrument,
		SignalType:   s.Rule.SignalType,
		Price:        q.LTP,
		Metrics:      toAny(metrics),
		CreatedAt:    time.Now(),
	}

	// Persist to Supabase — this insert is what Supabase Realtime fans
	// out to the owning user's browser.
	metricsJSON, _ := json.Marshal(sig.Metrics)
	_, err = e.db.Exec(ctx, `
		insert into signals (strategy_id, instrument_id, signal_type, price, metrics_json)
		values ($1, $2, $3, $4, $5)`,
		sig.StrategyID, sig.InstrumentID, sig.SignalType, sig.Price, metricsJSON)
	if err != nil {
		e.log.Error("signal insert failed", "strategy", s.ID, "err", err)
		// Release the cooldown so a transient DB failure can retry on the
		// next matching tick.
		e.rdb.Del(ctx, cd)
		return
	}

	// Remember the fired price for the dedup check (self-expires so a
	// stock revisiting the same level days later can still fire).
	e.rdb.Set(ctx, lastKey, q.LTP, 24*time.Hour)

	// Also publish for low-latency consumers (api quote gateway, alerts).
	if payload, err := json.Marshal(sig); err == nil {
		e.rdb.Publish(ctx, platform.ChanSignals, payload)
	}
	e.log.Info("signal", "type", sig.SignalType, "instrument", q.Instrument, "price", q.LTP, "strategy", s.ID)
}

// refreshStrategiesLoop reloads enabled strategies every 15s. Simple and
// robust; move to LISTEN/NOTIFY or a Redis channel if reload latency
// ever matters.
func (e *Engine) refreshStrategiesLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := e.loadStrategies(ctx); err != nil {
			e.log.Error("strategy reload failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *Engine) loadStrategies(ctx context.Context) error {
	rows, err := e.db.Query(ctx, `
		select s.id, s.user_id, s.configuration_json,
		       coalesce(json_agg(json_build_object('symbol', i.symbol, 'id', i.id))
		                filter (where i.id is not null), '[]')
		from strategies s
		left join strategy_instruments si on si.strategy_id = s.id
		left join instruments i on i.id = si.instrument_id and i.active
		where s.enabled
		group by s.id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var loaded []*Strategy
	for rows.Next() {
		var id, userID string
		var cfg, instJSON []byte
		if err := rows.Scan(&id, &userID, &cfg, &instJSON); err != nil {
			return err
		}
		rule, err := ParseRule(cfg)
		if err != nil {
			e.log.Warn("skipping strategy with invalid rule", "strategy", id, "err", err)
			continue
		}
		var insts []struct {
			Symbol string `json:"symbol"`
			ID     int64  `json:"id"`
		}
		if err := json.Unmarshal(instJSON, &insts); err != nil {
			continue
		}
		s := &Strategy{ID: id, UserID: userID, Rule: rule, Instruments: map[string]int64{}}
		for _, in := range insts {
			s.Instruments[in.Symbol] = in.ID
			// Candles need the symbol→id mapping to persist bars.
			e.candles.SetInstrumentID(in.Symbol, in.ID)
		}
		loaded = append(loaded, s)
	}

	e.mu.Lock()
	e.strategies = loaded
	e.mu.Unlock()
	return rows.Err()
}

func toAny(m map[string]float64) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
