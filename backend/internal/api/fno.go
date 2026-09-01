package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/utkrusht/market-platform/backend/internal/ingest"
	"github.com/utkrusht/market-platform/backend/internal/market"
)

// ── Price history proxy (candlestick charts) ────────────────────────
//
// GET /api/history/{symbol}?exchange=NSE&range=3mo&interval=1d
// Proxies Yahoo's chart endpoint (indices included via the shared
// symbol map) so the browser never talks to Yahoo directly. Cached in
// Redis; intraday ranges get a short TTL, daily ranges a long one.

var validRanges = map[string]bool{
	"1d": true, "5d": true, "1mo": true, "3mo": true, "6mo": true,
	"1y": true, "2y": true, "5y": true, "max": true,
}
var validIntervals = map[string]bool{
	"1m": true, "5m": true, "15m": true, "1h": true, "1d": true, "1wk": true,
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(chi.URLParam(r, "symbol"))
	exchange := market.Exchange(r.URL.Query().Get("exchange"))
	if exchange == "" {
		exchange = market.NSE
	}
	rng := r.URL.Query().Get("range")
	if !validRanges[rng] {
		rng = "3mo"
	}
	interval := r.URL.Query().Get("interval")
	if !validIntervals[interval] {
		interval = "1d"
	}

	cacheKey := fmt.Sprintf("history:%s:%s:%s:%s", exchange, symbol, rng, interval)
	if raw, err := s.rdb.Get(r.Context(), cacheKey).Bytes(); err == nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
		return
	}

	ySym := ingest.YahooSymbol(exchange, symbol)
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=%s&range=%s",
		neturl.PathEscape(ySym), interval, rng)
	b, err := s.yahoo.get(url)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "history unavailable")
		return
	}

	bars, err := parseChartBars(b)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	payload, _ := json.Marshal(bars)

	ttl := time.Hour
	if interval != "1d" && interval != "1wk" {
		ttl = 2 * time.Minute // intraday: refresh often
	}
	s.rdb.Set(r.Context(), cacheKey, payload, ttl)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

type historyBar struct {
	Time   int64   `json:"time"` // unix seconds
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

func parseChartBars(b []byte) ([]historyBar, error) {
	var resp struct {
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
		} `json:"chart"`
	}
	if err := json.Unmarshal(b, &resp); err != nil || len(resp.Chart.Result) == 0 {
		return nil, fmt.Errorf("no history data")
	}
	r := resp.Chart.Result[0]
	if len(r.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("no history data")
	}
	q := r.Indicators.Quote[0]
	bars := make([]historyBar, 0, len(r.Timestamp))
	for i, ts := range r.Timestamp {
		if i >= len(q.Close) || q.Close[i] == 0 {
			continue
		}
		bars = append(bars, historyBar{
			Time: ts, Open: q.Open[i], High: q.High[i], Low: q.Low[i],
			Close: q.Close[i], Volume: at(q.Volume, i),
		})
	}
	return bars, nil
}

func at(v []int64, i int) int64 {
	if i < len(v) {
		return v[i]
	}
	return 0
}

// ── F&O contract browser ────────────────────────────────────────────
//
// GET /api/fno/underlyings          → underlyings with contract counts
// GET /api/fno/{underlying}         → futures + option expiries/strikes
//
// Contract metadata comes from the seeded Kite NFO/BFO master. Live
// F&O prices require the Kite feed (FEED=kite); until then the
// structure is browsable but prices show as unavailable.

func (s *Server) handleFnoUnderlyings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		select underlying,
		       count(*) filter (where kind = 'FUT') as futures,
		       count(*) filter (where kind in ('CE','PE')) as options
		from instruments
		where kind in ('FUT','CE','PE') and active and expiry >= current_date
		group by underlying
		order by options desc, underlying
		limit 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	type u struct {
		Underlying string `json:"underlying"`
		Futures    int    `json:"futures"`
		Options    int    `json:"options"`
	}
	out := []u{}
	for rows.Next() {
		var x u
		if err := rows.Scan(&x.Underlying, &x.Futures, &x.Options); err == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleFnoContracts(w http.ResponseWriter, r *http.Request) {
	underlying := strings.ToUpper(chi.URLParam(r, "underlying"))

	rows, err := s.db.Query(r.Context(), `
		select symbol, kind, expiry, coalesce(strike, 0), coalesce(lot_size, 0)
		from instruments
		where underlying = $1 and kind in ('FUT','CE','PE')
		  and active and expiry >= current_date
		order by expiry, kind, strike`, underlying)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type contract struct {
		Symbol  string  `json:"symbol"`
		Kind    string  `json:"kind"`
		Expiry  string  `json:"expiry"`
		Strike  float64 `json:"strike,omitempty"`
		LotSize int     `json:"lot_size"`
	}
	futures := []contract{}
	optionsByExpiry := map[string][]contract{}
	for rows.Next() {
		var c contract
		var expiry time.Time
		if err := rows.Scan(&c.Symbol, &c.Kind, &expiry, &c.Strike, &c.LotSize); err != nil {
			continue
		}
		c.Expiry = expiry.Format("2006-01-02")
		if c.Kind == "FUT" {
			futures = append(futures, c)
		} else {
			optionsByExpiry[c.Expiry] = append(optionsByExpiry[c.Expiry], c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"underlying":        underlying,
		"futures":           futures,
		"options_by_expiry": optionsByExpiry,
	})
}
