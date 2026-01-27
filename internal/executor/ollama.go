package executor

import (
	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/ollama"
)

type OllamaExecutor struct {
	Model     string
	Log       func(string)
	Verbosity int // 0=minimal, 1=concise, 2=normal, 3=verbose
}

func (e *OllamaExecutor) Execute(messages []chat.Message) (string, error) {
	model := ollama.SelectedModel(e.Model)
	if err := ollama.EnsureModel(model, e.Log); err != nil {
		return "", err
	}
	if e.Log != nil {
		e.Log("local ollama request start")
	}

	// Map verbosity to num_predict (token limit)
	var options map[string]int
	if e.Verbosity >= 0 && e.Verbosity <= 3 {
		numPredict := map[int]int{
			0: 512,   // minimal
			1: 1024,  // concise
			2: 2048,  // normal
			3: 4096,  // verbose
		}[e.Verbosity]
		options = map[string]int{"num_predict": numPredict}
	}

	reply, err := ollama.AskWithOptions(model, messages, options)
	if err == nil && e.Log != nil {
		e.Log("local ollama response received")
	}
	return reply, err
}
