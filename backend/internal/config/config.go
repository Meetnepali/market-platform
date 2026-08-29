// Package config loads service configuration from the environment.
// All services (api, ingestion, engine) share this loader; each reads
// only the fields it needs. Fail fast: a missing required variable is a
// startup error, never a silent default in production.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Shared
	Env      string // "dev" | "prod"
	LogLevel string // "debug" | "info" | "warn" | "error"

	// Redis
	RedisURL string

	// Supabase / Postgres
	DatabaseURL        string // pooled connection string (Supavisor, port 6543) for the api
	DirectDatabaseURL  string // direct connection (port 5432) for workers doing batch writes
	SupabaseURL        string // https://<project>.supabase.co
	SupabaseJWTSecret  string // legacy HS256 secret (empty when the project uses asymmetric keys)
	SupabaseJWKSURL    string // <SupabaseURL>/auth/v1/.well-known/jwks.json

	// Kite Connect (ingestion only)
	KiteAPIKey      string
	KiteAccessToken string

	// API server
	HTTPAddr        string
	CORSOrigin      string
	ShutdownTimeout time.Duration

	// Engine
	EngineConsumerGroup string
	EngineConsumerName  string
	CandleFlushInterval time.Duration
}

func Load() (*Config, error) {
	c := &Config{
		Env:      getenv("APP_ENV", "dev"),
		LogLevel: getenv("LOG_LEVEL", "info"),

		RedisURL: getenv("REDIS_URL", "redis://localhost:6379/0"),

		DatabaseURL:       os.Getenv("DATABASE_URL"),
		DirectDatabaseURL: getenv("DIRECT_DATABASE_URL", os.Getenv("DATABASE_URL")),
		SupabaseURL:       os.Getenv("SUPABASE_URL"),
		SupabaseJWTSecret: os.Getenv("SUPABASE_JWT_SECRET"),

		KiteAPIKey:      os.Getenv("KITE_API_KEY"),
		KiteAccessToken: os.Getenv("KITE_ACCESS_TOKEN"),

		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		CORSOrigin:      getenv("CORS_ORIGIN", "http://localhost:5173"),
		ShutdownTimeout: getdur("SHUTDOWN_TIMEOUT", 15*time.Second),

		EngineConsumerGroup: getenv("ENGINE_CONSUMER_GROUP", "strategy-engine"),
		EngineConsumerName:  getenv("ENGINE_CONSUMER_NAME", hostnameOr("engine-1")),
		CandleFlushInterval: getdur("CANDLE_FLUSH_INTERVAL", time.Minute),
	}
	if c.SupabaseURL != "" {
		c.SupabaseJWKSURL = c.SupabaseURL + "/auth/v1/.well-known/jwks.json"
	}
	return c, nil
}

// Require returns an error listing every named field that is empty.
// Each binary calls this with the vars it cannot run without.
func (c *Config) Require(fields map[string]string) error {
	var missing []string
	for name, val := range fields {
		if val == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %v", missing)
	}
	return nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getdur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return def
}

func hostnameOr(def string) string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return def
}
