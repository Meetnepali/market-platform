// seed downloads Zerodha's public instrument master (no auth required)
// and upserts NSE equity instruments into Supabase.
//
// Usage:
//
//	go run ./cmd/seed                 # top NIFTY-liquid subset (default)
//	go run ./cmd/seed -all            # every NSE equity (~2000 rows)
//	go run ./cmd/seed -symbols TATAMOTORS,WIPRO,SBIN
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/utkrusht/market-platform/backend/internal/config"
	"github.com/utkrusht/market-platform/backend/internal/platform"
)

// Kite publishes per-exchange instrument dumps publicly (no auth).
// NFO/BFO are the NSE/BSE derivative segments (futures & options).
var instrumentDumps = map[string]string{
	"NSE": "https://api.kite.trade/instruments/NSE",
	"BSE": "https://api.kite.trade/instruments/BSE",
	"NFO": "https://api.kite.trade/instruments/NFO",
	"BFO": "https://api.kite.trade/instruments/BFO",
}

// fnoUnderlyings limits option seeding to liquid underlyings; futures
// are seeded for every underlying. Pass -fno-all to override.
var fnoUnderlyings = map[string]bool{
	"NIFTY": true, "BANKNIFTY": true, "FINNIFTY": true, "MIDCPNIFTY": true,
	"RELIANCE": true, "TCS": true, "HDFCBANK": true, "INFY": true, "SBIN": true,
}

// A liquid default universe so a fresh install has something sensible.
var defaultUniverse = map[string]bool{
	"RELIANCE": true, "TCS": true, "HDFCBANK": true, "INFY": true, "ICICIBANK": true,
	"SBIN": true, "BHARTIARTL": true, "ITC": true, "LT": true, "KOTAKBANK": true,
	"AXISBANK": true, "HINDUNILVR": true, "BAJFINANCE": true, "MARUTI": true,
	"TATAMOTORS": true, "SUNPHARMA": true, "TITAN": true, "WIPRO": true,
	"ULTRACEMCO": true, "NTPC": true,
}

func main() {
	all := flag.Bool("all", false, "seed every equity instead of the default liquid subset")
	symbols := flag.String("symbols", "", "comma-separated symbols to seed (overrides default subset)")
	exchanges := flag.String("exchanges", "NSE,BSE", "comma-separated exchanges to seed")
	flag.Parse()

	if err := run(*all, *symbols, *exchanges); err != nil {
		slog.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

type row struct {
	token      int64
	symbol     string
	tickSize   float64
	lotSize    int
	kind       string // EQ | FUT | CE | PE
	expiry     string // yyyy-mm-dd, derivatives only
	strike     float64
	underlying string
}

func run(all bool, symbolsCSV, exchangesCSV string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.Require(map[string]string{"DATABASE_URL": cfg.DatabaseURL}); err != nil {
		return err
	}

	want := defaultUniverse
	if symbolsCSV != "" {
		want = map[string]bool{}
		for _, s := range strings.Split(symbolsCSV, ",") {
			want[strings.ToUpper(strings.TrimSpace(s))] = true
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := platform.NewPostgres(ctx, cfg.DirectDatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	for _, exchange := range strings.Split(exchangesCSV, ",") {
		exchange = strings.ToUpper(strings.TrimSpace(exchange))
		url, ok := instrumentDumps[exchange]
		if !ok {
			return fmt.Errorf("unknown exchange %q", exchange)
		}
		rows, total, err := download(url, exchange, all, want)
		if err != nil {
			return fmt.Errorf("%s: %w", exchange, err)
		}
		fmt.Printf("%s: downloaded %d instruments, seeding %d equities\n", exchange, total, len(rows))
		if err := upsert(ctx, db, exchange, rows); err != nil {
			return fmt.Errorf("%s: %w", exchange, err)
		}
	}
	return nil
}

// download fetches the public instrument dump (CSV, no auth) and
// filters it to the requested exchange's cash equities.
func download(url, exchange string, all bool, want map[string]bool) ([]row, int, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, 0, fmt.Errorf("download instrument dump: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("instrument dump returned %s", resp.Status)
	}

	r := csv.NewReader(resp.Body)
	header, err := r.Read()
	if err != nil {
		return nil, 0, err
	}
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	required := []string{"instrument_token", "tradingsymbol", "instrument_type", "segment", "tick_size", "lot_size"}
	if exchange == "NFO" || exchange == "BFO" {
		required = append(required, "name", "expiry", "strike")
	}
	for _, want := range required {
		if _, ok := col[want]; !ok {
			return nil, 0, fmt.Errorf("instrument dump missing column %q", want)
		}
	}

	derivatives := exchange == "NFO" || exchange == "BFO"
	var rows []row
	total := 0
	for {
		rec, err := r.Read()
		if err != nil {
			break
		}
		total++
		itype := rec[col["instrument_type"]]
		sym := rec[col["tradingsymbol"]]
		token, _ := strconv.ParseInt(rec[col["instrument_token"]], 10, 64)
		tick, _ := strconv.ParseFloat(rec[col["tick_size"]], 64)
		lot, _ := strconv.Atoi(rec[col["lot_size"]])

		if derivatives {
			underlying := rec[col["name"]]
			switch itype {
			case "FUT":
				// all futures
			case "CE", "PE":
				if !all && !fnoUnderlyings[underlying] {
					continue
				}
			default:
				continue
			}
			strike, _ := strconv.ParseFloat(rec[col["strike"]], 64)
			rows = append(rows, row{
				token: token, symbol: sym, tickSize: tick, lotSize: lot,
				kind: itype, expiry: rec[col["expiry"]], strike: strike,
				underlying: underlying,
			})
			continue
		}

		if itype != "EQ" || rec[col["segment"]] != exchange {
			continue
		}
		if !all && !want[sym] {
			continue
		}
		rows = append(rows, row{token: token, symbol: sym, tickSize: tick, lotSize: lot, kind: "EQ"})
	}
	return rows, total, nil
}

func upsert(ctx context.Context, db *pgxpool.Pool, exchange string, rows []row) error {
	batch := &pgx.Batch{}
	for _, rw := range rows {
		var expiry, underlying any
		var strike any
		if rw.expiry != "" {
			expiry = rw.expiry
		}
		if rw.underlying != "" {
			underlying = rw.underlying
		}
		if rw.strike > 0 {
			strike = rw.strike
		}
		batch.Queue(`
			insert into instruments (exchange, symbol, provider_token, tick_size, lot_size, kind, expiry, strike, underlying)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			on conflict (exchange, symbol) do update
			set provider_token = excluded.provider_token,
			    tick_size = excluded.tick_size,
			    lot_size = excluded.lot_size,
			    kind = excluded.kind,
			    expiry = excluded.expiry,
			    strike = excluded.strike,
			    underlying = excluded.underlying,
			    active = true`,
			exchange, rw.symbol, rw.token, rw.tickSize, rw.lotSize, rw.kind, expiry, strike, underlying)
	}
	if err := db.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	fmt.Printf("%s: seeded %d instruments into Supabase\n", exchange, len(rows))
	return nil
}
