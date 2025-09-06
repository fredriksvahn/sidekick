package router

import (
	"fmt"
	"strings"

	"github.com/earlysvahn/sidekick/internal/config"
)

func Route(prompt string, base config.App) (string, config.App) {
	low := strings.ToLower(prompt)

	switch {
	case strings.HasPrefix(low, "code:"):
		base.OpenAI.Model = "gpt-4o-mini"
		if base.OpenAI.Temperature == 0.7 { // adjust only if default
			base.OpenAI.Temperature = 0.2
		}
		fmt.Printf("[router] using model=%s temp=%.2f\n", base.OpenAI.Model, base.OpenAI.Temperature)
		return strings.TrimPrefix(prompt, "code:"), base

	case strings.HasPrefix(low, "plan:"):
		base.OpenAI.Model = "gpt-4o"
		fmt.Printf("[router] using model=%s temp=%.2f\n", base.OpenAI.Model, base.OpenAI.Temperature)
		return strings.TrimPrefix(prompt, "plan:"), base

	case strings.HasPrefix(low, "creative:"), strings.Contains(low, "story"):
		base.OpenAI.Model = "gpt-4o-mini"
		if base.OpenAI.Temperature == 0.7 {
			base.OpenAI.Temperature = 0.9
		}
		fmt.Printf("[router] using model=%s temp=%.2f\n", base.OpenAI.Model, base.OpenAI.Temperature)
		return strings.TrimPrefix(prompt, "creative:"), base

	default:
		// leave defaults
		fmt.Printf("[router] using model=%s temp=%.2f\n", base.OpenAI.Model, base.OpenAI.Temperature)
		return prompt, base
	}
}
