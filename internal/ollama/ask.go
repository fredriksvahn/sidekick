package ollama

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/earlysvahn/sidekick/internal/chat"
)

const BaseURL = "http://localhost:11434"

type chatReq struct {
	Model    string         `json:"model"`
	Messages []chat.Message `json:"messages"`
	Stream   bool           `json:"stream"`
	Options  map[string]int `json:"options,omitempty"`
}

type chatResp struct {
	Message chat.Message `json:"message"`
	Error   string       `json:"error"`
}

func Ask(model string, messages []chat.Message) (string, error) {
	return AskWithOptions(model, messages, nil)
}

func AskWithOptions(model string, messages []chat.Message, options map[string]int) (string, error) {
	req := chatReq{
		Model:    model,
		Messages: messages,
		Stream:   false,
		Options:  options,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", BaseURL+"/api/chat", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var out chatResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", fmt.Errorf("%s", out.Error)
	}
	if out.Message.Content == "" {
		return "", fmt.Errorf("no response from Ollama")
	}
	return out.Message.Content, nil
}

// AskWithStreaming executes a request with streaming enabled.
// The onDelta callback is called for each token chunk as it arrives.
// Returns the complete response text or an error.
func AskWithStreaming(model string, messages []chat.Message, options map[string]int, onDelta func(string) error) (string, error) {
	req := chatReq{
		Model:    model,
		Messages: messages,
		Stream:   true,
		Options:  options,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", BaseURL+"/api/chat", bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var fullResponse strings.Builder
	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk chatResp
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return "", fmt.Errorf("parse streaming response: %w", err)
		}

		if chunk.Error != "" {
			return "", fmt.Errorf("%s", chunk.Error)
		}

		if chunk.Message.Content != "" {
			fullResponse.WriteString(chunk.Message.Content)
			if onDelta != nil {
				if err := onDelta(chunk.Message.Content); err != nil {
					return "", fmt.Errorf("delta callback error: %w", err)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read streaming response: %w", err)
	}

	return fullResponse.String(), nil
}
