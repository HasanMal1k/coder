package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/HasanMal1k/coder/internal/auth/model"
)

// contextKey is unexported so no other package can accidentally collide with
// the key we store the authenticated user under.
type contextKey int

const userContextKey contextKey = 0

// RequireAuth verifies the Authorization header on every request that
// reaches it, and only calls next if the token is valid.
func (h *UserHandler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		tokenString, ok := strings.CutPrefix(authHeader, "Bearer ")
		if !ok || tokenString == "" {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "missing or malformed authorization header"})
			return
		}

		user, err := h.svc.ParseJWT(r.Context(), tokenString)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid or expired token"})
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext returns the authenticated user a protected handler is
// running for. It only returns ok == true when called downstream of
// RequireAuth, which is the only place that sets it.
func UserFromContext(ctx context.Context) (*model.User, bool) {
	user, ok := ctx.Value(userContextKey).(*model.User)
	return user, ok
}
