// Package scanner implements the EOD "pullback in an uptrend" buy
// scan: rank NSE stocks that are strong long-term, just dipped, and
// where selling pressure is fading. Results land in scan_results and
// feed the Buy Signals tab.
package scanner

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/utkrusht/market-platform/backend/internal/ingest"
)

type Scanner struct {
	db      *pgxpool.Pool
	fetcher HistoryFetcher
	log     *slog.Logger
	cfg     Config
	topN    int
	workers int
}

func New(db *pgxpool.Pool, fetcher HistoryFetcher, log *slog.Logger, cfg Config, topN, workers int) *Scanner {
	if topN <= 0 {
		topN = 10
	}
	return &Scanner{
		db:      db,
		fetcher: fetcher,
		log:     log.With("component", "scanner"),
		cfg:     cfg,
		topN:    topN,
		workers: workers,
	}
}

// Run executes one full scan: load universe → fetch histories →
// evaluate → rank → persist today's top picks.
func (s *Scanner) Run(ctx context.Context) error {
	universe, err := s.loadUniverse(ctx)
	if err != nil {
		return err
	}
	s.log.Info("universe loaded", "stocks", len(universe))

	// Benchmark first — every stock's relative strength needs it.
	index, err := s.fetcher.Daily(ctx, NiftyIndexSymbol)
	if err != nil {
		return err
	}

	symbols := make([]string, 0, len(universe))
	for sym := range universe {
		symbols = append(symbols, sym)
	}
	start := time.Now()
	histories := fetchAll(ctx, s.fetcher, symbols, s.workers)
	s.log.Info("histories fetched",
		"ok", len(histories), "failed", len(symbols)-len(histories),
		"took", time.Since(start).Round(time.Second))

	var results []*Result
	for sym, h := range histories {
		if r, ok := Evaluate(h, index, s.cfg); ok {
			r.InstrumentID = universe[sym]
			results = append(results, r)
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > s.topN {
		results = results[:s.topN]
	}
	for i, r := range results {
		r.Rank = i + 1
	}

	if err := s.persist(ctx, results); err != nil {
		return err
	}
	s.log.Info("scan complete", "candidates", len(results))
	for _, r := range results {
		s.log.Info("pick", "rank", r.Rank, "symbol", r.Symbol, "score", int(r.Score))
	}
	return nil
}

// loadUniverse returns active NSE stocks (symbol → instrument id),
// excluding index instruments — you can't buy "NIFTY 50" as a stock.
func (s *Scanner) loadUniverse(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.Query(ctx, `
		select symbol, id from instruments
		where active and exchange = 'NSE'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int64{}
	for rows.Next() {
		var sym string
		var id int64
		if err := rows.Scan(&sym, &id); err != nil {
			return nil, err
		}
		if _, isIndex := ingest.IndexYahooSymbols[sym]; isIndex {
			continue
		}
		out[sym] = id
	}
	return out, rows.Err()
}

// persist replaces today's scan atomically so re-running the scanner
// (retry, manual run) never duplicates rows.
func (s *Scanner) persist(ctx context.Context, results []*Result) error {
	// Scan date in IST — the market this data belongs to.
	ist := time.FixedZone("IST", 5*3600+1800)
	scanDate := time.Now().In(ist).Format("2006-01-02")

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `delete from scan_results where scan_date = $1`, scanDate); err != nil {
		return err
	}
	for _, r := range results {
		reasons, _ := json.Marshal(r.Reasons)
		metrics, _ := json.Marshal(r.Metrics)
		if _, err := tx.Exec(ctx, `
			insert into scan_results
			  (scan_date, instrument_id, symbol, rank, score, close, reasons_json, metrics_json)
			values ($1, $2, $3, $4, $5, $6, $7, $8)`,
			scanDate, r.InstrumentID, r.Symbol, r.Rank, r.Score, r.Close, reasons, metrics); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
