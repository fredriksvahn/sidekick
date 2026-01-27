package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/earlysvahn/sidekick/internal/agent"
	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/cli"
	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/executor"
	"github.com/earlysvahn/sidekick/internal/render"
	"github.com/earlysvahn/sidekick/internal/store"
)

// RunOneShot handles one-shot query execution (default mode)
func RunOneShot(args []string) error {
	fs := flag.NewFlagSet("sidekick", flag.ExitOnError)

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
	fs.IntVar(&verbosity, "verbosity", -1, "verbosity|v: output verbosity (0=minimal, 1=concise, 2=normal, 3=verbose)")
	fs.IntVar(&verbosity, "v", -1, "")
	fs.BoolVar(&localOnly, "local", false, "force local Ollama execution")
	fs.BoolVar(&remoteOnly, "remote", false, "force remote execution")
	fs.BoolVar(&quiet, "quiet", false, "suppress non-error logs")
	fs.StringVar(&storageBackend, "storage", "file", "storage|s: storage backend (file|sqlite)")
	fs.StringVar(&storageBackend, "s", "file", "")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return fmt.Errorf("no prompt provided")
	}

	rawPrompt := strings.Join(fs.Args(), " ")

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
		// Apply profile defaults only if not explicitly overridden
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

	ctxHist, err := historyStore.LoadContext(contextName)
	if err != nil {
		return fmt.Errorf("history error: %w", err)
	}
	if systemPrompt != "" {
		ctxHist.System = systemPrompt
		if err := historyStore.SaveContext(contextName, ctxHist); err != nil {
			return fmt.Errorf("history error: %w", err)
		}
	}

	system := ctxHist.System
	history := ctxHist.Messages

	remoteURL, err := config.LoadRemote()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	// Calculate effective verbosity
	effectiveVerbosity := executor.Effective(verbosity, profile)

	// Inject system constraint for low verbosity modes
	systemWithConstraint := system
	if constraint := executor.SystemConstraint(effectiveVerbosity); constraint != "" {
		if system != "" {
			systemWithConstraint = system + "\n\n" + constraint
		} else {
			systemWithConstraint = constraint
		}
	}

	messages := chat.BuildMessages(systemWithConstraint, history, historyLimit, rawPrompt)

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
		return fmt.Errorf("executor error: %w", err)
	}

	// Apply post-processing and render
	processedReply := executor.PostProcess(result.Reply, effectiveVerbosity)
	fmt.Print(render.Markdown(processedReply))
	fmt.Printf("(source: %s)\n", result.Source)

	now := time.Now().UTC()
	_ = historyStore.Append(contextName, store.Message{Role: "user", Content: rawPrompt, Time: now})
	_ = historyStore.Append(contextName, store.Message{Role: "assistant", Content: result.Reply, Time: now})

	return nil
}

// PrintUsage prints the usage information
func PrintUsage() {
	fmt.Println("Sidekick - AI assistant CLI with context persistence and agent profiles")
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("  sidekick [OPTIONS] \"your prompt\"              One-shot query")
	fmt.Println("  sidekick chat [OPTIONS]                       Interactive chat mode")
	fmt.Println("  sidekick tui [OPTIONS]                        Full-screen TUI mode")
	fmt.Println("  sidekick contexts [--storage BACKEND]         List all contexts")
	fmt.Println("  sidekick history --context NAME               Show context history")
	fmt.Println("  sidekick sync push|pull                       Sync contexts SQLite ↔ Postgres")
	fmt.Println("  sidekick sync agents push|pull                Sync agents SQLite ↔ Postgres")
	fmt.Println("  sidekick agents list                          List all agents")
	fmt.Println("  sidekick agents show <id>                     Show agent details")
	fmt.Println("  sidekick agents create [--file PATH]          Create agent from JSON")
	fmt.Println("  sidekick agents update <id> [--file PATH]     Update agent from JSON")
	fmt.Println("  sidekick agents delete <id>                   Delete agent")
	fmt.Println("  sidekick agents enable <id>                   Enable agent")
	fmt.Println("  sidekick agents disable <id>                  Disable agent")
	fmt.Println()
	fmt.Println("COMMON OPTIONS:")
	fmt.Println("  --agent PROFILE        Use agent profile (see below)")
	fmt.Println("  --context, --ctx NAME  Context name (default: misc)")
	fmt.Println("  --system PROMPT        Override system prompt")
	fmt.Println("  --history N            Number of messages to include (default: 4)")
	fmt.Println("  --storage BACKEND      Storage backend: file|sqlite|postgres")
	fmt.Println("  --local                Force local Ollama execution")
	fmt.Println("  --remote               Force remote execution")
	fmt.Println("  --model MODEL          Override model selection")
	fmt.Println("  --quiet                Suppress non-error logs")
	fmt.Println()
	fmt.Println("AVAILABLE AGENTS:")
	profiles := agent.ListProfiles()
	for _, name := range profiles {
		p := agent.GetProfile(name)
		if p != nil {
			if name == "default" {
				fmt.Printf("  %-18s No specialized behavior\n", name)
			} else {
				// Show first line of system prompt as description
				desc := p.SystemPrompt
				if len(desc) > 50 {
					desc = desc[:50] + "..."
				}
				if idx := strings.Index(desc, "\n"); idx > 0 {
					desc = desc[:idx]
				}
				fmt.Printf("  %-18s %s\n", name, desc)
			}
		}
	}
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  sidekick \"explain goroutines\"")
	fmt.Println("  sidekick --agent golang-dev \"write a web server\"")
	fmt.Println("  sidekick chat --agent code --context myproject")
	fmt.Println("  sidekick tui --agent sql-dev")
	fmt.Println("  sidekick sync agents push")
	fmt.Println("  echo '{...}' | sidekick agents create")
	fmt.Println()
	fmt.Println("INTERACTIVE COMMANDS:")
	fmt.Println("  /agent NAME            Switch to different agent profile")
	fmt.Println("  Ctrl+C / Ctrl+D        Exit chat/TUI mode")
	fmt.Println()
	fmt.Println("EXECUTION SOURCE:")
	fmt.Println("  Responses show (source: local|remote|fallback) indicating where")
	fmt.Println("  the LLM execution occurred. Use --local or --remote to force a source.")
}
