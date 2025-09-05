package config

import (
	"encoding/json"
	"os"
)

func DefaultProfile() HostProfile {
	return HostProfile{
		GeneralModel: "phi3:3.8b",
		CodeModel:    "deepseek-coder:1.3b",
		KeepAlive:    "30m",
		Temperature:  0.7,
		NumPredict:   128,
		NumCtx:       2048,
	}
}

func EnsureDir() error { return os.MkdirAll(Dir(), 0o755) }

func Load() (AppConfig, error) {
	var cfg AppConfig
	b, err := os.ReadFile(File())
	if err != nil {
		return AppConfig{Version: 1, Hosts: map[string]HostProfile{}}, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return AppConfig{}, err
	}
	if cfg.Hosts == nil {
		cfg.Hosts = map[string]HostProfile{}
	}
	return cfg, nil
}

func Save(cfg AppConfig) error {
	if err := EnsureDir(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := File() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, File())
}

func ActiveHostName(getHostname func() (string, error)) string {
	h, err := getHostname()
	if err != nil || h == "" {
		return "default"
	}
	return h
}

func GetActiveProfile(cfg AppConfig, host string) HostProfile {
	if p, ok := cfg.Hosts[host]; ok {
		return p
	}
	if cfg.DefaultHost != "" {
		if p, ok := cfg.Hosts[cfg.DefaultHost]; ok {
			return p
		}
	}
	return DefaultProfile()
}

func SetHostProfile(cfg *AppConfig, host string, p HostProfile) {
	if cfg.Hosts == nil {
		cfg.Hosts = map[string]HostProfile{}
	}
	cfg.Hosts[host] = p
}
