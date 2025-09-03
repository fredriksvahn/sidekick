package router

import (
	"strings"

	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/utils"
)

func Route(q string, base config.App) (string, config.Ollama) {
	cfg := base.Ollama

	s := strings.TrimSpace(q)
	low := strings.ToLower(s)

	switch {
	case utils.HasPrefixCI(low, "log:"):
		cfg.Model = "phi3:mini"

	case utils.HasPrefixCI(low, "code:"), utils.HasPrefixCI(low, "fix:"), utils.HasPrefixCI(low, "explain code:"):
		cfg.Model = "deepseek-coder:6.7b"

	// add more rules over time…
	default:
		// keep defaults (mistral:latest)
	}

	return s, cfg
}
