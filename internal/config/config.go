package config

import "os"

// Later we can parse flags/env/files. For now: env overrides.
func FromEnv() App {
	cfg := Defaults()

	if v := os.Getenv("SIDEKICK_OLLAMA_HOST"); v != "" {
		cfg.Ollama.Host = v
	}
	if v := os.Getenv("SIDEKICK_OLLAMA_MODEL"); v != "" {
		cfg.Ollama.Model = v
	}
	if v := os.Getenv("SIDEKICK_OLLAMA_KEEP_ALIVE"); v != "" {
		cfg.Ollama.KeepAlive = v
	}
	if v := os.Getenv("SIDEKICK_OLLAMA_TEMP"); v != "" {
		// keep simple; parse later as needed
	}

	return cfg
}
