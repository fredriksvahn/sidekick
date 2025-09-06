package config

import (
	"os"
	"strconv"
)

func FromEnv() App {
	cfg := Defaults()

	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.OpenAI.APIKey = v
	}
	if v := os.Getenv("SIDEKICK_OPENAI_MODEL"); v != "" {
		cfg.OpenAI.Model = v
	}
	if v := os.Getenv("SIDEKICK_OPENAI_TEMP"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.OpenAI.Temperature = f
		}
	}
	if v := os.Getenv("SIDEKICK_OPENAI_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.OpenAI.MaxTokens = n
		}
	}

	return cfg
}
