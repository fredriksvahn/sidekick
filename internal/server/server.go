package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/executor"
)

const DefaultAddr = "0.0.0.0:1337"

// Run starts the HTTP server
func Run(modelOverride string) error {
	// Explicitly bind to IPv4 to ensure LAN reachability on Windows/WSL2
	listener, err := net.Listen("tcp4", DefaultAddr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[sidekick] listening on %s\n", listener.Addr())

	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/execute", handleExecute(modelOverride))

	return http.Serve(listener, nil)
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
