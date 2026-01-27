package db

import (
	"database/sql"
	"path/filepath"

	"github.com/earlysvahn/sidekick/internal/config"
	_ "github.com/mattn/go-sqlite3"
)

// SQLitePath returns the path to the SQLite database file.
func SQLitePath() string {
	return filepath.Join(config.Dir(), "sidekick.db")
}

// OpenSQLite opens a connection to the SQLite database.
func OpenSQLite() (*sql.DB, error) {
	return sql.Open("sqlite3", SQLitePath())
}
