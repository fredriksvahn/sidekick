package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/earlysvahn/sidekick/internal/agent"
	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/executor"
	"github.com/earlysvahn/sidekick/internal/server"
	"github.com/earlysvahn/sidekick/internal/store"
	"github.com/earlysvahn/sidekick/internal/tui"
)

func main() {
	// Check for chat subcommand
	if len(os.Args) > 1 && os.Args[1] == "chat" {
		if err := runChatCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// Check for contexts subcommand
	if len(os.Args) > 1 && os.Args[1] == "contexts" {
		if err := runContextsCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// Check for history subcommand
	if len(os.Args) > 1 && os.Args[1] == "history" {
		if err := runHistoryCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// Check for tui subcommand
	if len(os.Args) > 1 && os.Args[1] == "tui" {
		if err := runTUICommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// Check for sync subcommand
	if len(os.Args) > 1 && os.Args[1] == "sync" {
		if err := runSyncCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// One-shot mode
	var modelOverride string
	var contextName string
	var historyLimit int
	var systemPrompt string
	var serve bool
	var localOnly bool
	var remoteOnly bool
	var quiet bool
	var storageBackend string
	var agentProfile string

	flag.StringVar(&modelOverride, "model", "", "force a specific Ollama model")
	flag.StringVar(&contextName, "context", "misc", "context name")
	flag.StringVar(&contextName, "ctx", "misc", "context name (alias for -context)")
	flag.IntVar(&historyLimit, "history", 4, "number of prior messages to include")
	flag.StringVar(&systemPrompt, "system", "", "system prompt for this context")
	flag.StringVar(&agentProfile, "agent", "", "agent profile (code, golang-dev, etc)")
	flag.BoolVar(&serve, "serve", false, "run HTTP server")
	flag.BoolVar(&localOnly, "local", false, "force local Ollama execution")
	flag.BoolVar(&remoteOnly, "remote", false, "force remote execution")
	flag.BoolVar(&quiet, "quiet", false, "suppress non-error logs")
	flag.StringVar(&storageBackend, "storage", "file", "storage backend (file|sqlite)")
	flag.Parse()

	logf := func(msg string) {
		if quiet {
			return
		}
		fmt.Fprintf(os.Stderr, "[sidekick] %s\n", msg)
	}

	// Apply agent profile if specified
	if agentProfile != "" {
		profile := agent.GetProfile(agentProfile)
		if profile == nil {
			fmt.Fprintf(os.Stderr, "Unknown agent profile: %s\n", agentProfile)
			fmt.Fprintf(os.Stderr, "Available profiles: %s\n", strings.Join(agent.ListProfiles(), ", "))
			os.Exit(1)
		}
		// Apply profile defaults only if not explicitly overridden
		if modelOverride == "" {
			if localOnly {
				modelOverride = profile.LocalModel
			} else if remoteOnly {
				modelOverride = profile.RemoteModel
			}
			// If neither local nor remote is forced, leave modelOverride empty
			// and let executeWithFallback use profile models
		}
		if systemPrompt == "" {
			systemPrompt = profile.SystemPrompt
		}
	}

	if serve {
		// Instantiate storage for server
		historyStore, err := createHistoryStore(storageBackend)
		if err != nil {
			fmt.Fprintln(os.Stderr, "[storage error]", err)
			os.Exit(1)
		}
		if err := server.Run(modelOverride, historyStore); err != nil {
			fmt.Fprintln(os.Stderr, "[server error]", err)
			os.Exit(1)
		}
		return
	}

	if flag.NArg() == 0 {
		fmt.Println("Usage: sidekick [--model MODEL] \"your prompt\"")
		fmt.Println("   or: sidekick chat [--context NAME]")
		fmt.Println("   or: sidekick tui [--context NAME]")
		fmt.Println("   or: sidekick contexts [--storage BACKEND]")
		fmt.Println("   or: sidekick history --context NAME [--storage BACKEND]")
		fmt.Println("   or: sidekick sync push|pull")
		os.Exit(1)
	}
	rawPrompt := strings.Join(flag.Args(), " ")

	// Instantiate storage
	historyStore, err := createHistoryStore(storageBackend)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[storage error]", err)
		os.Exit(1)
	}

	ctxHist, err := historyStore.LoadContext(contextName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[history error]", err)
		os.Exit(1)
	}
	if systemPrompt != "" {
		ctxHist.System = systemPrompt
		if err := historyStore.SaveContext(contextName, ctxHist); err != nil {
			fmt.Fprintln(os.Stderr, "[history error]", err)
			os.Exit(1)
		}
	}

	system := ctxHist.System
	history := ctxHist.Messages
	messages := buildMessages(system, history, historyLimit, rawPrompt)

	remoteURL, err := config.LoadRemote()
	if err != nil {
		fmt.Fprintln(os.Stderr, "[config error]", err)
		os.Exit(1)
	}

	// Get agent profile for execution
	var profile *agent.AgentProfile
	if agentProfile != "" {
		profile = agent.GetProfile(agentProfile)
	}

	reply, err := executeWithFallback(modelOverride, remoteURL, localOnly, remoteOnly, messages, logf, profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[executor error]", err)
		os.Exit(1)
	}
	fmt.Println(reply)

	now := time.Now().UTC()
	_ = historyStore.Append(contextName, store.Message{Role: "user", Content: rawPrompt, Time: now})
	_ = historyStore.Append(contextName, store.Message{Role: "assistant", Content: reply, Time: now})
}

// createHistoryStore instantiates the appropriate storage backend
func createHistoryStore(backend string) (store.HistoryStore, error) {
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

// buildMessages constructs the message array for LLM execution
func buildMessages(system string, history []store.Message, historyLimit int, userPrompt string) []chat.Message {
	// Apply history limit
	limitedHistory := history
	if historyLimit > 0 && len(limitedHistory) > historyLimit {
		limitedHistory = limitedHistory[len(limitedHistory)-historyLimit:]
	}

	// Build message array
	messages := make([]chat.Message, 0, len(limitedHistory)+2)
	if system != "" {
		messages = append(messages, chat.Message{Role: "system", Content: system})
	}
	for _, m := range limitedHistory {
		messages = append(messages, chat.Message{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, chat.Message{Role: "user", Content: userPrompt})
	return messages
}

// runChatCommand handles the 'chat' subcommand
func runChatCommand(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)

	var modelOverride string
	var contextName string
	var historyLimit int
	var systemPrompt string
	var localOnly bool
	var remoteOnly bool
	var quiet bool
	var storageBackend string
	var agentProfile string

	fs.StringVar(&modelOverride, "model", "", "force a specific Ollama model")
	fs.StringVar(&contextName, "context", "misc", "context name")
	fs.StringVar(&contextName, "ctx", "misc", "context name (alias for -context)")
	fs.IntVar(&historyLimit, "history", 4, "number of prior messages to include")
	fs.StringVar(&systemPrompt, "system", "", "system prompt for this context")
	fs.StringVar(&agentProfile, "agent", "", "agent profile (code, golang-dev, etc)")
	fs.BoolVar(&localOnly, "local", false, "force local Ollama execution")
	fs.BoolVar(&remoteOnly, "remote", false, "force remote execution")
	fs.BoolVar(&quiet, "quiet", false, "suppress non-error logs")
	fs.StringVar(&storageBackend, "storage", "file", "storage backend (file|sqlite)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	logf := func(msg string) {
		if quiet {
			return
		}
		fmt.Fprintf(os.Stderr, "[sidekick] %s\n", msg)
	}

	// Apply agent profile if specified
	var profile *agent.AgentProfile
	if agentProfile != "" {
		profile = agent.GetProfile(agentProfile)
		if profile == nil {
			return fmt.Errorf("unknown agent profile: %s\nAvailable profiles: %s", agentProfile, strings.Join(agent.ListProfiles(), ", "))
		}
		// Apply profile defaults
		if modelOverride == "" {
			if localOnly {
				modelOverride = profile.LocalModel
			} else if remoteOnly {
				modelOverride = profile.RemoteModel
			}
		}
		if systemPrompt == "" {
			systemPrompt = profile.SystemPrompt
		}
	}

	// Instantiate storage
	historyStore, err := createHistoryStore(storageBackend)
	if err != nil {
		return fmt.Errorf("storage error: %w", err)
	}

	// Load remote URL
	remoteURL, err := config.LoadRemote()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	// Load context
	ctxHist, err := historyStore.LoadContext(contextName)
	if err != nil {
		return fmt.Errorf("load context: %w", err)
	}

	// Apply system prompt override if provided
	if systemPrompt != "" {
		ctxHist.System = systemPrompt
		if err := historyStore.SaveContext(contextName, ctxHist); err != nil {
			return fmt.Errorf("save system prompt: %w", err)
		}
	}

	return runChatMode(
		contextName,
		ctxHist.System,
		ctxHist.Messages,
		historyStore,
		historyLimit,
		modelOverride,
		remoteURL,
		localOnly,
		remoteOnly,
		logf,
		profile,
	)
}

// runChatMode runs the interactive REPL
func runChatMode(
	contextName string,
	system string,
	initialHistory []store.Message,
	historyStore store.HistoryStore,
	historyLimit int,
	modelOverride string,
	remoteURL string,
	localOnly bool,
	remoteOnly bool,
	logf func(string),
	profile *agent.AgentProfile,
) error {
	// Setup signal handling with context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Print welcome message
	fmt.Fprintf(os.Stderr, "Chat mode (context: %s)\n", contextName)
	if system != "" {
		fmt.Fprintf(os.Stderr, "System: %s\n", system)
	}
	fmt.Fprintln(os.Stderr, "Press Ctrl+C or Ctrl+D to exit.\n")

	reader := bufio.NewReader(os.Stdin)
	history := initialHistory

	for {
		// Check if context is cancelled before prompting
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\nExiting chat mode...")
			return nil
		default:
		}

		fmt.Fprint(os.Stderr, "> ")

		// Read input in goroutine to allow cancellation
		inputChan := make(chan string, 1)
		errChan := make(chan error, 1)

		go func() {
			line, err := reader.ReadString('\n')
			if err != nil {
				errChan <- err
			} else {
				inputChan <- line
			}
		}()

		// Wait for input or cancellation
		var input string
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\nExiting chat mode...")
			return nil
		case err := <-errChan:
			if err == io.EOF {
				fmt.Fprintln(os.Stderr, "\nExiting chat mode...")
				return nil
			}
			return fmt.Errorf("read input: %w", err)
		case line := <-inputChan:
			input = strings.TrimSpace(line)
		}

		// Skip empty input
		if input == "" {
			continue
		}

		// Build messages
		messages := buildMessages(system, history, historyLimit, input)

		// Execute
		reply, err := executeWithFallback(modelOverride, remoteURL, localOnly, remoteOnly, messages, logf, profile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] %v\n\n", err)
			continue
		}

		// Print response
		fmt.Println(reply)
		fmt.Println()

		// Persist messages
		now := time.Now().UTC()
		userMsg := store.Message{Role: "user", Content: input, Time: now}
		assistantMsg := store.Message{Role: "assistant", Content: reply, Time: now}

		if err := historyStore.Append(contextName, userMsg); err != nil {
			fmt.Fprintf(os.Stderr, "[warning] failed to save user message: %v\n", err)
		}
		if err := historyStore.Append(contextName, assistantMsg); err != nil {
			fmt.Fprintf(os.Stderr, "[warning] failed to save assistant message: %v\n", err)
		}

		// Update in-memory history
		history = append(history, userMsg, assistantMsg)
	}
}

func executeWithFallback(modelOverride, remoteURL string, localOnly, remoteOnly bool, messages []chat.Message, logf func(string), profile *agent.AgentProfile) (string, error) {
	// Determine local model to use
	localModel := modelOverride
	if profile != nil && modelOverride == "" {
		localModel = profile.LocalModel
	}

	if localOnly {
		logf("execution path: local ollama (forced)")
		return (&executor.OllamaExecutor{Model: localModel, Log: nil}).Execute(messages)
	}
	if remoteURL == "" {
		if remoteOnly {
			return "", fmt.Errorf("remote execution requested but no remote is configured")
		}
		logf("execution path: local ollama (no remote configured)")
		return (&executor.OllamaExecutor{Model: localModel, Log: nil}).Execute(messages)
	}

	httpExec := executor.NewHTTPExecutor(remoteURL, 30*time.Second, nil)

	// If using profile, pass the remote model to HTTP executor
	if profile != nil && modelOverride == "" {
		// For now, HTTPExecutor doesn't support model selection
		// This is handled by the remote server's default
	}

	ok, healthErr := httpExec.Available()
	if ok {
		reply, err := httpExec.Execute(messages)
		if err == nil {
			return reply, nil
		}
		if remoteOnly {
			return "", err
		}
		logf("using local")
	} else if healthErr != nil {
		if remoteOnly {
			return "", fmt.Errorf("remote execution requested but health check failed: %v", healthErr)
		}
		logf("using local")
	} else if remoteOnly {
		return "", fmt.Errorf("remote execution requested but health check failed")
	}

	return (&executor.OllamaExecutor{Model: localModel, Log: nil}).Execute(messages)
}

// runContextsCommand handles the 'contexts' subcommand
func runContextsCommand(args []string) error {
	fs := flag.NewFlagSet("contexts", flag.ExitOnError)
	var storageBackend string
	fs.StringVar(&storageBackend, "storage", "file", "storage backend (file|sqlite)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Instantiate storage
	historyStore, err := createHistoryStore(storageBackend)
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

// runHistoryCommand handles the 'history' subcommand
func runHistoryCommand(args []string) error {
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
	historyStore, err := createHistoryStore(storageBackend)
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

// runTUICommand handles the 'tui' subcommand
func runTUICommand(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	var modelOverride string
	var contextName string
	var historyLimit int
	var systemPrompt string
	var localOnly bool
	var remoteOnly bool
	var storageBackend string
	var agentProfile string

	fs.StringVar(&modelOverride, "model", "", "force a specific Ollama model")
	fs.StringVar(&contextName, "context", "misc", "context name")
	fs.StringVar(&contextName, "ctx", "misc", "context name (alias for -context)")
	fs.IntVar(&historyLimit, "history", 4, "number of prior messages to include")
	fs.StringVar(&systemPrompt, "system", "", "system prompt for this context")
	fs.StringVar(&agentProfile, "agent", "", "agent profile (code, golang-dev, etc)")
	fs.BoolVar(&localOnly, "local", false, "force local Ollama execution")
	fs.BoolVar(&remoteOnly, "remote", false, "force remote execution")
	fs.StringVar(&storageBackend, "storage", "file", "storage backend (file|sqlite)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Apply agent profile if specified
	var profile *agent.AgentProfile
	if agentProfile != "" {
		profile = agent.GetProfile(agentProfile)
		if profile == nil {
			return fmt.Errorf("unknown agent profile: %s\nAvailable profiles: %s", agentProfile, strings.Join(agent.ListProfiles(), ", "))
		}
		// Apply profile defaults
		if modelOverride == "" {
			if localOnly {
				modelOverride = profile.LocalModel
			} else if remoteOnly {
				modelOverride = profile.RemoteModel
			}
		}
		if systemPrompt == "" {
			systemPrompt = profile.SystemPrompt
		}
	}

	// Instantiate storage
	historyStore, err := createHistoryStore(storageBackend)
	if err != nil {
		return fmt.Errorf("storage error: %w", err)
	}

	// Load remote URL
	remoteURL, err := config.LoadRemote()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	// Load context
	ctxHist, err := historyStore.LoadContext(contextName)
	if err != nil {
		return fmt.Errorf("load context: %w", err)
	}

	// Apply system prompt override if provided
	if systemPrompt != "" {
		ctxHist.System = systemPrompt
		if err := historyStore.SaveContext(contextName, ctxHist); err != nil {
			return fmt.Errorf("save system prompt: %w", err)
		}
	}

	// Create execute function that wraps executeWithFallback
	logf := func(msg string) {
		// Silent logging for TUI
	}
	executeFn := func(messages []chat.Message) (string, error) {
		return executeWithFallback(modelOverride, remoteURL, localOnly, remoteOnly, messages, logf, profile)
	}

	// Run TUI
	return tui.Run(tui.Config{
		ContextName:   contextName,
		SystemPrompt:  ctxHist.System,
		History:       ctxHist.Messages,
		HistoryStore:  historyStore,
		HistoryLimit:  historyLimit,
		ModelOverride: modelOverride,
		RemoteURL:     remoteURL,
		LocalOnly:     localOnly,
		RemoteOnly:    remoteOnly,
		ExecuteFn:     executeFn,
	})
}

// runSyncCommand handles the 'sync' subcommand
func runSyncCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("sync requires a direction: push or pull")
	}

	direction := args[0]
	if direction != "push" && direction != "pull" {
		return fmt.Errorf("sync direction must be 'push' or 'pull', got: %s", direction)
	}

	// Create both storage backends
	dbPath := filepath.Join(config.Dir(), "sidekick.db")
	sqliteStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open SQLite: %w", err)
	}

	dsn := os.Getenv("SIDEKICK_POSTGRES_DSN")
	if dsn == "" {
		return fmt.Errorf("SIDEKICK_POSTGRES_DSN environment variable is required for sync")
	}
	postgresStore, err := store.NewPostgresStore(dsn)
	if err != nil {
		return fmt.Errorf("failed to open Postgres: %w", err)
	}

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

	return performSync(source, target, sourceName, targetName)
}

// performSync syncs contexts and messages from source to target
func performSync(source, target store.HistoryStore, sourceName, targetName string) error {
	fmt.Printf("Syncing from %s to %s...\n\n", sourceName, targetName)

	// Load all contexts from source
	sourceContexts, err := source.ListContexts()
	if err != nil {
		return fmt.Errorf("failed to list source contexts: %w", err)
	}

	contextsSynced := 0
	messagesInserted := 0
	var errors []string

	for _, ctxInfo := range sourceContexts {
		// Load full context from source
		sourceCtx, err := source.LoadContext(ctxInfo.Name)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to load context %s: %v", ctxInfo.Name, err))
			continue
		}

		// Upsert context in target (this will create if not exists, or update system prompt)
		if err := target.SaveContext(ctxInfo.Name, sourceCtx); err != nil {
			errors = append(errors, fmt.Sprintf("failed to save context %s: %v", ctxInfo.Name, err))
			continue
		}

		// Load target context to see what messages already exist
		targetCtx, err := target.LoadContext(ctxInfo.Name)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to load target context %s: %v", ctxInfo.Name, err))
			continue
		}

		// Find messages that don't exist in target
		newMessages := findNewMessages(sourceCtx.Messages, targetCtx.Messages)

		// Insert new messages
		for _, msg := range newMessages {
			if err := target.Append(ctxInfo.Name, msg); err != nil {
				errors = append(errors, fmt.Sprintf("failed to append message to %s: %v", ctxInfo.Name, err))
				continue
			}
			messagesInserted++
		}

		contextsSynced++
	}

	// Print summary
	fmt.Printf("Sync complete!\n")
	fmt.Printf("  Contexts synced: %d\n", contextsSynced)
	fmt.Printf("  Messages inserted: %d\n", messagesInserted)

	if len(errors) > 0 {
		fmt.Printf("  Errors: %d\n\n", len(errors))
		for _, e := range errors {
			fmt.Printf("  - %s\n", e)
		}
		return fmt.Errorf("sync completed with errors")
	}

	return nil
}

// findNewMessages returns messages from source that don't exist in target
// Messages are matched by: role, content, and created_at timestamp
func findNewMessages(sourceMessages, targetMessages []store.Message) []store.Message {
	// Build a set of existing messages for fast lookup
	existing := make(map[string]bool)
	for _, msg := range targetMessages {
		key := messageKey(msg)
		existing[key] = true
	}

	// Find messages that don't exist in target
	var newMessages []store.Message
	for _, msg := range sourceMessages {
		key := messageKey(msg)
		if !existing[key] {
			newMessages = append(newMessages, msg)
		}
	}

	return newMessages
}

// messageKey generates a unique key for a message based on role, content, and timestamp
func messageKey(msg store.Message) string {
	return fmt.Sprintf("%s|%s|%s", msg.Role, msg.Content, msg.Time.Format(time.RFC3339Nano))
}
