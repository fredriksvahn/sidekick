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

	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/executor"
	"github.com/earlysvahn/sidekick/internal/server"
	"github.com/earlysvahn/sidekick/internal/store"
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

	flag.StringVar(&modelOverride, "model", "", "force a specific Ollama model")
	flag.StringVar(&contextName, "context", "misc", "context name")
	flag.IntVar(&historyLimit, "history", 4, "number of prior messages to include")
	flag.StringVar(&systemPrompt, "system", "", "system prompt for this context")
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

	if serve {
		if err := server.Run(modelOverride); err != nil {
			fmt.Fprintln(os.Stderr, "[server error]", err)
			os.Exit(1)
		}
		return
	}

	if flag.NArg() == 0 {
		fmt.Println("Usage: sidekick [--model MODEL] \"your prompt\"")
		fmt.Println("   or: sidekick chat [--context NAME]")
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

	reply, err := executeWithFallback(modelOverride, remoteURL, localOnly, remoteOnly, messages, logf)
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
	default:
		return nil, fmt.Errorf("unknown storage backend: %s (must be 'file' or 'sqlite')", backend)
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

	fs.StringVar(&modelOverride, "model", "", "force a specific Ollama model")
	fs.StringVar(&contextName, "context", "misc", "context name")
	fs.IntVar(&historyLimit, "history", 4, "number of prior messages to include")
	fs.StringVar(&systemPrompt, "system", "", "system prompt for this context")
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
		reply, err := executeWithFallback(modelOverride, remoteURL, localOnly, remoteOnly, messages, logf)
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

func executeWithFallback(modelOverride, remoteURL string, localOnly, remoteOnly bool, messages []chat.Message, logf func(string)) (string, error) {
	if localOnly {
		logf("execution path: local ollama (forced)")
		return (&executor.OllamaExecutor{Model: modelOverride, Log: nil}).Execute(messages)
	}
	if remoteURL == "" {
		if remoteOnly {
			return "", fmt.Errorf("remote execution requested but no remote is configured")
		}
		logf("execution path: local ollama (no remote configured)")
		return (&executor.OllamaExecutor{Model: modelOverride, Log: nil}).Execute(messages)
	}

	httpExec := executor.NewHTTPExecutor(remoteURL, 30*time.Second, nil)
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

	return (&executor.OllamaExecutor{Model: modelOverride, Log: nil}).Execute(messages)
}
