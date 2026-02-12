package auth

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// User represents a row in the users table.
type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	LastLoginAt  *time.Time
}

// CheckPassword returns true if password matches the stored bcrypt hash.
func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}

// CreateUser inserts a new user with a bcrypt-hashed password.
// Returns the full user record as persisted (id and created_at set by the DB).
func CreateUser(db *sql.DB, email, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	var user User
	var lastLogin sql.NullTime
	err = db.QueryRow(`
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		RETURNING id, email, password_hash, created_at, last_login_at
	`, email, string(hash)).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt, &lastLogin)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	if lastLogin.Valid {
		user.LastLoginAt = &lastLogin.Time
	}
	return &user, nil
}

// GetUserByEmail loads a user by email. Returns (nil, nil) if no row exists.
func GetUserByEmail(db *sql.DB, email string) (*User, error) {
	var user User
	var lastLogin sql.NullTime
	err := db.QueryRow(`
		SELECT id, email, password_hash, created_at, last_login_at
		FROM users WHERE email = $1
	`, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt, &lastLogin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if lastLogin.Valid {
		user.LastLoginAt = &lastLogin.Time
	}
	return &user, nil
}

// MarkLogin updates last_login_at to now for the given user.
func MarkLogin(db *sql.DB, userID uuid.UUID) error {
	_, err := db.Exec(`UPDATE users SET last_login_at = NOW() WHERE id = $1`, userID)
	return err
}

// EnsureBootstrapUser creates the initial user from SIDEKICK_AUTH_EMAIL and
// SIDEKICK_AUTH_PASSWORD if both are set and no user with that email exists yet.
// This is the single-user bootstrap path. Idempotent on repeated calls.
func EnsureBootstrapUser(db *sql.DB) error {
	email := os.Getenv("SIDEKICK_AUTH_EMAIL")
	password := os.Getenv("SIDEKICK_AUTH_PASSWORD")
	if email == "" || password == "" {
		return nil // bootstrap env vars not set; skip
	}

	existing, err := GetUserByEmail(db, email)
	if err != nil {
		return fmt.Errorf("check bootstrap user: %w", err)
	}
	if existing != nil {
		return nil // already exists
	}

	if _, err := CreateUser(db, email, password); err != nil {
		return fmt.Errorf("create bootstrap user: %w", err)
	}
	return nil
}
