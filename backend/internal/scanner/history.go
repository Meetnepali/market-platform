package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"sync"
	"time"

	"github.com/utkrusht/market-platform/backend/internal/ingest"
	"github.com/utkrusht/market-platform/backend/internal/market"
)

// Bar is one daily OHLCV candle.
type Bar struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume int64
}

// History is a stock's daily bar series, oldest first.
type History struct {
	Symbol string
	Bars   []Bar
}

// Closes returns the close series, oldest first.
func (h *History) Closes() []float64 {
	out := make([]float64, len(h.Bars))
	for i, b := range h.Bars {
		out[i] = b.Close
	}
	return out
}

// Volumes returns the volume series, oldest first.
func (h *History) Volumes() []int64 {
	out := make([]int64, len(h.Bars))
	for i, b := range h.Bars {
		out[i] = b.Volume
	}
	return out
}

// HistoryFetcher pulls daily bars. Interface so the Yahoo implementation
// can be swapped for Kite historical data (or a test fixture) without
// touching scoring.
type HistoryFetcher interface {
	Daily(ctx context.Context, symbol string) (*History, error)
}

// yahooHistory fetches ~1 year of daily bars from Yahoo's chart API.
// Same source and caveats as ingest.YahooFeed: free, unofficial,
// delayed — fine for an EOD scanner.
type yahooHistory struct {
	client *http.Client
}

func NewYahooHistory() HistoryFetcher {
	return &yahooHistory{client: &http.Client{Timeout: 15 * time.Second}}
}

// NiftyIndexSymbol is the Yahoo ticker used as the relative-strength
// benchmark for every scanned stock.
const NiftyIndexSymbol = "^NSEI"

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
					Volume []int64   `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

func (y *yahooHistory) Daily(ctx context.Context, symbol string) (*History, error) {
	ticker := symbol
	if symbol != NiftyIndexSymbol {
		ticker = ingest.YahooSymbol(market.NSE, symbol)
	}
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&range=1y",
		neturl.PathEscape(ticker))

	var cr yahooChartResponse
	if err := y.getJSON(ctx, url, &cr); err != nil {
		return nil, err
	}
	if cr.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo error: %s", cr.Chart.Error.Description)
	}
	if len(cr.Chart.Result) == 0 || len(cr.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, fmt.Errorf("empty result")
	}

	r := cr.Chart.Result[0]
	q := r.Indicators.Quote[0]
	h := &History{Symbol: symbol, Bars: make([]Bar, 0, len(r.Timestamp))}
	for i, ts := range r.Timestamp {
		if i >= len(q.Close) || q.Close[i] <= 0 {
			continue // Yahoo pads holidays/suspensions with nulls
		}
		h.Bars = append(h.Bars, Bar{
			Time:   time.Unix(ts, 0),
			Open:   at(q.Open, i),
			High:   at(q.High, i),
			Low:    at(q.Low, i),
			Close:  q.Close[i],
			Volume: atI(q.Volume, i),
		})
	}
	if len(h.Bars) == 0 {
		return nil, fmt.Errorf("no usable bars")
	}
	return h, nil
}

// getJSON fetches with retry — Yahoo intermittently 429s bursty
// clients, and a full-universe sweep must survive that.
func (y *yahooHistory) getJSON(ctx context.Context, url string, out any) error {
	backoff := []time.Duration{0, 3 * time.Second, 8 * time.Second}
	var lastErr error
	for _, wait := range backoff {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

		resp, err := y.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("yahoo returned %s", resp.Status)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("yahoo returned %s", resp.Status)
		}
		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		return err
	}
	return lastErr
}

func at(s []float64, i int) float64 {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func atI(s []int64, i int) int64 {
	if i < len(s) {
		return s[i]
	}
	return 0
}

// fetchAll pulls histories for every symbol with a bounded worker pool
// (Yahoo rate-limits aggressive clients — same budget as ingest).
func fetchAll(ctx context.Context, f HistoryFetcher, symbols []string, workers int) map[string]*History {
	if workers <= 0 {
		workers = 4
	}
	jobs := make(chan string)
	var mu sync.Mutex
	out := make(map[string]*History, len(symbols))
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range jobs {
				h, err := f.Daily(ctx, sym)
				if err != nil {
					continue // logged as a count by the caller
				}
				mu.Lock()
				out[sym] = h
				mu.Unlock()
			}
		}()
	}
	for _, sym := range symbols {
		if ctx.Err() != nil {
			break
		}
		jobs <- sym
	}
	close(jobs)
	wg.Wait()
	return out
}
