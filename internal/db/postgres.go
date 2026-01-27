package db

import (
	"database/sql"
	"os"

	_ "github.com/lib/pq"
)

// PostgresDSN returns the Postgres DSN from environment and whether it was set.
func PostgresDSN() (string, bool) {
	dsn := os.Getenv("SIDEKICK_POSTGRES_DSN")
	return dsn, dsn != ""
}

// OpenPostgres opens a connection to the Postgres database.
// Returns an error if SIDEKICK_POSTGRES_DSN is not set.
func OpenPostgres() (*sql.DB, error) {
	dsn, ok := PostgresDSN()
	if !ok {
		return nil, &PostgresNotConfiguredError{}
	}
	return sql.Open("postgres", dsn)
}

// PostgresNotConfiguredError is returned when Postgres DSN is not configured.
type PostgresNotConfiguredError struct{}

func (e *PostgresNotConfiguredError) Error() string {
	return "SIDEKICK_POSTGRES_DSN environment variable is not set"
}
