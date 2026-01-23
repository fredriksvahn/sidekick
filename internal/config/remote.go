package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type RemoteConfig struct {
	BaseURL string `json:"base_url"`
}

func RemoteFile() string {
	return filepath.Join(Dir(), "remote.json")
}

func LoadRemote() (string, error) {
	b, err := os.ReadFile(RemoteFile())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var cfg RemoteConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return "", err
	}
	return strings.TrimSpace(cfg.BaseURL), nil
}
