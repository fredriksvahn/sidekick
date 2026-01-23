package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/executor"
)

const DefaultAddr = "0.0.0.0:1337"

// Run starts the HTTP server
func Run(modelOverride string) error {
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/execute", handleExecute(modelOverride))

	fmt.Fprintf(os.Stderr, "[sidekick] server listening on %s\n", DefaultAddr)
	return http.ListenAndServe(DefaultAddr, nil)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func handleExecute(modelOverride string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Messages []chat.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		logf := func(msg string) {
			fmt.Fprintf(os.Stderr, "[sidekick] %s\n", msg)
		}

		// Log incoming request
		if len(req.Messages) > 0 {
			lastMsg := req.Messages[len(req.Messages)-1]
			logf(fmt.Sprintf("received request: %d messages, last=%q", len(req.Messages), lastMsg.Content))
		}

		reply, err := (&executor.OllamaExecutor{Model: modelOverride, Log: logf}).Execute(req.Messages)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"reply": reply})
	}
}
