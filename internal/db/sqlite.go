package db

import (
	"database/sql"
	"os"
	"path/filepath"

	"github.com/earlysvahn/sidekick/internal/config"
	_ "modernc.org/sqlite"
)

// SQLitePath returns the path to the SQLite database file.
func SQLitePath() string {
	return filepath.Join(config.Dir(), "sidekick.db")
}

// OpenSQLite opens a connection to the SQLite database.
func OpenSQLite() (*sql.DB, error) {
	// Ensure directory exists
	if err := os.MkdirAll(config.Dir(), 0755); err != nil {
		return nil, err
	}
	return sql.Open("sqlite", SQLitePath())
}
