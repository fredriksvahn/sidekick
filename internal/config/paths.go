package config

import (
	"os"
	"path/filepath"
)

func Dir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "sidekick")
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "sidekick")
}
func File() string { return filepath.Join(Dir(), "config.json") }
