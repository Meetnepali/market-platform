package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Server struct {
	rdb   *redis.Client
	db    *pgxpool.Pool
	auth  *Authenticator
	log   *slog.Logger
	yahoo *yahooClient
}

func NewServer(rdb *redis.Client, db *pgxpool.Pool, auth *Authenticator, log *slog.Logger) *Server {
	return &Server{rdb: rdb, db: db, auth: auth, log: log.With("component", "api"), yahoo: newYahooClient()}
}

func (s *Server) Router(corsOrigin string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(slogRequestLogger(s.log))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{corsOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Liveness/readiness for the load balancer & orchestrator.
	r.Get("/healthz", s.handleHealth)

	r.Route("/api", func(r chi.Router) {
		// Public reference data.
		r.Get("/instruments", s.handleListInstruments)

		// Authenticated routes.
		r.Group(func(r chi.Router) {
			r.Use(s.auth.Middleware)
			r.Get("/quotes/{symbol}", s.handleGetQuote)
			r.Get("/stocks/{symbol}", s.handleStockDetails)
			r.Get("/history/{symbol}", s.handleHistory)
			r.Get("/fno/underlyings", s.handleFnoUnderlyings)
			r.Get("/fno/{underlying}", s.handleFnoContracts)
			r.Get("/candles/{symbol}", s.handleGetCandles)
			r.Get("/signals", s.handleListSignals)
			r.Post("/strategies", s.handleCreateStrategy)
			r.Put("/strategies/{id}", s.handleUpdateStrategy)
			r.Post("/strategies/{id}/enable", s.handleSetStrategyEnabled(true))
			r.Post("/strategies/{id}/disable", s.handleSetStrategyEnabled(false))
		})
	})

	// Live quote stream (token passed as ?token= — browsers can't set WS
	// headers). Auth is enforced inside the handler.
	r.Get("/ws/quotes", s.handleQuoteSocket)

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.rdb.Ping(ctx).Err(); err != nil {
		http.Error(w, `{"status":"redis down"}`, http.StatusServiceUnavailable)
		return
	}
	if err := s.db.Ping(ctx); err != nil {
		http.Error(w, `{"status":"db down"}`, http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── helpers ─────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func slogRequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"dur_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}
