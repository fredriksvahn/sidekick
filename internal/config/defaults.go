package config

type OpenAI struct {
	APIKey      string
	Model       string
	Temperature float64
	MaxTokens   int
}

type App struct {
	OpenAI OpenAI
}

func Defaults() App {
	return App{
		OpenAI: OpenAI{
			APIKey:      "",
			Model:       "gpt-4o-mini",
			Temperature: 0.7,
			MaxTokens:   512,
		},
	}
}
