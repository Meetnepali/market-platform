package ingest

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/utkrusht/market-platform/backend/internal/market"
)

// SimFeed generates realistic random-walk ticks for development and
// testing — same Feed interface as the Kite adapter, so the rest of the
// pipeline is exercised unchanged. Enable with FEED=sim.
type SimFeed struct {
	mu          sync.RWMutex
	instruments []Instrument
	state       map[string]*simState
}

type simState struct {
	prevClose float64
	open      float64
	high      float64
	low       float64
	last      float64
	volume    int64
}

// Default sim universe used when no instruments are subscribed (e.g.
// running without a database).
var DefaultSimUniverse = []Instrument{
	{ID: 1, Exchange: market.NSE, Symbol: "RELIANCE", Token: 738561},
	{ID: 2, Exchange: market.NSE, Symbol: "TCS", Token: 2953217},
	{ID: 3, Exchange: market.NSE, Symbol: "HDFCBANK", Token: 341249},
	{ID: 4, Exchange: market.NSE, Symbol: "INFY", Token: 408065},
	{ID: 5, Exchange: market.NSE, Symbol: "ICICIBANK", Token: 1270529},
}

var simBasePrices = map[string]float64{
	"RELIANCE": 1480, "TCS": 4150, "HDFCBANK": 1720, "INFY": 1890, "ICICIBANK": 1240,
}

func NewSimFeed() *SimFeed {
	return &SimFeed{state: map[string]*simState{}}
}

func (s *SimFeed) Subscribe(instruments []Instrument) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instruments = instruments
	for _, in := range instruments {
		if _, ok := s.state[in.Symbol]; !ok {
			base := simBasePrices[in.Symbol]
			if base == 0 {
				base = 500 + rand.Float64()*2000
			}
			// Start the day slightly gapped from the previous close.
			open := base * (1 + (rand.Float64()-0.5)*0.01)
			s.state[in.Symbol] = &simState{
				prevClose: base, open: open, high: open, low: open, last: open,
			}
		}
	}
	return nil
}

func (s *SimFeed) Run(ctx context.Context, out chan<- market.Quote) error {
	s.mu.Lock()
	if len(s.instruments) == 0 {
		s.mu.Unlock()
		_ = s.Subscribe(DefaultSimUniverse)
		s.mu.Lock()
	}
	s.mu.Unlock()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.mu.RLock()
			instruments := s.instruments
			s.mu.RUnlock()
			for _, in := range instruments {
				q := s.tick(in)
				select {
				case out <- q:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}

func (s *SimFeed) tick(in Instrument) market.Quote {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state[in.Symbol]

	// Geometric random walk, ~0.05% per tick, with occasional bursts so
	// volume-spike rules have something to fire on.
	drift := (rand.Float64() - 0.5) * 0.001
	if rand.Intn(200) == 0 {
		drift *= 15 // burst
	}
	st.last = math.Round(st.last*(1+drift)*20) / 20 // 5-paise tick size
	st.high = math.Max(st.high, st.last)
	st.low = math.Min(st.low, st.last)
	st.volume += int64(1000 + rand.Intn(50000))

	now := time.Now()
	return market.Quote{
		Instrument:      in.Symbol,
		Exchange:        in.Exchange,
		InstrumentToken: in.Token,
		LTP:             st.last,
		Open:            st.open,
		High:            st.high,
		Low:             st.low,
		PrevClose:       st.prevClose,
		Volume:          st.volume,
		Bid:             st.last - 0.05,
		Ask:             st.last + 0.05,
		EventTime:       now,
		IngestTime:      now,
	}
}
