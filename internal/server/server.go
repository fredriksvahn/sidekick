package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/executor"
	"github.com/earlysvahn/sidekick/internal/store"
)

const DefaultAddr = "0.0.0.0:1337"

// Run starts the HTTP server
func Run(modelOverride string, historyStore store.HistoryStore) error {
	// Explicitly bind to IPv4 to ensure LAN reachability on Windows/WSL2
	listener, err := net.Listen("tcp4", DefaultAddr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[sidekick] listening on %s\n", listener.Addr())

	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/execute", handleExecute(modelOverride))
	http.HandleFunc("/contexts", handleContexts(historyStore))
	http.HandleFunc("/contexts/", handleContextMessages(historyStore))

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

func handleContexts(historyStore store.HistoryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		contexts, err := historyStore.ListContexts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Convert to JSON response format
		type contextResponse struct {
			Name      string  `json:"name"`
			Messages  int     `json:"messages"`
			LastUsed  *string `json:"last_used"`
		}

		response := make([]contextResponse, 0, len(contexts))
		for _, ctx := range contexts {
			var lastUsed *string
			if !ctx.LastUsed.IsZero() {
				formatted := ctx.LastUsed.Format("2006-01-02T15:04:05Z07:00")
				lastUsed = &formatted
			}
			response = append(response, contextResponse{
				Name:     ctx.Name,
				Messages: ctx.MessageCount,
				LastUsed: lastUsed,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}
}

func handleContextMessages(historyStore store.HistoryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Extract context name from path
		path := strings.TrimPrefix(r.URL.Path, "/contexts/")
		path = strings.TrimSuffix(path, "/messages")
		contextName := path

		if contextName == "" {
			http.Error(w, "context name required", http.StatusBadRequest)
			return
		}

		// Load context
		ctxHist, err := historyStore.LoadContext(contextName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Check if context exists
		if ctxHist.System == "" && len(ctxHist.Messages) == 0 {
			http.Error(w, "context not found", http.StatusNotFound)
			return
		}

		// Build response with system prompt as first message if present
		type messageResponse struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}

		response := make([]messageResponse, 0, len(ctxHist.Messages)+1)
		if ctxHist.System != "" {
			response = append(response, messageResponse{
				Role:    "system",
				Content: ctxHist.System,
			})
		}

		for _, msg := range ctxHist.Messages {
			response = append(response, messageResponse{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}
}
