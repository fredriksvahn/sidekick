package config

import (
	"os"
	"strings"
)

func Resolve(modelOverride, prompt string) App {
	cfg := FromEnv()

	userCfg, _ := Load()
	host, _ := os.Hostname()
	profile := GetActiveProfile(userCfg, host)

	if profile.Model != "" {
		cfg.OpenAI.Model = profile.Model
	}
	if profile.Temperature != 0 {
		cfg.OpenAI.Temperature = profile.Temperature
	}
	if profile.MaxTokens != 0 {
		cfg.OpenAI.MaxTokens = profile.MaxTokens
	}
	if profile.APIKey != "" {
		cfg.OpenAI.APIKey = profile.APIKey
	}

	if modelOverride != "" {
		cfg.OpenAI.Model = modelOverride
		return cfg
	}

	low := strings.ToLower(prompt)

	switch {
	case strings.Contains(low, "code"), strings.Contains(low, "api"), strings.Contains(low, "sql"):
		cfg.OpenAI.Temperature = 0.2
	case strings.Contains(low, "story"), strings.Contains(low, "creative"), strings.Contains(low, "spanish"):
		cfg.OpenAI.Temperature = 0.9
	default:
		cfg.OpenAI.Temperature = 0.7
	}

	return cfg
}
