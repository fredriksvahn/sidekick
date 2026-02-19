package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"net/http"
	"os"
	"strings"

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

// apiKeyUserID resolves the user ID to associate with an API key request.
// It looks up the bootstrap user by SIDEKICK_AUTH_EMAIL.
func apiKeyUserID(db *sql.DB) (uuid.UUID, bool) {
	email := os.Getenv("SIDEKICK_AUTH_EMAIL")
	if email == "" {
		return uuid.Nil, false
	}
	user, err := GetUserByEmail(db, email)
	if err != nil || user == nil {
		return uuid.Nil, false
	}
	return user.ID, true
}

// RequireAuth wraps a handler with authentication. It first checks for an API
// key in the Authorization: Bearer header matched against SIDEKICK_API_KEY. If
// that check is absent or does not match, it falls through to cookie-based
// session auth. On success the user_id is injected into the request context.
func RequireAuth(db *sql.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// --- API key check (runs only when SIDEKICK_API_KEY is configured) ---
		if apiKey := os.Getenv("SIDEKICK_API_KEY"); apiKey != "" {
			if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
				token := strings.TrimPrefix(header, "Bearer ")
				tokenHash := sha256.Sum256([]byte(token))
				keyHash := sha256.Sum256([]byte(apiKey))
				if subtle.ConstantTimeCompare(tokenHash[:], keyHash[:]) == 1 {
					if userID, ok := apiKeyUserID(db); ok {
						ctx := context.WithValue(r.Context(), contextKey{}, userID)
						next(w, r.WithContext(ctx))
						return
					}
				}
			}
		}

		// --- Session cookie fallback ---
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
