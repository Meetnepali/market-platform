// Package ingest consumes a provider market-data stream, normalizes it
// to market.Quote, and publishes into Redis (latest-state hash + durable
// tick stream). The provider sits behind the Feed interface so swapping
// Kite for a licensed NSE/BSE vendor feed (Phase 6) touches one file.
package ingest

import (
	"context"

	"github.com/utkrusht/market-platform/backend/internal/market"
)

// Instrument maps our instrument master row to a provider token.
type Instrument struct {
	ID       int64
	Exchange market.Exchange
	Symbol   string
	Token    uint32
}

// Feed is a provider-agnostic live market data source. Implementations
// must be safe to Run once; reconnection is the implementation's job.
type Feed interface {
	// Run connects and streams normalized quotes onto out until ctx is
	// cancelled. It must handle reconnects internally (with backoff) and
	// only return a non-nil error for unrecoverable conditions (e.g. bad
	// credentials).
	Run(ctx context.Context, out chan<- market.Quote) error

	// Subscribe replaces the active instrument set. Safe to call while
	// Run is active (used when strategies/watchlists change).
	Subscribe(instruments []Instrument) error
}
