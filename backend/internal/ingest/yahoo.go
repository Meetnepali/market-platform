package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	neturl "net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/utkrusht/market-platform/backend/internal/market"
)

// YahooFeed polls Yahoo Finance's public chart endpoint for real NSE
// prices — free and unauthenticated, but unofficial and delayed
// (~15 min during market hours; last close when the market is shut).
//
// This is a REAL-data development feed: use it to exercise the pipeline
// with genuine prices before paying for Kite Connect. It is not
// licensed for production redistribution — same release-gate rules as
// any other market data source. Enable with FEED=yahoo.
type YahooFeed struct {
	log      *slog.Logger
	client   *http.Client
	interval time.Duration

	mu          sync.RWMutex
	instruments []Instrument
}

func NewYahooFeed(log *slog.Logger, pollInterval time.Duration) *YahooFeed {
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	return &YahooFeed{
		log:      log.With("component", "yahoo_feed"),
		client:   &http.Client{Timeout: 10 * time.Second},
		interval: pollInterval,
	}
}

func (y *YahooFeed) Subscribe(instruments []Instrument) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	y.instruments = instruments
	return nil
}

// yahooWorkers bounds concurrent requests. Yahoo rate-limits aggressive
// clients, so a large universe sweeps in ~1-2 minutes rather than
// hammering the endpoint. For genuinely live wide coverage, use Kite.
const yahooWorkers = 4

func (y *YahooFeed) Run(ctx context.Context, out chan<- market.Quote) error {
	y.mu.Lock()
	if len(y.instruments) == 0 {
		y.instruments = DefaultSimUniverse // sensible default universe
	}
	y.mu.Unlock()

	// Sweep continuously: finish one pass over the universe, wait the
	// interval, sweep again.
	for {
		start := time.Now()
		n := y.pollAll(ctx, out)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		y.log.Info("sweep complete", "quotes", n, "took", time.Since(start).Round(time.Second))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(y.interval):
		}
	}
}

func (y *YahooFeed) pollAll(ctx context.Context, out chan<- market.Quote) int {
	y.mu.RLock()
	instruments := y.instruments
	y.mu.RUnlock()

	jobs := make(chan Instrument)
	var wg sync.WaitGroup
	var fetched atomic.Int64

	for range yahooWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for in := range jobs {
				q, err := y.fetch(ctx, in)
				if err != nil {
					if ctx.Err() == nil {
						y.log.Debug("fetch failed", "symbol", in.Symbol, "err", err)
					}
					continue
				}
				if err := q.Validate(); err != nil {
					continue
				}
				select {
				case out <- *q:
					fetched.Add(1)
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	for _, in := range instruments {
		if ctx.Err() != nil {
			break
		}
		jobs <- in
	}
	close(jobs)
	wg.Wait()
	return int(fetched.Load())
}

// chartResponse is the subset of Yahoo's v8 chart payload we need.
type chartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				RegularMarketPrice  float64 `json:"regularMarketPrice"`
				ChartPreviousClose  float64 `json:"chartPreviousClose"`
				RegularMarketVolume int64   `json:"regularMarketVolume"`
				RegularMarketDayHigh float64 `json:"regularMarketDayHigh"`
				RegularMarketDayLow  float64 `json:"regularMarketDayLow"`
				RegularMarketTime   int64   `json:"regularMarketTime"`
			} `json:"meta"`
			Indicators struct {
				Quote []struct {
					Open  []float64 `json:"open"`
					Close []float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// IndexYahooSymbols maps our index instrument symbols to Yahoo tickers.
// Shared with the api's history proxy.
var IndexYahooSymbols = map[string]string{
	"NIFTY 50":          "^NSEI",
	"NIFTY BANK":        "^NSEBANK",
	"NIFTY IT":          "^CNXIT",
	"NIFTY FIN SERVICE": "NIFTY_FIN_SERVICE.NS",
	"SENSEX":            "^BSESN",
}

// YahooSymbol resolves an instrument to its Yahoo ticker.
func YahooSymbol(exchange market.Exchange, symbol string) string {
	if y, ok := IndexYahooSymbols[symbol]; ok {
		return y
	}
	if exchange == market.BSE {
		return symbol + ".BO"
	}
	return symbol + ".NS"
}

func (y *YahooFeed) fetch(ctx context.Context, in Instrument) (*market.Quote, error) {
	// 5 daily bars in one request: today's open plus the close ~5
	// trading days back for weekly-change metrics.
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&range=5d",
		neturl.PathEscape(YahooSymbol(in.Exchange, in.Symbol)))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (market-platform dev feed)")

	resp, err := y.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo returned %s", resp.Status)
	}

	var cr chartResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, err
	}
	if cr.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo error: %s", cr.Chart.Error.Description)
	}
	if len(cr.Chart.Result) == 0 {
		return nil, fmt.Errorf("empty result")
	}
	meta := cr.Chart.Result[0].Meta

	open, weekAgoClose := 0.0, 0.0
	if qs := cr.Chart.Result[0].Indicators.Quote; len(qs) > 0 {
		// Today's open = last bar's open; week-ago close = first bar's.
		for i := len(qs[0].Open) - 1; i >= 0; i-- {
			if qs[0].Open[i] > 0 {
				open = qs[0].Open[i]
				break
			}
		}
		for _, c := range qs[0].Close {
			if c > 0 {
				weekAgoClose = c
				break
			}
		}
	}

	now := time.Now()
	return &market.Quote{
		Instrument:      in.Symbol,
		Exchange:        in.Exchange,
		InstrumentToken: in.Token,
		LTP:             meta.RegularMarketPrice,
		Open:            open,
		High:            meta.RegularMarketDayHigh,
		Low:             meta.RegularMarketDayLow,
		PrevClose:       meta.ChartPreviousClose,
		Volume:          meta.RegularMarketVolume,
		WeekAgoClose:    weekAgoClose,
		EventTime:       time.Unix(meta.RegularMarketTime, 0),
		IngestTime:      now,
	}, nil
}
