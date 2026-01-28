package main

import (
	"fmt"
	"os"

	"github.com/earlysvahn/sidekick/cmd/sidekick/commands"
	"github.com/earlysvahn/sidekick/internal/agent"
	"github.com/earlysvahn/sidekick/internal/db"
	"github.com/earlysvahn/sidekick/internal/server"
	"github.com/earlysvahn/sidekick/internal/store"
)

func main() {
	// Check if running in server mode FIRST
	isServerMode := len(os.Args) > 1 && (os.Args[1] == "--serve" || os.Args[1] == "-serve")

	if isServerMode {
		// API mode: use Postgres for everything
		if err := runServer(); err != nil {
			fmt.Fprintln(os.Stderr, "[server error]", err)
			os.Exit(1)
		}
		return
	}

	// CLI mode: use SQLite for agents (offline-first)
	if err := initCLIAgentRepository(); err != nil {
		fmt.Fprintf(os.Stderr, "[agent init error] %v\n", err)
		os.Exit(1)
	}

	// Route to appropriate command
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "chat":
			if err := commands.RunChatCommand(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "tui":
			if err := commands.RunTUICommand(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "contexts":
			if err := commands.RunContextsCommand(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "history":
			if err := commands.RunHistoryCommand(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "sync":
			if err := commands.RunSyncCommand(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "agents":
			if err := commands.RunAgentsCommand(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}

	// One-shot mode or help
	if len(os.Args) == 1 || os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help" {
		commands.PrintUsage()
		os.Exit(0)
	}

	// Execute one-shot query
	if err := commands.RunOneShot(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// initCLIAgentRepository initializes SQLite-based agent repository for CLI mode.
// CLI uses SQLite for offline-first operation.
func initCLIAgentRepository() error {
	database, err := db.OpenSQLite()
	if err != nil {
		return fmt.Errorf("open agent database: %w", err)
	}

	// Create SQLite repository
	repo := agent.NewRepository(database)

	// Migrate hardcoded agents to database (idempotent - only inserts if not exists)
	if err := agent.MigrateHardcodedAgents(repo); err != nil {
		database.Close()
		return fmt.Errorf("migrate agents: %w", err)
	}

	// Set global repository for agent loading
	agent.SetRepository(repo)

	return nil
}

// runServer starts the HTTP server with Postgres storage.
// API mode ALWAYS uses Postgres. Fails fast if not configured.
func runServer() error {
	// Require SIDEKICK_POSTGRES_DSN for API mode
	dsn := os.Getenv("SIDEKICK_POSTGRES_DSN")
	if dsn == "" {
		return fmt.Errorf("SIDEKICK_POSTGRES_DSN environment variable is required for API mode")
	}

	// Open Postgres connection
	postgresDB, err := db.OpenPostgres()
	if err != nil {
		return fmt.Errorf("failed to connect to Postgres: %w", err)
	}

	// Initialize Postgres history store (contexts/messages)
	historyStore, err := store.NewPostgresStore(dsn)
	if err != nil {
		postgresDB.Close()
		return fmt.Errorf("failed to initialize history store: %w", err)
	}

	// Initialize Postgres agent repository
	agentRepo := agent.NewPostgresRepository(postgresDB)

	// Migrate hardcoded agents to Postgres (idempotent)
	if err := agent.MigrateHardcodedAgents(agentRepo); err != nil {
		historyStore.Close()
		postgresDB.Close()
		return fmt.Errorf("failed to migrate agents: %w", err)
	}

	// Set global agent repository
	agent.SetRepository(agentRepo)

	fmt.Fprintf(os.Stderr, "[sidekick] using Postgres for storage\n")

	return server.Run("", historyStore, agentRepo)
}
