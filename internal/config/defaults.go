package config

import "time"

type Ollama struct {
	Host        string
	Model       string
	KeepAlive   string
	Temperature float64
	ColdStart   time.Duration
}

type App struct {
	Ollama Ollama
}

func Defaults() App {
	return App{
		Ollama: Ollama{
			Host:        "http://localhost:11434",
			Model:       "mistral:latest",
			KeepAlive:   "30m",
			Temperature: 0.7,
			ColdStart:   3 * time.Second,
		},
	}
}
