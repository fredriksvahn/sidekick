package config

import (
	"os"
)

func Resolve(modelOverride string) App {
	cfg := FromEnv()

	userCfg, _ := Load()
	host, _ := os.Hostname()
	profile := GetActiveProfile(userCfg, host)

	if profile.GeneralModel != "" {
		cfg.Ollama.Model = profile.GeneralModel
	}
	if profile.KeepAlive != "" {
		cfg.Ollama.KeepAlive = profile.KeepAlive
	}
	if profile.Temperature != 0 {
		cfg.Ollama.Temperature = profile.Temperature
	}

	if modelOverride != "" {
		cfg.Ollama.Model = modelOverride
	}

	return cfg
}
