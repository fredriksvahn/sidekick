package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/earlysvahn/sidekick/internal/chat"
)

type chatReq struct {
	Model    string         `json:"model"`
	Messages []chat.Message `json:"messages"`
	Stream   bool           `json:"stream"`
}

type chatResp struct {
	Message chat.Message `json:"message"`
	Error   string       `json:"error"`
}

func Ask(model string, messages []chat.Message) (string, error) {
	req := chatReq{
		Model:    model,
		Messages: messages,
		Stream:   false,
	}
	b, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest("POST", "http://localhost:11434/api/chat", bytes.NewReader(b))
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
