package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/utkrusht/market-platform/backend/internal/engine"
	"github.com/utkrusht/market-platform/backend/internal/platform"
)

// ── Instruments ─────────────────────────────────────────────────────

func (s *Server) handleListInstruments(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		select id, exchange, symbol, active
		from instruments where active order by exchange, symbol`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type instrument struct {
		ID       int64  `json:"id"`
		Exchange string `json:"exchange"`
		Symbol   string `json:"symbol"`
		Active   bool   `json:"active"`
	}
	out := []instrument{}
	for rows.Next() {
		var in instrument
		if err := rows.Scan(&in.ID, &in.Exchange, &in.Symbol, &in.Active); err != nil {
			writeErr(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, in)
	}
	writeJSON(w, http.StatusOK, out)
}

// ── Quotes (Redis latest-state) ─────────────────────────────────────

func (s *Server) handleGetQuote(w http.ResponseWriter, r *http.Request) {
	symbol := chi.URLParam(r, "symbol")
	exchange := r.URL.Query().Get("exchange")
	if exchange == "" {
		exchange = "NSE"
	}
	raw, err := s.rdb.Get(r.Context(), platform.KeyQuote(exchange, symbol)).Bytes()
	if err != nil {
		writeErr(w, http.StatusNotFound, "no live quote for "+symbol)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// ── Candles ─────────────────────────────────────────────────────────

func (s *Server) handleGetCandles(w http.ResponseWriter, r *http.Request) {
	symbol := chi.URLParam(r, "symbol")
	limit := clampInt(r.URL.Query().Get("limit"), 500, 5000)

	rows, err := s.db.Query(r.Context(), `
		select c.candle_time, c.open, c.high, c.low, c.close, c.volume
		from candles_1m c
		join instruments i on i.id = c.instrument_id
		where i.symbol = $1
		order by c.candle_time desc
		limit $2`, symbol, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type bar struct {
		Time   time.Time `json:"time"`
		Open   float64   `json:"open"`
		High   float64   `json:"high"`
		Low    float64   `json:"low"`
		Close  float64   `json:"close"`
		Volume int64     `json:"volume"`
	}
	out := []bar{}
	for rows.Next() {
		var b bar
		if err := rows.Scan(&b.Time, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume); err != nil {
			writeErr(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, b)
	}
	writeJSON(w, http.StatusOK, out)
}

// ── Signals (user-scoped in SQL; RLS also enforces at the DB) ───────

func (s *Server) handleListSignals(w http.ResponseWriter, r *http.Request) {
	userID := UserID(r.Context())
	limit := clampInt(r.URL.Query().Get("limit"), 100, 1000)

	rows, err := s.db.Query(r.Context(), `
		select sig.id, sig.strategy_id, i.symbol, sig.signal_type,
		       sig.price, sig.metrics_json, sig.created_at
		from signals sig
		join strategies st on st.id = sig.strategy_id
		join instruments i on i.id = sig.instrument_id
		where st.user_id = $1
		order by sig.created_at desc
		limit $2`, userID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type signal struct {
		ID         string          `json:"id"`
		StrategyID string          `json:"strategy_id"`
		Symbol     string          `json:"symbol"`
		SignalType string          `json:"signal_type"`
		Price      float64         `json:"price"`
		Metrics    json.RawMessage `json:"metrics"`
		CreatedAt  time.Time       `json:"created_at"`
	}
	out := []signal{}
	for rows.Next() {
		var sg signal
		if err := rows.Scan(&sg.ID, &sg.StrategyID, &sg.Symbol, &sg.SignalType, &sg.Price, &sg.Metrics, &sg.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "scan failed")
			return
		}
		out = append(out, sg)
	}
	writeJSON(w, http.StatusOK, out)
}

// ── Strategies ──────────────────────────────────────────────────────

type strategyRequest struct {
	Name          string          `json:"name"`
	Configuration json.RawMessage `json:"configuration"`
	InstrumentIDs []int64         `json:"instrument_ids"`
}

func (s *Server) handleCreateStrategy(w http.ResponseWriter, r *http.Request) {
	userID := UserID(r.Context())
	var req strategyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name and configuration are required")
		return
	}
	// Validate the rule DSL before it can ever reach the engine.
	if _, err := engine.ParseRule(req.Configuration); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "invalid rule: "+err.Error())
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "tx failed")
		return
	}
	defer tx.Rollback(r.Context())

	var id string
	err = tx.QueryRow(r.Context(), `
		insert into strategies (user_id, name, configuration_json)
		values ($1, $2, $3) returning id`,
		userID, req.Name, req.Configuration).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "insert failed")
		return
	}
	for _, instID := range req.InstrumentIDs {
		if _, err := tx.Exec(r.Context(), `
			insert into strategy_instruments (strategy_id, instrument_id)
			values ($1, $2) on conflict do nothing`, id, instID); err != nil {
			writeErr(w, http.StatusBadRequest, "unknown instrument id")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "commit failed")
		return
	}
	s.notifyResubscribe(r)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleUpdateStrategy(w http.ResponseWriter, r *http.Request) {
	userID := UserID(r.Context())
	id := chi.URLParam(r, "id")
	var req strategyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	if _, err := engine.ParseRule(req.Configuration); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "invalid rule: "+err.Error())
		return
	}
	tag, err := s.db.Exec(r.Context(), `
		update strategies
		set name = coalesce(nullif($3, ''), name),
		    configuration_json = $4,
		    updated_at = now()
		where id = $1 and user_id = $2`,
		id, userID, req.Name, req.Configuration)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "strategy not found")
		return
	}
	s.notifyResubscribe(r)
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *Server) handleSetStrategyEnabled(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := UserID(r.Context())
		id := chi.URLParam(r, "id")
		tag, err := s.db.Exec(r.Context(), `
			update strategies set enabled = $3, updated_at = now()
			where id = $1 and user_id = $2`, id, userID, enabled)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusInternalServerError, "update failed")
			return
		}
		if tag.RowsAffected() == 0 {
			writeErr(w, http.StatusNotFound, "strategy not found")
			return
		}
		s.notifyResubscribe(r)
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": enabled})
	}
}

// notifyResubscribe tells the ingestion service the instrument universe
// may have changed.
func (s *Server) notifyResubscribe(r *http.Request) {
	_ = s.rdb.Publish(r.Context(), platform.ChanResubscribe, "changed").Err()
}

func clampInt(raw string, def, max int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
