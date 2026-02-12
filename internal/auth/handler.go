package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

// secureCookies returns true only if SIDEKICK_COOKIE_SECURE is explicitly "true".
// Defaults to false for easier local development over HTTP.
// Set SIDEKICK_COOKIE_SECURE=true in production with HTTPS.
func secureCookies() bool {
	return strings.ToLower(os.Getenv("SIDEKICK_COOKIE_SECURE")) == "true"
}

// HandleLogin handles POST /auth/login.
// Validates email + password, creates a session, and sets the session cookie.
func HandleLogin(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		email := strings.TrimSpace(req.Email)
		if email == "" || req.Password == "" {
			http.Error(w, "email and password required", http.StatusBadRequest)
			return
		}

		user, err := GetUserByEmail(db, email)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// Deliberate: same response for "user not found" and "wrong password".
		if user == nil || !user.CheckPassword(req.Password) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		sess, err := CreateSession(db, user.ID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if err := MarkLogin(db, user.ID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    sess.Token,
			Path:     "/",
			HttpOnly: true,
			Secure:   secureCookies(),
			SameSite: http.SameSiteLaxMode,
			Expires:  sess.ExpiresAt,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_id":    user.ID.String(),
			"email":      user.Email,
			"expires_at": sess.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
}

// HandleLogout handles POST /auth/logout.
// Deletes the server-side session (best-effort) and clears the cookie.
// Does not require an active session — safe to call when already logged out.
func HandleLogout(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if cookie, err := r.Cookie(sessionCookie); err == nil {
			_ = DeleteSession(db, cookie.Value) // best-effort
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   secureCookies(),
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})

		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleMe handles GET /auth/me.
// Must be wrapped with RequireAuth. Returns the current user's public fields.
func HandleMe(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		userID, ok := UserIDFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var email string
		var createdAt time.Time
		var lastLogin sql.NullTime
		err := db.QueryRow(`
			SELECT email, created_at, last_login_at FROM users WHERE id = $1
		`, userID).Scan(&email, &createdAt, &lastLogin)
		if err == sql.ErrNoRows {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		resp := map[string]any{
			"user_id":    userID.String(),
			"email":      email,
			"created_at": createdAt.UTC().Format(time.RFC3339),
		}
		if lastLogin.Valid {
			resp["last_login_at"] = lastLogin.Time.UTC().Format(time.RFC3339)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
