package executor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/earlysvahn/sidekick/internal/chat"
)

type HTTPExecutor struct {
	BaseURL string
	Client  *http.Client
	Log     func(string)
}

func NewHTTPExecutor(baseURL string, timeout time.Duration, log func(string)) *HTTPExecutor {
	return &HTTPExecutor{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: timeout},
		Log:     log,
	}
}

func (e *HTTPExecutor) Available() (bool, error) {
	if e.Log != nil {
		e.Log(fmt.Sprintf("remote health check start %s/health", e.BaseURL))
	}
	req, _ := http.NewRequest("GET", e.BaseURL+"/health", nil)
	resp, err := e.Client.Do(req)
	if err != nil {
		if e.Log != nil {
			e.Log(fmt.Sprintf("remote health check failed: %v", err))
		}
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if e.Log != nil {
			e.Log(fmt.Sprintf("remote health check failed: status %d", resp.StatusCode))
		}
		return false, fmt.Errorf("health check status %d", resp.StatusCode)
	}
	if e.Log != nil {
		e.Log("remote health check ok")
	}
	return true, nil
}

func (e *HTTPExecutor) Execute(messages []chat.Message) (string, error) {
	if e.Log != nil {
		e.Log("remote execute start")
	}
	payload := map[string]any{"messages": messages}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", e.BaseURL+"/execute", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.Client.Do(req)
	if err != nil {
		if e.Log != nil {
			e.Log(fmt.Sprintf("remote execute failed: %v", err))
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		if e.Log != nil {
			e.Log(fmt.Sprintf("remote execute non-200: %d", resp.StatusCode))
		}
		return "", fmt.Errorf("http executor error: %d %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Reply string `json:"reply"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Reply == "" {
		return "", errors.New("empty reply")
	}
	if e.Log != nil {
		e.Log("remote execute ok")
	}
	return out.Reply, nil
}
