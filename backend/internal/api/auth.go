// Package api is the HTTP surface: REST endpoints plus a thin WebSocket
// gateway for live quotes. Signals reach the browser via Supabase
// Realtime, not through this service.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

type ctxKey int

const userIDKey ctxKey = 1

// UserID returns the authenticated Supabase user id from the request
// context ("" if unauthenticated — only possible on public routes).
func UserID(ctx context.Context) string {
	id, _ := ctx.Value(userIDKey).(string)
	return id
}

// Authenticator verifies Supabase Auth JWTs. Prefers asymmetric keys via
// the project JWKS endpoint; falls back to the legacy HS256 shared
// secret when configured.
type Authenticator struct {
	jwks      keyfunc.Keyfunc
	hs256     []byte
	log       *slog.Logger
}

func NewAuthenticator(ctx context.Context, jwksURL, hs256Secret string, log *slog.Logger) (*Authenticator, error) {
	a := &Authenticator{log: log.With("component", "auth")}
	if hs256Secret != "" {
		a.hs256 = []byte(hs256Secret)
	}
	if jwksURL != "" {
		k, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
		if err != nil {
			// JWKS may be unreachable at boot in dev; log and rely on HS256.
			log.Warn("jwks unavailable", "url", jwksURL, "err", err)
		} else {
			a.jwks = k
		}
	}
	return a, nil
}

// Middleware rejects requests without a valid Supabase JWT and stashes
// the user id (sub claim) in the context.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			http.Error(w, `{"error":"missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		sub, err := a.verify(token)
		if err != nil {
			a.log.Debug("token rejected", "err", err)
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, sub)))
	})
}

func (a *Authenticator) verify(raw string) (string, error) {
	claims := jwt.MapClaims{}
	keyFn := func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); ok && a.hs256 != nil {
			return a.hs256, nil
		}
		if a.jwks != nil {
			return a.jwks.Keyfunc(t)
		}
		return nil, jwt.ErrTokenUnverifiable
	}
	tok, err := jwt.ParseWithClaims(raw, claims, keyFn,
		jwt.WithValidMethods([]string{"HS256", "RS256", "ES256"}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !tok.Valid {
		return "", err
	}
	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return "", jwt.ErrTokenInvalidSubject
	}
	return sub, nil
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	// WebSocket clients can't set headers from the browser; allow a
	// query token for /ws routes only (checked by the caller's route).
	if strings.HasPrefix(r.URL.Path, "/ws/") {
		return r.URL.Query().Get("token")
	}
	return ""
}
