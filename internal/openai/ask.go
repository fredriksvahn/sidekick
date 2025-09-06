package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/earlysvahn/sidekick/internal/config"
)

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResp struct {
	Choices []struct {
		Message chatMsg `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func Ask(cfg config.OpenAI, prompt string) (string, error) {
	if cfg.APIKey == "" {
		return "", fmt.Errorf("missing OPENAI_API_KEY (set in env or config)")
	}

	req := map[string]any{
		"model": cfg.Model,
		"messages": []chatMsg{
			{Role: "user", Content: prompt},
		},
		"temperature": cfg.Temperature,
	}
	if cfg.MaxTokens > 0 {
		req["max_tokens"] = cfg.MaxTokens
	}

	b, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(b))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var out chatResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error.Message != "" {
		return "", fmt.Errorf("%s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("no choices from OpenAI")
	}

	return out.Choices[0].Message.Content, nil
}
