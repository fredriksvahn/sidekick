package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/earlysvahn/sidekick/internal/agent"
	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/executor"
	"github.com/earlysvahn/sidekick/internal/tui"
)

// RunTUICommand handles the 'tui' subcommand
func RunTUICommand(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	var modelOverride string
	var contextName string
	var historyLimit int
	var systemPrompt string
	var localOnly bool
	var remoteOnly bool
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
	fs.StringVar(&storageBackend, "storage", "file", "storage|s: storage backend (file|sqlite)")
	fs.StringVar(&storageBackend, "s", "file", "")
	fs.IntVar(&verbosity, "verbosity", -1, "verbosity|v: output verbosity (0=minimal, 1=concise, 2=normal, 3=verbose, 4=very verbose, 5=exhaustive)")
	fs.IntVar(&verbosity, "v", -1, "")

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

	effectiveVerbosity := executor.DefaultVerbosity()
	if verbosity >= 0 {
		if v, clamped := executor.ClampVerbosity(verbosity); clamped {
			fmt.Fprintf(os.Stderr, "[warning] verbosity %d clamped to %d\n", verbosity, v)
			effectiveVerbosity = v
		} else {
			effectiveVerbosity = v
		}
	}

	// Create execute function that wraps executor
	logf := func(msg string) {
		// Silent logging for TUI
	}
	executeFn := func(messages []chat.Message, currentVerbosity int) (tui.ExecutionResult, error) {
		result, err := executor.ExecuteWithFallback(executor.FallbackConfig{
			ModelOverride: modelOverride,
			RemoteURL:     remoteURL,
			LocalOnly:     localOnly,
			RemoteOnly:    remoteOnly,
			Profile:       profile,
			Verbosity:     currentVerbosity,
			Log:           logf,
		}, messages)
		if err != nil {
			return tui.ExecutionResult{}, err
		}
		return tui.ExecutionResult{Reply: result.Reply, Source: result.Source}, nil
	}

	// Determine agent name for display
	currentAgent := "default"
	if agentProfile != "" {
		currentAgent = agentProfile
	}

	// Run TUI
	return tui.Run(tui.Config{
		ContextName:     contextName,
		SystemPrompt:    ctxHist.System,
		History:         ctxHist.Messages,
		HistoryStore:    historyStore,
		HistoryLimit:    historyLimit,
		ModelOverride:   modelOverride,
		RemoteURL:       remoteURL,
		LocalOnly:       localOnly,
		RemoteOnly:      remoteOnly,
		AgentName:       currentAgent,
		AgentProfile:    profile,
		AvailableAgents: agent.ListProfiles(),
		Verbosity:       effectiveVerbosity,
		ExecuteFn:       executeFn,
	})
}
