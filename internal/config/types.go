package config

type HostProfile struct {
	GeneralModel string  `json:"general_model"`
	CodeModel    string  `json:"code_model"`
	KeepAlive    string  `json:"keep_alive"`
	Temperature  float64 `json:"temperature"`
	NumPredict   int     `json:"num_predict"`
	NumCtx       int     `json:"num_ctx"`
}

type AppConfig struct {
	Version     int                    `json:"version"`
	Hosts       map[string]HostProfile `json:"hosts"`
	DefaultHost string                 `json:"default_host"`
}
