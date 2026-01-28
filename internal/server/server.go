package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/executor"
	"github.com/earlysvahn/sidekick/internal/store"
)

const DefaultAddr = "0.0.0.0:1337"

// Run starts the HTTP server
func Run(modelOverride string, historyStore *store.PostgresStore) error {
	// Explicitly bind to IPv4 to ensure LAN reachability on Windows/WSL2
	listener, err := net.Listen("tcp4", DefaultAddr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[sidekick] listening on %s\n", listener.Addr())

	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/execute", handleExecute(modelOverride))
	http.HandleFunc("/api/chat", handleChat(historyStore))
	http.HandleFunc("/api/contexts", handleAPIContexts(historyStore))
	http.HandleFunc("/api/contexts/", handleAPIContext(historyStore))
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

func handleChat(historyStore *store.PostgresStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Context   string `json:"context"`
			Agent     string `json:"agent"`
			Verbosity *int   `json:"verbosity"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		contextName := strings.TrimSpace(req.Context)
		if contextName == "" {
			http.Error(w, "context required", http.StatusBadRequest)
			return
		}
		if len(req.Messages) == 0 {
			http.Error(w, "messages required", http.StatusBadRequest)
			return
		}

		agentName := req.Agent
		if agentName == "" {
			agentName = "default"
		}
		verbosity := 2
		if req.Verbosity != nil {
			verbosity = *req.Verbosity
		}

		now := time.Now().UTC()
		messages := make([]store.Message, 0, len(req.Messages))
		for _, msg := range req.Messages {
			messages = append(messages, store.Message{
				Role:    msg.Role,
				Content: msg.Content,
				Time:    now,
			})
		}

		// Contexts are created implicitly when the first message is written.
		if err := historyStore.AppendMessagesWithMeta(contextName, agentName, verbosity, messages); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

func handleAPIContexts(historyStore *store.PostgresStore) http.HandlerFunc {
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

		type contextResponse struct {
			Name      string `json:"name"`
			Agent     string `json:"agent"`
			Verbosity int    `json:"verbosity"`
		}

		response := make([]contextResponse, 0, len(contexts))
		for _, ctx := range contexts {
			response = append(response, contextResponse{
				Name:      ctx.Name,
				Agent:     ctx.Agent,
				Verbosity: ctx.Verbosity,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}
}

func handleAPIContext(historyStore *store.PostgresStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/contexts/")
		name = strings.TrimSuffix(name, "/")
		if name == "" {
			http.Error(w, "context name required", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodPatch:
			var req struct {
				Name      *string `json:"name"`
				Agent     *string `json:"agent"`
				Verbosity *int    `json:"verbosity"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}

			updated, err := historyStore.UpdateContext(name, req.Name, req.Agent, req.Verbosity)
			if err != nil {
				if err == sql.ErrNoRows {
					http.Error(w, "context not found", http.StatusNotFound)
					return
				}
				if err.Error() == "context already exists" {
					http.Error(w, "context already exists", http.StatusConflict)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":      updated.Name,
				"agent":     updated.Agent,
				"verbosity": updated.Verbosity,
			})
		case http.MethodDelete:
			if err := historyStore.DeleteContext(name); err != nil {
				if err == sql.ErrNoRows {
					http.Error(w, "context not found", http.StatusNotFound)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
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
			Name     string  `json:"name"`
			Messages int     `json:"messages"`
			LastUsed *string `json:"last_used"`
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
