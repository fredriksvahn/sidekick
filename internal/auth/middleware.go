package auth

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/google/uuid"
)

const sessionCookie = "sidekick_session"

type contextKey struct{}

// UserIDFromContext extracts the authenticated user's UUID from the request
// context. Returns (uuid.Nil, false) if no authenticated user is present.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(contextKey{}).(uuid.UUID)
	return id, ok
}

// RequireAuth wraps a handler with session validation. If no valid session
// cookie is present the request is rejected with 401 before next is called.
// On success the user_id is injected into the request context.
func RequireAuth(db *sql.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		sess, err := GetSession(db, cookie.Value)
		if err != nil || sess == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), contextKey{}, sess.UserID)
		next(w, r.WithContext(ctx))
	}
}
