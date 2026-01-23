package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/executor"
	"github.com/earlysvahn/sidekick/internal/server"
	"github.com/earlysvahn/sidekick/internal/store"
)

func main() {
	var modelOverride string
	var contextName string
	var historyLimit int
	var systemPrompt string
	var serve bool
	var localOnly bool
	var remoteOnly bool
	var quiet bool
	flag.StringVar(&modelOverride, "model", "", "force a specific Ollama model")
	flag.StringVar(&contextName, "context", "misc", "context name")
	flag.IntVar(&historyLimit, "history", 4, "number of prior messages to include")
	flag.StringVar(&systemPrompt, "system", "", "system prompt for this context")
	flag.BoolVar(&serve, "serve", false, "run HTTP server")
	flag.BoolVar(&localOnly, "local", false, "force local Ollama execution")
	flag.BoolVar(&remoteOnly, "remote", false, "force remote execution")
	flag.BoolVar(&quiet, "quiet", false, "suppress non-error logs")
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
		os.Exit(1)
	}
	rawPrompt := strings.Join(flag.Args(), " ")

	historyStore := store.NewFileStore()
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
	if len(history) > historyLimit {
		history = history[len(history)-historyLimit:]
	}
	messages := make([]chat.Message, 0, len(history)+1)
	if system != "" {
		messages = append(messages, chat.Message{Role: "system", Content: system})
	}
	for _, m := range history {
		messages = append(messages, chat.Message{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, chat.Message{Role: "user", Content: rawPrompt})

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
