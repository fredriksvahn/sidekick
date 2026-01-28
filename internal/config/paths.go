package config

import (
	"os"
	"path/filepath"
)

func Dir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sidekick")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME") // fallback
	}
	return filepath.Join(home, ".config", "sidekick")
}
func File() string { return filepath.Join(Dir(), "config.json") }
