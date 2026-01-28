package db

import (
	"database/sql"
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
	return sql.Open("sqlite", SQLitePath())
}
