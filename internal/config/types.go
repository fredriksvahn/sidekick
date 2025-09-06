package config

type HostProfile struct {
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"max_tokens"`
	APIKey      string  `json:"api_key,omitempty"`
}

type AppConfig struct {
	Version     int                    `json:"version"`
	Hosts       map[string]HostProfile `json:"hosts"`
	DefaultHost string                 `json:"default_host"`
}
