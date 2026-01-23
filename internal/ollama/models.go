package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const DefaultModel = "qwen2.5:7b"

func SelectedModel(override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	return DefaultModel
}

func EnsureModel(model string, logf func(string)) error {
	name := SelectedModel(model)
	if logf != nil {
		logf(fmt.Sprintf("model selected: %s", name))
	}
	ok, err := hasModel(name)
	if err != nil {
		return err
	}
	if ok {
		if logf != nil {
			logf(fmt.Sprintf("model ready: %s", name))
		}
		return nil
	}
	if logf != nil {
		logf(fmt.Sprintf("model missing: %s", name))
		logf(fmt.Sprintf("pulling model: %s", name))
	}
	if err := pullModel(name); err != nil {
		return err
	}
	if logf != nil {
		logf(fmt.Sprintf("model ready: %s", name))
	}
	return nil
}

func hasModel(model string) (bool, error) {
	resp, err := http.Get(BaseURL + "/api/tags")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("ollama tags status %d", resp.StatusCode)
	}
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	for _, m := range out.Models {
		if m.Name == model {
			return true, nil
		}
	}
	return false, nil
}

func pullModel(model string) error {
	payload := map[string]any{"name": model, "stream": false}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal pull request: %w", err)
	}
	req, err := http.NewRequest("POST", BaseURL+"/api/pull", bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("create pull request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama pull status %d", resp.StatusCode)
	}
	return nil
}
