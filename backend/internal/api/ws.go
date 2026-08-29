package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/utkrusht/market-platform/backend/internal/platform"
)

// handleQuoteSocket streams live quotes for the requested symbols.
// It uses SSE (text/event-stream) rather than a raw WebSocket: no extra
// dependency, passes cleanly through proxies, and the browser's
// EventSource auto-reconnects for free. The client subscribes:
//
//	GET /ws/quotes?symbols=RELIANCE,TCS&exchange=NSE&token=<jwt>
//
// Each event is one canonical Quote JSON. Poll cadence is 1s against
// Redis latest-state, which matches UI needs without fanning the raw
// tick stream to every browser.
func (s *Server) handleQuoteSocket(w http.ResponseWriter, r *http.Request) {
	// Auth: EventSource cannot set headers; token comes via query param.
	token := r.URL.Query().Get("token")
	if token == "" {
		writeErr(w, http.StatusUnauthorized, "missing token")
		return
	}
	if _, err := s.auth.verify(token); err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}

	rawSymbols := r.URL.Query().Get("symbols")
	all := rawSymbols == "*"
	symbols := splitCSV(rawSymbols)
	if !all && (len(symbols) == 0 || len(symbols) > 500) {
		writeErr(w, http.StatusBadRequest, "1-500 symbols required (or * for all)")
		return
	}
	exchange := r.URL.Query().Get("exchange")
	if exchange == "" {
		exchange = "NSE"
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var keys []string
	if !all {
		keys = make([]string, len(symbols))
		for i, sym := range symbols {
			keys[i] = platform.KeyQuote(exchange, sym)
		}
	}

	// Wide subscriptions poll every 3s; narrow ones every second.
	interval := time.Second
	if all {
		interval = 3 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	ctx := r.Context()

	for {
		if all {
			var err error
			keys, err = s.rdb.Keys(ctx, "market:quote:*").Result()
			if err != nil || len(keys) == 0 {
				keys = nil
			}
		}
		if len(keys) > 0 {
			s.pushQuotes(ctx, w, flusher, keys)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) pushQuotes(ctx context.Context, w http.ResponseWriter, f http.Flusher, keys []string) {
	vals, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return
	}
	wrote := false
	for _, v := range vals {
		str, ok := v.(string)
		if !ok {
			continue
		}
		if _, err := w.Write([]byte("data: " + str + "\n\n")); err != nil {
			return
		}
		wrote = true
	}
	if wrote {
		f.Flush()
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToUpper(p))
		}
	}
	return out
}
