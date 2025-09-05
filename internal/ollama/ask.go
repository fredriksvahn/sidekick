package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/earlysvahn/sidekick/internal/config"
)

type chatReq struct {
	Model    string              `json:"model"`
	Messages []map[string]string `json:"messages"`
	Stream   bool                `json:"stream"`
	Options  map[string]any      `json:"options,omitempty"`
}

type chatResp struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error"`
}

// Ask sends a prompt to Ollama and returns the reply.
// If the model is missing, it asks user if they want to pull it.
func Ask(cfg config.Ollama, prompt string) string {
	body := chatReq{
		Model: cfg.Model,
		Messages: []map[string]string{
			{"role": "user", "content": prompt},
		},
		Stream: false,
		Options: map[string]any{
			"temperature": cfg.Temperature,
		},
	}

	b, _ := json.Marshal(body)
	resp, err := http.Post(cfg.Host+"/api/chat", "application/json", bytes.NewReader(b))
	if err != nil {
		return fmt.Sprintf("[ollama error] %v", err)
	}
	defer resp.Body.Close()

	var out chatResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Sprintf("[decode error] %v", err)
	}

	// Handle missing model
	if strings.Contains(out.Error, "model") && strings.Contains(out.Error, "not found") {
		fmt.Printf("[ollama error] %s\n", out.Error)
		fmt.Printf("Do you want to pull \"%s\"? [y/N]: ", cfg.Model)

		var ans string
		fmt.Scanln(&ans)
		if strings.ToLower(strings.TrimSpace(ans)) == "y" {
			cmd := exec.Command("ollama", "pull", cfg.Model)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Sprintf("[ollama pull failed] %v", err)
			}
			// retry once after pull
			return Ask(cfg, prompt)
		}
	}

	if out.Error != "" {
		return fmt.Sprintf("[ollama error] %s", out.Error)
	}
	return out.Message.Content
}
