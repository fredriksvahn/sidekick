package main

import (
	"fmt"
	"os"

	"github.com/earlysvahn/sidekick/cmd/sidekick/commands"
	"github.com/earlysvahn/sidekick/internal/agent"
	"github.com/earlysvahn/sidekick/internal/db"
	"github.com/earlysvahn/sidekick/internal/server"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// Initialize agent repository FIRST (must happen before any agent operations)
	if err := initAgentRepository(); err != nil {
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
		case "--serve", "-serve":
			if err := runServer(); err != nil {
				fmt.Fprintln(os.Stderr, "[server error]", err)
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

// initAgentRepository initializes the agent database and sets the global repository.
// This MUST be called early on startup before any agent operations.
func initAgentRepository() error {
	database, err := db.OpenSQLite()
	if err != nil {
		return fmt.Errorf("open agent database: %w", err)
	}

	// Migrate hardcoded agents to database (idempotent - only inserts if not exists)
	if err := agent.MigrateHardcodedAgents(database); err != nil {
		database.Close()
		return fmt.Errorf("migrate agents: %w", err)
	}

	// Set global repository for agent loading
	repo := agent.NewRepository(database)
	agent.SetRepository(repo)

	return nil
}

// runServer starts the HTTP server
func runServer() error {
	historyStore, err := commands.CreateHistoryStore("file")
	if err != nil {
		return fmt.Errorf("storage error: %w", err)
	}
	return server.Run("", historyStore)
}
