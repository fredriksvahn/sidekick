package commands

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/earlysvahn/sidekick/internal/agent"
	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/cli"
	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/executor"
	"github.com/earlysvahn/sidekick/internal/store"
)

// RunChatCommand handles the 'chat' subcommand
func RunChatCommand(args []string) error {
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
	var verbosity int

	fs.StringVar(&modelOverride, "model", "", "force a specific Ollama model")
	fs.StringVar(&contextName, "context", "misc", "context|ctx: context name")
	fs.StringVar(&contextName, "ctx", "misc", "")
	fs.IntVar(&historyLimit, "history", 4, "history|h: number of prior messages to include")
	fs.IntVar(&historyLimit, "h", 4, "")
	fs.StringVar(&systemPrompt, "system", "", "system|sp: system prompt for this context")
	fs.StringVar(&systemPrompt, "sp", "", "")
	fs.StringVar(&agentProfile, "agent", "", "agent|a: agent profile (code, golang-dev, etc)")
	fs.StringVar(&agentProfile, "a", "", "")
	fs.BoolVar(&localOnly, "local", false, "force local Ollama execution")
	fs.BoolVar(&remoteOnly, "remote", false, "force remote execution")
	fs.BoolVar(&quiet, "quiet", false, "suppress non-error logs")
	fs.StringVar(&storageBackend, "storage", "file", "storage|s: storage backend (file|sqlite)")
	fs.StringVar(&storageBackend, "s", "file", "")
	fs.IntVar(&verbosity, "verbosity", -1, "verbosity|v: output verbosity (0=minimal, 1=concise, 2=normal, 3=verbose, 4=very verbose, 5=exhaustive)")
	fs.IntVar(&verbosity, "v", -1, "")

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
	historyStore, err := CreateHistoryStore(storageBackend)
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

	// Determine agent name for display
	currentAgent := "default"
	if agentProfile != "" {
		currentAgent = agentProfile
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
		currentAgent,
		verbosity,
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
	currentAgent string,
	verbosity int,
) error {
	// Setup signal handling with context
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// Note: Signal handling simplified - readline handles Ctrl+C/Ctrl+D

	defaultVerbosity := executor.DefaultVerbosity()
	requestedVerbosityValue := 0
	var requestedVerbosity *int
	if verbosity >= 0 {
		requestedVerbosityValue = verbosity
		requestedVerbosity = &requestedVerbosityValue
	}
	keywordStore := resolveKeywordLister(historyStore)

	// Print welcome message
	fmt.Fprintf(os.Stderr, "Chat mode (context: %s | agent: %s)\n", contextName, currentAgent)
	if system != "" {
		fmt.Fprintf(os.Stderr, "System: %s\n", system)
	}
	fmt.Fprintln(os.Stderr, "Press Ctrl+C or Ctrl+D to exit.\n")

	// Setup readline
	rl, err := readline.New(fmt.Sprintf("[%s] > ", currentAgent))
	if err != nil {
		return fmt.Errorf("readline init: %w", err)
	}
	defer rl.Close()

	history := initialHistory

	for {
		// Check if context is cancelled before prompting
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\nExiting chat mode...")
			return nil
		default:
		}

		// Update prompt with current agent
		rl.SetPrompt(fmt.Sprintf("[%s] > ", currentAgent))

		// Read input
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
				fmt.Fprintln(os.Stderr, "\nExiting chat mode...")
				return nil
			}
			return fmt.Errorf("read input: %w", err)
		}

		input := strings.TrimSpace(line)

		// Skip empty input
		if input == "" {
			continue
		}

		// Check for /agent command
		if strings.HasPrefix(input, "/agent ") {
			newAgent := strings.TrimSpace(strings.TrimPrefix(input, "/agent"))
			newProfile := agent.GetProfile(newAgent)
			if newProfile == nil {
				fmt.Fprintf(os.Stderr, "Unknown agent: %s\n", newAgent)
				fmt.Fprintf(os.Stderr, "Available agents: %s\n\n", strings.Join(agent.ListProfiles(), ", "))
				continue
			}
			// Switch agent
			currentAgent = newAgent
			profile = newProfile
			// Update system prompt if profile has one
			if newProfile.SystemPrompt != "" {
				system = newProfile.SystemPrompt
			}
			fmt.Fprintf(os.Stderr, "Switched to agent: %s\n\n", currentAgent)
			continue
		}

		// Check for /verbosity command
		if strings.HasPrefix(input, "/verbosity ") {
			levelStr := strings.TrimSpace(strings.TrimPrefix(input, "/verbosity"))
			var newLevel int
			_, err := fmt.Sscanf(levelStr, "%d", &newLevel)
			if err != nil || newLevel < 0 || newLevel > 5 {
				fmt.Fprintf(os.Stderr, "Invalid verbosity level. Use 0 (minimal), 1 (concise), 2 (normal), 3 (verbose), 4 (very verbose), or 5 (exhaustive)\n\n")
				continue
			}
			requestedVerbosityValue = newLevel
			requestedVerbosity = &requestedVerbosityValue
			fmt.Fprintf(os.Stderr, "Verbosity set to: %d\n\n", newLevel)
			continue
		}

		escalationResult, err := executor.ResolveVerbosity(ctx, requestedVerbosity, defaultVerbosity, currentAgent, input, keywordStore)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] %v\n\n", err)
			continue
		}
		effectiveVerbosity := escalationResult.EffectiveVerbosity
		if escalationResult.Warning != "" {
			fmt.Fprintf(os.Stderr, "[warning] %s\n", escalationResult.Warning)
		}

		// Inject system constraint for low verbosity modes
		systemWithConstraint := system
		if constraint := executor.SystemConstraint(effectiveVerbosity); constraint != "" {
			if system != "" {
				systemWithConstraint = system + "\n\n" + constraint
			} else {
				systemWithConstraint = constraint
			}
		}

		// Build messages
		messages := chat.BuildMessages(systemWithConstraint, history, historyLimit, input)

		// Execute with spinner
		result, err := cli.ExecuteWithSpinner("", func() (executor.ExecutionResult, error) {
			return executor.ExecuteWithFallback(executor.FallbackConfig{
				ModelOverride: modelOverride,
				RemoteURL:     remoteURL,
				LocalOnly:     localOnly,
				RemoteOnly:    remoteOnly,
				Profile:       profile,
				Verbosity:     effectiveVerbosity,
				Log:           logf,
			}, messages)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] %v\n\n", err)
			continue
		}

		// Apply post-processing and render
		fmt.Printf("\n[%s]\n", currentAgent)
		fmt.Print(result.Reply)
		fmt.Printf("(source: %s)\n", result.Source)
		fmt.Println()

		// Persist messages
		now := time.Now().UTC()
		userMsg := store.Message{Role: "user", Content: input, Time: now}
		assistantAgent := currentAgent
		assistantVerbosity := effectiveVerbosity
		assistantMsg := store.Message{
			Role:      "assistant",
			Content:   result.Reply,
			Agent:     &assistantAgent,
			Verbosity: &assistantVerbosity,
			Time:      now,
		}

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
