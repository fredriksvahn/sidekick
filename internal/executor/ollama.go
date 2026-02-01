package executor

import (
	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/ollama"
)

type OllamaExecutor struct {
	Model     string
	Log       func(string)
	Verbosity int // 0=minimal, 1=concise, 2=normal, 3=verbose, 4=very verbose, 5=exhaustive
}

func (e *OllamaExecutor) Execute(messages []chat.Message) (string, error) {
	model := ollama.SelectedModel(e.Model)
	if err := ollama.EnsureModel(model, e.Log); err != nil {
		return "", err
	}
	if e.Log != nil {
		e.Log("local ollama request start")
	}

	// Hard cap tokens per verbosity.
	var options map[string]int
	if e.Verbosity >= 0 && e.Verbosity <= 5 {
		options = map[string]int{"num_predict": MaxTokens(e.Verbosity)}
	}

	reply, err := ollama.AskWithOptions(model, messages, options)
	if err == nil && e.Log != nil {
		e.Log("local ollama response received")
	}
	return reply, err
}

// ExecuteStreaming executes with real-time token streaming.
// The onDelta callback is called for each token chunk as it arrives from Ollama.
// Returns the complete response text or an error.
func (e *OllamaExecutor) ExecuteStreaming(messages []chat.Message, onDelta func(string) error) (string, error) {
	model := ollama.SelectedModel(e.Model)
	if err := ollama.EnsureModel(model, e.Log); err != nil {
		return "", err
	}
	if e.Log != nil {
		e.Log("local ollama streaming request start")
	}

	// Hard cap tokens per verbosity.
	var options map[string]int
	if e.Verbosity >= 0 && e.Verbosity <= 5 {
		options = map[string]int{"num_predict": MaxTokens(e.Verbosity)}
	}

	reply, err := ollama.AskWithStreaming(model, messages, options, onDelta)
	if err == nil && e.Log != nil {
		e.Log("local ollama streaming response complete")
	}
	return reply, err
}
