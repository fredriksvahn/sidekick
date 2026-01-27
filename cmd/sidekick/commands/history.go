package commands

import (
	"flag"
	"fmt"
)

// RunHistoryCommand handles the 'history' subcommand
func RunHistoryCommand(args []string) error {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	var contextName string
	var storageBackend string
	fs.StringVar(&contextName, "context", "", "context name (required)")
	fs.StringVar(&contextName, "ctx", "", "context name (alias for -context)")
	fs.StringVar(&storageBackend, "storage", "file", "storage backend (file|sqlite)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validate required flag
	if contextName == "" {
		return fmt.Errorf("--context is required")
	}

	// Instantiate storage
	historyStore, err := CreateHistoryStore(storageBackend)
	if err != nil {
		return fmt.Errorf("storage error: %w", err)
	}

	// Load context
	ctxHist, err := historyStore.LoadContext(contextName)
	if err != nil {
		return fmt.Errorf("load context: %w", err)
	}

	// Check if context exists (empty context with no system prompt means it doesn't exist)
	if ctxHist.System == "" && len(ctxHist.Messages) == 0 {
		return fmt.Errorf("context '%s' does not exist", contextName)
	}

	// Print system prompt if present
	if ctxHist.System != "" {
		fmt.Printf("[system] %s\n", ctxHist.System)
	}

	// Print all messages in chronological order
	for _, msg := range ctxHist.Messages {
		fmt.Printf("[%s] %s\n", msg.Role, msg.Content)
	}

	return nil
}
