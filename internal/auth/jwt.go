// Package auth provides JWT authentication. The athlete_id is always derived from the
// verified token — never from the request path or body — so tenant scoping can't be spoofed.
package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/jetlum/playerboard/internal/platform/httpx"
)

type ctxKey int

const (
	keyAthlete ctxKey = iota
	keyRole
)

// Claims carries the athlete_id (Subject) and role (athlete|agent).
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// Mint issues a signed access token. Used by the dev token minter.
func Mint(secret, athleteID, role string, ttl time.Duration) (string, error) {
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   athleteID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// Middleware validates the bearer token and injects athlete_id + role into the context.
func Middleware(secret string) func(http.Handler) http.Handler {
	key := []byte(secret)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := bearer(r)
			if tok == "" {
				httpx.Error(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			claims := &Claims{}
			parsed, err := jwt.ParseWithClaims(tok, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrTokenSignatureInvalid
				}
				return key, nil
			})
			if err != nil || !parsed.Valid {
				httpx.Error(w, http.StatusUnauthorized, "invalid token")
				return
			}
			aid, err := uuid.Parse(claims.Subject)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "invalid subject")
				return
			}
			ctx := context.WithValue(r.Context(), keyAthlete, aid)
			ctx = context.WithValue(ctx, keyRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	// WebSocket clients can't set Authorization easily; allow ?token= for the stream endpoint.
	return r.URL.Query().Get("token")
}

// AthleteID returns the authenticated athlete_id from the context.
func AthleteID(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(keyAthlete).(uuid.UUID)
	return v, ok
}

// Role returns the authenticated role (athlete|agent).
func Role(ctx context.Context) string {
	v, _ := ctx.Value(keyRole).(string)
	return v
}
