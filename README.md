# Market Platform — NSE/BSE Live Data Processing & Signals

Go backend + React frontend + Supabase. Receives live NSE/BSE market data,
runs it through configurable strategy rules, and pushes only the resulting
signals to the UI. Raw ticks are never stored — hot state lives in Redis,
only signals and 1-minute candles reach Postgres.

## Stack

| Layer | Tech |
|---|---|
| Frontend | React 18 + Vite + TypeScript (SPA) |
| Backend | Go 1.23 — three binaries: `api`, `ingestion`, `engine` |
| Data / Auth / Realtime | Supabase (Postgres 15, RLS, Realtime) |
| Hot state | Redis 7 (Streams + Pub/Sub) |
| Market data | Zerodha Kite Connect (adapter-isolated, swappable) |

## Architecture

```
Kite Connect WS
      ↓
ingestion  ── normalize + validate ──►  Redis
                                        ├─ latest quote (market:quote:*)
                                        └─ tick stream  (market:ticks)
                                                ↓ consumer group
                                             engine
                                        ├─ rolling metrics (SMA/RSI/vol)
                                        ├─ rule DSL evaluation
                                        ├─ signals → Supabase → Realtime → browser
                                        └─ 1-min candles → Supabase (batch)
      api  ── REST + SSE live quotes ──►  React SPA
```

## Getting started

1. **Supabase**: create a project, then apply the migration:
   ```sh
   supabase link --project-ref <ref>
   supabase db push
   ```
2. **Env**: `cp .env.example .env` and fill in Supabase + Kite credentials.
3. **Backend deps** (first time):
   ```sh
   cd backend && go mod tidy
   ```
4. **Run everything**:
   ```sh
   docker compose up --build
   ```
   Or individually during development:
   ```sh
   cd backend && go run ./cmd/api        # :8080
   cd backend && go run ./cmd/ingestion  # needs Kite creds + market hours
   cd backend && go run ./cmd/engine
   cd web && npm install && npm run dev  # :5173
   ```
5. **Tests**:
   ```sh
   cd backend && go test ./...
   ```

## Key design decisions

- **Ticks are never persisted.** Redis holds latest state + a capped stream;
  Postgres receives only signals and per-minute candle upserts.
- **Strategies are a validated JSON DSL** (`internal/engine/rules.go`) — no
  user code ever executes. Unknown metrics/operators are rejected at save time.
- **Signals reach the browser via Supabase Realtime** on the `signals` table;
  RLS ensures a user only receives their own strategies' rows.
- **Live quotes** stream over SSE from the Go api (`/ws/quotes`), reading
  Redis latest-state — the raw feed is never fanned out to browsers.
- **Provider isolation**: `internal/ingest/provider.go` defines the `Feed`
  interface; moving to a licensed NSE/BSE vendor feed (Phase 6) replaces one
  adapter.
- **Signal debounce/idempotency** via Redis `SET NX` cooldown keys, so
  at-least-once stream delivery cannot double-fire a signal.

## Licensing gate (before any commercial launch)

Market data availability in a consumer product is **not** a redistribution
license. Review the Kite Connect terms and NSE/BSE data-sharing agreements
before exposing data publicly or commercially.

## Layout

```
backend/
  cmd/{api,ingestion,engine}/   # binaries
  internal/config/              # env config
  internal/market/              # canonical Quote/Signal/Candle models
  internal/platform/            # Redis + Postgres clients, key layout
  internal/ingest/              # Feed interface + Kite adapter + publisher
  internal/engine/              # rule DSL, metrics, consumer, candles
  deploy/Dockerfile             # multi-stage, distroless
supabase/migrations/            # schema + RLS + realtime publication
web/                            # Vite + React + TS SPA
```
