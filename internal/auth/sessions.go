package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const sessionDuration = 24 * time.Hour

// Session represents a row in the sessions table.
type Session struct {
	Token     string
	UserID    uuid.UUID
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// newToken generates a cryptographically random 32-byte token, hex-encoded (64 chars).
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateSession inserts a new session for the given user with a 24-hour lifetime.
func CreateSession(db *sql.DB, userID uuid.UUID) (*Session, error) {
	token, err := newToken()
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(sessionDuration)

	var sess Session
	err = db.QueryRow(`
		INSERT INTO sessions (token, user_id, issued_at, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING token, user_id, issued_at, expires_at
	`, token, userID, now, expiresAt).Scan(&sess.Token, &sess.UserID, &sess.IssuedAt, &sess.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return &sess, nil
}

// GetSession loads a session by token. Returns (nil, nil) if the token does
// not exist or has expired.
func GetSession(db *sql.DB, token string) (*Session, error) {
	var sess Session
	err := db.QueryRow(`
		SELECT token, user_id, issued_at, expires_at
		FROM sessions
		WHERE token = $1 AND expires_at > NOW()
	`, token).Scan(&sess.Token, &sess.UserID, &sess.IssuedAt, &sess.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &sess, nil
}

// DeleteSession removes a session by token. No error if the row does not exist.
func DeleteSession(db *sql.DB, token string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE token = $1`, token)
	return err
}
