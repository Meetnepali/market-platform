package api

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Stock detail endpoint: merges live quote (Redis) with fundamentals
// (Yahoo quoteSummary, crumb-authed) and computed risk metrics
// (alpha/beta vs NIFTY from 1y daily closes). Fundamentals change
// slowly, so results are cached in Redis for 24h — one upstream fetch
// per stock per day regardless of how many users open the panel.

const detailCacheTTL = 24 * time.Hour

type StockDetails struct {
	Symbol   string  `json:"symbol"`
	Name     string  `json:"name,omitempty"`
	Exchange string  `json:"exchange"`

	// Performance
	FiftyTwoWeekHigh float64 `json:"fifty_two_week_high,omitempty"`
	FiftyTwoWeekLow  float64 `json:"fifty_two_week_low,omitempty"`

	// Fundamentals (0 = unavailable)
	MarketCap     float64 `json:"market_cap,omitempty"`
	TrailingPE    float64 `json:"trailing_pe,omitempty"`
	PriceToBook   float64 `json:"price_to_book,omitempty"`
	TrailingEPS   float64 `json:"trailing_eps,omitempty"`
	DividendYield float64 `json:"dividend_yield,omitempty"` // fraction, 0.0047 = 0.47%
	BookValue     float64 `json:"book_value,omitempty"`
	DebtToEquity  float64 `json:"debt_to_equity,omitempty"` // percent, 36.6 = 0.37x
	ROE           float64 `json:"roe,omitempty"`            // fraction

	// Risk (computed vs NIFTY 50, 1y daily returns)
	Beta        float64 `json:"beta,omitempty"`
	AlphaAnnual float64 `json:"alpha_annual,omitempty"` // percent/yr, CAPM rf=7%
	VolAnnual   float64 `json:"vol_annual,omitempty"`   // percent/yr
	YearReturn  float64 `json:"year_return,omitempty"`  // percent
	FetchedAt   string  `json:"fetched_at"`
}

// yahooClient handles the cookie+crumb dance quoteSummary requires.
type yahooClient struct {
	mu     sync.Mutex
	http   *http.Client
	crumb  string
	expiry time.Time
}

const yahooUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)"

func newYahooClient() *yahooClient {
	jar, _ := cookiejar.New(nil)
	return &yahooClient{http: &http.Client{Timeout: 15 * time.Second, Jar: jar}}
}

func (y *yahooClient) get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", yahooUA)
	resp, err := y.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (y *yahooClient) getCrumb() (string, error) {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.crumb != "" && time.Now().Before(y.expiry) {
		return y.crumb, nil
	}
	// Seed session cookies, then fetch the crumb tied to them.
	if _, err := y.get("https://fc.yahoo.com"); err != nil {
		// fc.yahoo.com returns 404 but still sets cookies; ignore status errors.
		_ = err
	}
	b, err := y.get("https://query1.finance.yahoo.com/v1/test/getcrumb")
	if err != nil {
		return "", fmt.Errorf("get crumb: %w", err)
	}
	crumb := strings.TrimSpace(string(b))
	if crumb == "" || strings.Contains(crumb, "<") {
		return "", fmt.Errorf("invalid crumb response")
	}
	y.crumb, y.expiry = crumb, time.Now().Add(30*time.Minute)
	return crumb, nil
}

// ── HTTP handler ────────────────────────────────────────────────────

func (s *Server) handleStockDetails(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(chi.URLParam(r, "symbol"))
	exchange := r.URL.Query().Get("exchange")
	if exchange == "" {
		exchange = "NSE"
	}
	cacheKey := fmt.Sprintf("stock:details:%s:%s", exchange, symbol)

	// Serve from cache when fresh.
	if raw, err := s.rdb.Get(r.Context(), cacheKey).Bytes(); err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "hit")
		_, _ = w.Write(raw)
		return
	}

	d, err := s.fetchStockDetails(symbol, exchange)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "details unavailable: "+err.Error())
		return
	}
	payload, _ := json.Marshal(d)
	s.rdb.Set(r.Context(), cacheKey, payload, detailCacheTTL)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func (s *Server) fetchStockDetails(symbol, exchange string) (*StockDetails, error) {
	suffix := ".NS"
	if exchange == "BSE" {
		suffix = ".BO"
	}
	ySym := symbol + suffix

	d := &StockDetails{
		Symbol:    symbol,
		Exchange:  exchange,
		FetchedAt: time.Now().Format(time.RFC3339),
	}

	// 1. Fundamentals via quoteSummary (crumb-authed). Failure here is
	// non-fatal — risk metrics still get computed.
	if crumb, err := s.yahoo.getCrumb(); err == nil {
		url := fmt.Sprintf(
			"https://query1.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=defaultKeyStatistics,summaryDetail,financialData,price&crumb=%s",
			ySym, crumb)
		if b, err := s.yahoo.get(url); err == nil {
			parseFundamentals(b, d)
		}
	}

	// 2. Risk metrics from 1y daily closes vs NIFTY 50.
	stock, err := s.yahooCloses(ySym)
	if err != nil {
		return nil, err
	}
	nifty, err := s.yahooCloses("%5ENSEI")
	if err == nil {
		computeRisk(stock, nifty, d)
	}
	return d, nil
}

func parseFundamentals(b []byte, d *StockDetails) {
	var resp struct {
		QuoteSummary struct {
			Result []map[string]map[string]json.RawMessage `json:"result"`
		} `json:"quoteSummary"`
	}
	if json.Unmarshal(b, &resp) != nil || len(resp.QuoteSummary.Result) == 0 {
		return
	}
	r := resp.QuoteSummary.Result[0]
	num := func(mod, key string) float64 {
		raw, ok := r[mod][key]
		if !ok {
			return 0
		}
		var v struct {
			Raw float64 `json:"raw"`
		}
		if json.Unmarshal(raw, &v) == nil && v.Raw != 0 {
			return v.Raw
		}
		var f float64
		if json.Unmarshal(raw, &f) == nil {
			return f
		}
		return 0
	}
	str := func(mod, key string) string {
		raw, ok := r[mod][key]
		if !ok {
			return ""
		}
		var v string
		_ = json.Unmarshal(raw, &v)
		return v
	}
	d.Name = str("price", "longName")
	d.MarketCap = num("summaryDetail", "marketCap")
	d.TrailingPE = num("summaryDetail", "trailingPE")
	d.DividendYield = num("summaryDetail", "dividendYield")
	d.FiftyTwoWeekHigh = num("summaryDetail", "fiftyTwoWeekHigh")
	d.FiftyTwoWeekLow = num("summaryDetail", "fiftyTwoWeekLow")
	d.PriceToBook = num("defaultKeyStatistics", "priceToBook")
	d.TrailingEPS = num("defaultKeyStatistics", "trailingEps")
	d.BookValue = num("defaultKeyStatistics", "bookValue")
	d.DebtToEquity = num("financialData", "debtToEquity")
	d.ROE = num("financialData", "returnOnEquity")
}

// yahooCloses returns 1y of daily closes keyed by trading day.
func (s *Server) yahooCloses(ySym string) (map[int64]float64, error) {
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&range=1y", ySym)
	b, err := s.yahoo.get(url)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Close []float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(b, &resp); err != nil || len(resp.Chart.Result) == 0 {
		return nil, fmt.Errorf("bad chart response for %s", ySym)
	}
	r := resp.Chart.Result[0]
	if len(r.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("no quotes for %s", ySym)
	}
	out := map[int64]float64{}
	closes := r.Indicators.Quote[0].Close
	for i, ts := range r.Timestamp {
		if i < len(closes) && closes[i] > 0 {
			// normalize to day granularity so stock & index align
			out[ts/86400] = closes[i]
		}
	}
	return out, nil
}

// computeRisk fills beta/alpha/volatility from aligned daily returns.
func computeRisk(stock, index map[int64]float64, d *StockDetails) {
	var days []int64
	for day := range stock {
		if _, ok := index[day]; ok {
			days = append(days, day)
		}
	}
	if len(days) < 60 { // need a few months minimum
		return
	}
	// sort days ascending
	for i := 1; i < len(days); i++ {
		for j := i; j > 0 && days[j] < days[j-1]; j-- {
			days[j], days[j-1] = days[j-1], days[j]
		}
	}

	var rs, ri []float64
	for k := 1; k < len(days); k++ {
		ps, pi := stock[days[k-1]], index[days[k-1]]
		rs = append(rs, stock[days[k]]/ps-1)
		ri = append(ri, index[days[k]]/pi-1)
	}
	n := float64(len(rs))
	var ms, mi float64
	for k := range rs {
		ms += rs[k]
		mi += ri[k]
	}
	ms /= n
	mi /= n
	var cov, varI, varS float64
	for k := range rs {
		cov += (rs[k] - ms) * (ri[k] - mi)
		varI += (ri[k] - mi) * (ri[k] - mi)
		varS += (rs[k] - ms) * (rs[k] - ms)
	}
	cov /= n
	varI /= n
	varS /= n
	if varI == 0 {
		return
	}

	const rfDaily = 0.07 / 252 // ~India 10Y yield
	beta := cov / varI
	alphaDaily := ms - (rfDaily + beta*(mi-rfDaily))

	d.Beta = round2(beta)
	d.AlphaAnnual = round2(alphaDaily * 252 * 100)
	d.VolAnnual = round2(math.Sqrt(varS) * math.Sqrt(252) * 100)
	first, last := stock[days[0]], stock[days[len(days)-1]]
	if first > 0 {
		d.YearReturn = round2((last/first - 1) * 100)
	}
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
