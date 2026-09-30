package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/HasanMal1k/coder/internal/auth"
	"github.com/HasanMal1k/coder/internal/store"
)

// contextKey is unexported so no other package can accidentally collide with
// the key we store the authenticated user under.
type contextKey int

const userContextKey contextKey = 0

// RequireAuth verifies the Authorization header on every request that
// reaches it, and only calls next if the token is valid.
func RequireAuth(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")

			tokenString, ok := strings.CutPrefix(authHeader, "Bearer ")
			if !ok || tokenString == "" {
				unauthorized(w, "missing or malformed authorization header")
				return
			}

			user, err := svc.ParseJWT(r.Context(), tokenString)
			if err != nil {
				unauthorized(w, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserFromContext returns the authenticated user a protected handler is
// running for. It only returns ok == true when called downstream of
// RequireAuth, which is the only place that sets it.
func UserFromContext(ctx context.Context) (*store.User, bool) {
	user, ok := ctx.Value(userContextKey).(*store.User)
	return user, ok
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
