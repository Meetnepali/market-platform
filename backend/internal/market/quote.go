// Package market defines the canonical, provider-agnostic market data
// model. Every provider adapter (Kite today, a licensed NSE/BSE vendor
// feed later) normalizes into these types; nothing downstream ever sees
// a provider-specific payload.
package market

import (
	"encoding/json"
	"fmt"
	"time"
)

type Exchange string

const (
	NSE Exchange = "NSE"
	BSE Exchange = "BSE"
)

// Quote is the normalized tick/quote event flowing through the system.
// Prices are paise-precision floats; monetary math that must be exact
// (P&L, billing) belongs in the database as numeric, not here.
type Quote struct {
	Instrument      string    `json:"instrument"`       // e.g. "RELIANCE"
	Exchange        Exchange  `json:"exchange"`         // NSE | BSE
	InstrumentToken uint32    `json:"instrument_token"` // provider token
	LTP             float64   `json:"ltp"`
	Open            float64   `json:"open"`
	High            float64   `json:"high"`
	Low             float64   `json:"low"`
	PrevClose       float64   `json:"previous_close"`
	Volume          int64     `json:"volume"`
	Bid             float64   `json:"bid,omitempty"`
	Ask             float64   `json:"ask,omitempty"`
	WeekAgoClose    float64   `json:"week_ago_close,omitempty"` // close ~5 trading days back (0 = unknown)
	EventTime       time.Time `json:"event_time"`  // provider timestamp (IST)
	IngestTime      time.Time `json:"ingest_time"` // server-stamped on receipt
}

// Validate rejects malformed ticks before they reach Redis. A bad tick
// is dropped and counted, never propagated.
func (q *Quote) Validate() error {
	switch {
	case q.Instrument == "":
		return fmt.Errorf("empty instrument")
	case q.Exchange != NSE && q.Exchange != BSE:
		return fmt.Errorf("unknown exchange %q", q.Exchange)
	case q.LTP <= 0:
		return fmt.Errorf("non-positive ltp %f", q.LTP)
	case q.Volume < 0:
		return fmt.Errorf("negative volume %d", q.Volume)
	case q.EventTime.IsZero():
		return fmt.Errorf("zero event time")
	}
	return nil
}

// ChangePercent is the day change vs previous close, the most common
// rule-engine input. Returns 0 when PrevClose is unknown.
func (q *Quote) ChangePercent() float64 {
	if q.PrevClose <= 0 {
		return 0
	}
	return (q.LTP - q.PrevClose) / q.PrevClose * 100
}

// WeekChangePercent is the move vs the close ~5 trading days ago.
// Returns 0 when weekly history is unavailable (e.g. Kite ticks, which
// carry no weekly context — the engine falls back to candle history).
func (q *Quote) WeekChangePercent() float64 {
	if q.WeekAgoClose <= 0 {
		return 0
	}
	return (q.LTP - q.WeekAgoClose) / q.WeekAgoClose * 100
}

func (q *Quote) MarshalBinary() ([]byte, error)  { return json.Marshal(q) }
func (q *Quote) UnmarshalBinary(b []byte) error  { return json.Unmarshal(b, q) }

// Signal is a rule-engine match: the only market-derived event that is
// persisted to Supabase and pushed to the UI.
type Signal struct {
	StrategyID   string         `json:"strategy_id"`
	InstrumentID int64          `json:"instrument_id"`
	Instrument   string         `json:"instrument"`
	SignalType   string         `json:"signal_type"`
	Price        float64        `json:"price"`
	Metrics      map[string]any `json:"metrics"`
	CreatedAt    time.Time      `json:"created_at"`
}

// Candle is a 1-minute OHLCV aggregate, the only routine market-data
// write path into Postgres.
type Candle struct {
	InstrumentID int64     `json:"instrument_id"`
	Time         time.Time `json:"candle_time"` // minute-truncated, UTC
	Open         float64   `json:"open"`
	High         float64   `json:"high"`
	Low          float64   `json:"low"`
	Close        float64   `json:"close"`
	Volume       int64     `json:"volume"`
}
