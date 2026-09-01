package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// scanPick mirrors one scan_results row for the Buy Signals tab.
type scanPick struct {
	Symbol  string             `json:"symbol"`
	Rank    int                `json:"rank"`
	Score   float64            `json:"score"`
	Close   float64            `json:"close"`
	Reasons []string           `json:"reasons"`
	Metrics map[string]float64 `json:"metrics"`
}

type scanResponse struct {
	ScanDate string     `json:"scan_date"`
	Picks    []scanPick `json:"picks"`
}

// handleLatestScan returns the most recent day's ranked buy picks.
func (s *Server) handleLatestScan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var scanDate time.Time
	err := s.db.QueryRow(ctx, `select max(scan_date) from scan_results`).Scan(&scanDate)
	if err != nil || scanDate.IsZero() {
		writeJSON(w, http.StatusOK, scanResponse{Picks: []scanPick{}})
		return
	}

	rows, err := s.db.Query(ctx, `
		select symbol, rank, score, close, reasons_json, metrics_json
		from scan_results
		where scan_date = $1
		order by rank`, scanDate)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	resp := scanResponse{ScanDate: scanDate.Format("2006-01-02"), Picks: []scanPick{}}
	for rows.Next() {
		var p scanPick
		var reasons, metrics []byte
		if err := rows.Scan(&p.Symbol, &p.Rank, &p.Score, &p.Close, &reasons, &metrics); err != nil {
			writeErr(w, http.StatusInternalServerError, "scan failed")
			return
		}
		_ = json.Unmarshal(reasons, &p.Reasons)
		_ = json.Unmarshal(metrics, &p.Metrics)
		resp.Picks = append(resp.Picks, p)
	}
	writeJSON(w, http.StatusOK, resp)
}
