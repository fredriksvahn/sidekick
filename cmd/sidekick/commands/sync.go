package commands

import (
	"fmt"

	"github.com/earlysvahn/sidekick/internal/db"
	"github.com/earlysvahn/sidekick/internal/store"
	"github.com/earlysvahn/sidekick/internal/sync"
)

// RunSyncCommand handles the 'sync' subcommand
func RunSyncCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("sync requires arguments: 'push|pull' for contexts or 'agents push|pull' for agents")
	}

	// Check if first arg is "agents"
	if args[0] == "agents" {
		if len(args) < 2 {
			return fmt.Errorf("sync agents requires a direction: push or pull")
		}
		return RunAgentSyncCommand(args[1])
	}

	// Otherwise, assume context sync
	direction := args[0]
	if direction != "push" && direction != "pull" {
		return fmt.Errorf("sync direction must be 'push' or 'pull', got: %s", direction)
	}

	// Create both storage backends
	sqliteStore, err := store.NewSQLiteStore(db.SQLitePath())
	if err != nil {
		return fmt.Errorf("failed to open SQLite: %w", err)
	}

	dsn, ok := db.PostgresDSN()
	if !ok {
		return fmt.Errorf("SIDEKICK_POSTGRES_DSN environment variable is required for sync")
	}
	pgStore, err := store.NewPostgresStore(dsn)
	if err != nil {
		return fmt.Errorf("failed to open Postgres: %w", err)
	}
	defer pgStore.Close()

	// Wrap PostgresStore for CLI compatibility
	postgresStore := store.NewCLIPostgresAdapter(pgStore)

	var source, target store.HistoryStore
	var sourceName, targetName string

	if direction == "push" {
		source = sqliteStore
		target = postgresStore
		sourceName = "SQLite"
		targetName = "Postgres"
	} else {
		source = postgresStore
		target = sqliteStore
		sourceName = "Postgres"
		targetName = "SQLite"
	}

	fmt.Printf("Syncing from %s to %s...\n\n", sourceName, targetName)
	result, err := sync.SyncContexts(source, target, sourceName, targetName)
	if err != nil && result != nil {
		// Print partial results even on error
		fmt.Printf("Sync completed with errors!\n")
		fmt.Printf("  Contexts synced: %d\n", result.ContextsSynced)
		fmt.Printf("  Messages inserted: %d\n", result.MessagesInserted)
		fmt.Printf("  Errors: %d\n\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Printf("  - %s\n", e)
		}
		return err
	}
	if result != nil {
		fmt.Printf("Sync complete!\n")
		fmt.Printf("  Contexts synced: %d\n", result.ContextsSynced)
		fmt.Printf("  Messages inserted: %d\n", result.MessagesInserted)
	}
	return nil
}

// RunAgentSyncCommand handles agent sync between SQLite and Postgres
func RunAgentSyncCommand(direction string) error {
	if direction != "push" && direction != "pull" {
		return fmt.Errorf("sync agents direction must be 'push' or 'pull', got: %s", direction)
	}

	// Open SQLite database
	sqliteDB, err := db.OpenSQLite()
	if err != nil {
		return fmt.Errorf("failed to open SQLite: %w", err)
	}
	defer sqliteDB.Close()

	// Open Postgres database
	postgresDB, err := db.OpenPostgres()
	if err != nil {
		return fmt.Errorf("failed to open Postgres: %w", err)
	}
	defer postgresDB.Close()

	// Perform sync
	if direction == "push" {
		fmt.Println("Syncing agents from SQLite to Postgres...")
	} else {
		fmt.Println("Pulling agents from Postgres to SQLite...")
	}

	if err := sync.SyncAgents(sqliteDB, postgresDB, direction); err != nil {
		return fmt.Errorf("agent sync failed: %w", err)
	}

	fmt.Println("Agent sync complete!")
	return nil
}
