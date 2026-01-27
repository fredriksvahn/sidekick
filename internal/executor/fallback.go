package executor

import (
	"fmt"
	"time"

	"github.com/earlysvahn/sidekick/internal/agent"
	"github.com/earlysvahn/sidekick/internal/chat"
)

// ExecutionResult contains the reply and source of an LLM execution
type ExecutionResult struct {
	Reply  string
	Source string // "local", "remote", or "fallback"
}

// FallbackConfig configures the fallback execution behavior
type FallbackConfig struct {
	ModelOverride string
	RemoteURL     string
	LocalOnly     bool
	RemoteOnly    bool
	Profile       *agent.AgentProfile
	Verbosity     int
	Log           func(string)
}

// ExecuteWithFallback executes an LLM request with fallback logic:
// 1. If localOnly, use local Ollama
// 2. If no remote URL configured, use local Ollama
// 3. If remote available and healthy, try remote first
// 4. On remote failure (if not remoteOnly), fallback to local Ollama
func ExecuteWithFallback(cfg FallbackConfig, messages []chat.Message) (ExecutionResult, error) {
	logf := cfg.Log
	if logf == nil {
		logf = func(string) {} // No-op logger
	}

	// Determine local model to use
	localModel := cfg.ModelOverride
	if cfg.Profile != nil && cfg.ModelOverride == "" {
		localModel = cfg.Profile.LocalModel
	}

	// Force local execution
	if cfg.LocalOnly {
		logf("execution path: local ollama (forced)")
		reply, err := (&OllamaExecutor{Model: localModel, Log: nil, Verbosity: cfg.Verbosity}).Execute(messages)
		return ExecutionResult{Reply: reply, Source: "local"}, err
	}

	// No remote configured, use local
	if cfg.RemoteURL == "" {
		if cfg.RemoteOnly {
			return ExecutionResult{}, fmt.Errorf("remote execution requested but no remote is configured")
		}
		logf("execution path: local ollama (no remote configured)")
		reply, err := (&OllamaExecutor{Model: localModel, Log: nil, Verbosity: cfg.Verbosity}).Execute(messages)
		return ExecutionResult{Reply: reply, Source: "local"}, err
	}

	// Try remote execution
	httpExec := NewHTTPExecutor(cfg.RemoteURL, 30*time.Second, nil)

	// If using profile, pass the remote model to HTTP executor
	if cfg.Profile != nil && cfg.ModelOverride == "" {
		// For now, HTTPExecutor doesn't support model selection
		// This is handled by the remote server's default
	}

	ok, healthErr := httpExec.Available()
	if ok {
		reply, err := httpExec.Execute(messages)
		if err == nil {
			return ExecutionResult{Reply: reply, Source: "remote"}, nil
		}
		if cfg.RemoteOnly {
			return ExecutionResult{}, err
		}
		logf("using local")
	} else if healthErr != nil {
		if cfg.RemoteOnly {
			return ExecutionResult{}, fmt.Errorf("remote execution requested but health check failed: %v", healthErr)
		}
		logf("using local")
	} else if cfg.RemoteOnly {
		return ExecutionResult{}, fmt.Errorf("remote execution requested but health check failed")
	}

	// Fallback to local
	reply, err := (&OllamaExecutor{Model: localModel, Log: nil, Verbosity: cfg.Verbosity}).Execute(messages)
	return ExecutionResult{Reply: reply, Source: "fallback"}, err
}
