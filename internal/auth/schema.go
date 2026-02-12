package auth

import "database/sql"

// InitSchema creates the users and sessions tables if they do not exist.
// Safe to call on every startup (idempotent).
func InitSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id              TEXT       PRIMARY KEY,
			email           TEXT       UNIQUE NOT NULL,
			password_hash   TEXT       NOT NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_login_at   TIMESTAMPTZ
		);

		CREATE TABLE IF NOT EXISTS sessions (
			token      TEXT        PRIMARY KEY,
			user_id    TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			issued_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_sessions_user_id    ON sessions(user_id);
		CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
	`)
	return err
}
