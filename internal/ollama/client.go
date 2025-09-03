package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/earlysvahn/sidekick/internal/config"
	"github.com/earlysvahn/sidekick/internal/utils"
)

type chatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error,omitempty"`
}

func isModelHot(cfg config.Ollama) bool {
	resp, err := http.Get(cfg.Host + "/api/ps")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var data struct {
		Models []struct{ Name string `json:"name"` } `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return false
	}
	for _, m := range data.Models {
		if m.Name == cfg.Model {
			return true
		}
	}
	return false
}

func Ask(cfg config.Ollama, question string) string {
	cold := false
	if !isModelHot(cfg) {
		fmt.Fprintf(os.Stderr, "[cold start] Loading %s into VRAM…\n", cfg.Model)
		cold = true
	}
	start := time.Now()

	req := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "user", "content": question},
		},
		"options": map[string]any{
			"temperature": cfg.Temperature,
		},
		"keep_alive": cfg.KeepAlive,
		"stream":     false,
	}

	body := utils.Must2(json.Marshal(req))
	resp, err := http.Post(cfg.Host+"/api/chat", "application/json", bytes.NewReader(body))
	utils.Must(err)
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	utils.Must(err)

	var cr chatResponse
	utils.Must(json.Unmarshal(raw, &cr))
	if cr.Error != "" {
		fmt.Fprintf(os.Stderr, "[ollama error] %s\n", cr.Error)
		os.Exit(1)
	}

	elapsed := time.Since(start)
	if cold && elapsed >= cfg.ColdStart {
		fmt.Fprintf(os.Stderr, "[ready] %s loaded in %dms\n", cfg.Model, elapsed.Milliseconds())
	}
	return cr.Message.Content
}
