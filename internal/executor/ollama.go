package executor

import (
	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/ollama"
)

type OllamaExecutor struct {
	Model string
	Log   func(string)
}

func (e *OllamaExecutor) Execute(messages []chat.Message) (string, error) {
	model := ollama.SelectedModel(e.Model)
	if err := ollama.EnsureModel(model, e.Log); err != nil {
		return "", err
	}
	if e.Log != nil {
		e.Log("local ollama request start")
	}
	reply, err := ollama.Ask(model, messages)
	if err == nil && e.Log != nil {
		e.Log("local ollama response received")
	}
	return reply, err
}
