package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/store"
)

// RunContextsCommand handles the 'contexts' subcommand
func RunContextsCommand(args []string) error {
	fs := flag.NewFlagSet("contexts", flag.ExitOnError)
	var storageBackend string
	fs.StringVar(&storageBackend, "storage", "file", "storage backend (file|sqlite)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Instantiate storage
	historyStore, err := CreateHistoryStore(storageBackend)
	if err != nil {
		return fmt.Errorf("storage error: %w", err)
	}

	// List contexts
	contexts, err := historyStore.ListContexts()
	if err != nil {
		return fmt.Errorf("list contexts: %w", err)
	}

	// Print header
	fmt.Printf("%-12s  %-10s  %s\n", "NAME", "MESSAGES", "LAST_USED")

	// Print each context
	for _, ctx := range contexts {
		lastUsed := "-"
		if !ctx.LastUsed.IsZero() {
			lastUsed = ctx.LastUsed.Format("2006-01-02 15:04")
		}
		fmt.Printf("%-12s  %-10d  %s\n", ctx.Name, ctx.MessageCount, lastUsed)
	}

	return nil
}

// CreateHistoryStore instantiates the appropriate storage backend
func CreateHistoryStore(backend string) (store.HistoryStore, error) {
	switch backend {
	case "file":
		return store.NewFileStore(), nil
	case "sqlite":
		dbPath := filepath.Join(config.Dir(), "sidekick.db")
		return store.NewSQLiteStore(dbPath)
	case "postgres":
		dsn := os.Getenv("SIDEKICK_POSTGRES_DSN")
		if dsn == "" {
			return nil, fmt.Errorf("SIDEKICK_POSTGRES_DSN environment variable is required for postgres storage")
		}
		return store.NewPostgresStore(dsn)
	default:
		return nil, fmt.Errorf("unknown storage backend: %s (must be 'file', 'sqlite', or 'postgres')", backend)
	}
}
