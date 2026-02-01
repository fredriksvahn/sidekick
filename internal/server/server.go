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

	"github.com/earlysvahn/sidekick/internal/agent"
	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/executor"
	"github.com/earlysvahn/sidekick/internal/store"
)

const DefaultAddr = "0.0.0.0:1337"

// Run starts the HTTP server
func Run(modelOverride string, historyStore *store.PostgresStore, agentRepo agent.AgentRepository) error {
	// Explicitly bind to IPv4 to ensure LAN reachability on Windows/WSL2
	listener, err := net.Listen("tcp4", DefaultAddr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[sidekick] listening on %s\n", listener.Addr())

	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/execute", handleExecute(modelOverride, historyStore))
	http.HandleFunc("/chat", handleChat(historyStore))
	http.HandleFunc("/api/chat", handleLegacyChat(historyStore))
	http.HandleFunc("/settings", handleSettings)
	http.HandleFunc("/agents", handleAPIAgents(agentRepo))
	http.HandleFunc("/agents/", handleAPIAgent(agentRepo))
	http.HandleFunc("/api/agents", handleAPIAgents(agentRepo))
	http.HandleFunc("/api/agents/", handleAPIAgent(agentRepo))
	http.HandleFunc("/api/contexts", handleAPIContexts(historyStore))
	http.HandleFunc("/api/contexts/", handleAPIContext(historyStore))
	http.HandleFunc("/contexts", handleContexts(historyStore))
	http.HandleFunc("/contexts/", handleContextRoutes(historyStore))
	http.HandleFunc("/verbosity/keywords", handleVerbosityKeywords(historyStore))
	http.HandleFunc("/verbosity/keywords/", handleVerbosityKeyword(historyStore))

	return http.Serve(listener, nil)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func handleExecute(modelOverride string, keywordStore store.VerbosityKeywordLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Messages  []chat.Message `json:"messages"`
			Verbosity *int           `json:"verbosity"`
			Stream    bool           `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if len(req.Messages) == 0 {
			http.Error(w, "messages required", http.StatusBadRequest)
			return
		}

		lastUserMessage := latestUserMessage(req.Messages)
		verbosity, warning, err := executor.ResolveVerbosity(r.Context(), req.Verbosity, executor.DefaultVerbosity(), "default", lastUserMessage, keywordStore)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		messages := applyVerbosityConstraint(req.Messages, verbosity)

		logf := func(msg string) {
			fmt.Fprintf(os.Stderr, "[sidekick] %s\n", msg)
		}

		// Log incoming request
		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			logf(fmt.Sprintf("received request: %d messages, last=%q", len(req.Messages), lastMsg.Content))
		}

		var reply string

		if req.Stream {
			// Streaming path
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming not supported", http.StatusInternalServerError)
				return
			}

			// Set SSE headers immediately
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			// Send escalation info event immediately if it occurred
			if warning != "" {
				infoPayload, _ := json.Marshal(map[string]any{
					"type":    "info",
					"message": warning,
				})
				fmt.Fprintf(w, "data: %s\n\n", infoPayload)
				flusher.Flush()
			}

			// Stream tokens as they arrive from Ollama
			onDelta := func(delta string) error {
				deltaPayload, err := json.Marshal(map[string]any{
					"delta": delta,
				})
				if err != nil {
					return err
				}
				fmt.Fprintf(w, "data: %s\n\n", deltaPayload)
				flusher.Flush()
				return nil
			}

			reply, err = (&executor.OllamaExecutor{Model: modelOverride, Log: logf, Verbosity: verbosity}).ExecuteStreaming(messages, onDelta)
			if err != nil {
				// Can't use http.Error after headers sent
				errPayload, _ := json.Marshal(map[string]any{
					"type":  "error",
					"error": err.Error(),
				})
				fmt.Fprintf(w, "data: %s\n\n", errPayload)
				flusher.Flush()
				return
			}

			// Send final done event
			finalPayload, _ := json.Marshal(map[string]any{
				"done": true,
			})
			fmt.Fprintf(w, "data: %s\n\n", finalPayload)
			flusher.Flush()
			return
		}

		// Non-streaming path
		reply, err = (&executor.OllamaExecutor{Model: modelOverride, Log: logf, Verbosity: verbosity}).Execute(messages)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"reply": reply}
		if warning != "" {
			resp["warning"] = warning
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func handleLegacyChat(historyStore *store.PostgresStore) http.HandlerFunc {
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
			var agentNamePtr *string
			var verbosityPtr *int
			if msg.Role == "assistant" {
				agentNamePtr = &agentName
				verbosityPtr = &verbosity
			}
			messages = append(messages, store.Message{
				Role:      msg.Role,
				Content:   msg.Content,
				Agent:     agentNamePtr,
				Verbosity: verbosityPtr,
				Time:      now,
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

func handleChat(historyStore *store.PostgresStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Context   string         `json:"context"`
			Agent     string         `json:"agent"`
			Verbosity *int           `json:"verbosity"`
			Messages  []chat.Message `json:"messages"`
			Stream    bool           `json:"stream"`
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
		defaultAgent := "default"
		defaultVerbosity := executor.DefaultVerbosity()

		contextMeta, hasContext, err := historyStore.GetContextMeta(contextName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		agentName := strings.TrimSpace(req.Agent)
		if agentName == "" {
			if hasContext && strings.TrimSpace(contextMeta.Agent) != "" {
				agentName = contextMeta.Agent
			} else {
				agentName = defaultAgent
			}
		}

		warning := ""
		profile := agent.GetProfile(agentName)
		if profile == nil {
			warning = fmt.Sprintf("agent %q not found; falling back to %q", agentName, defaultAgent)
			agentName = defaultAgent
			profile = agent.GetProfile(agentName)
		}
		verbosityInput := req.Verbosity
		if verbosityInput == nil && hasContext {
			verbosityInput = &contextMeta.Verbosity
		}
		lastUserMessage := latestUserMessage(req.Messages)
		verbosity, verbosityWarning, err := executor.ResolveVerbosity(r.Context(), verbosityInput, defaultVerbosity, agentName, lastUserMessage, historyStore)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		warning = joinWarnings(warning, verbosityWarning)

		systemPrompt := ""
		model := ""
		if profile != nil {
			systemPrompt = profile.SystemPrompt
			model = profile.LocalModel
		}
		if constraint := executor.SystemConstraint(verbosity); constraint != "" {
			if systemPrompt != "" {
				systemPrompt = systemPrompt + "\n\n" + constraint
			} else {
				systemPrompt = constraint
			}
		}

		ctxHist, err := historyStore.LoadContext(contextName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		execMessages := buildChatMessages(systemPrompt, ctxHist.Messages, req.Messages)

		var reply string

		if req.Stream {
			// Streaming path: send events in real-time
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "streaming not supported", http.StatusInternalServerError)
				return
			}

			// Set SSE headers immediately
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			// Send escalation info event immediately if it occurred
			if warning != "" {
				infoPayload, _ := json.Marshal(map[string]any{
					"type":    "info",
					"message": warning,
				})
				fmt.Fprintf(w, "data: %s\n\n", infoPayload)
				flusher.Flush()
			}

			// Stream tokens as they arrive from Ollama
			onDelta := func(delta string) error {
				deltaPayload, err := json.Marshal(map[string]any{
					"delta": delta,
				})
				if err != nil {
					return err
				}
				fmt.Fprintf(w, "data: %s\n\n", deltaPayload)
				flusher.Flush()
				return nil
			}

			reply, err = (&executor.OllamaExecutor{Model: model, Verbosity: verbosity}).ExecuteStreaming(execMessages, onDelta)
			if err != nil {
				// Can't use http.Error after headers sent
				errPayload, _ := json.Marshal(map[string]any{
					"type":  "error",
					"error": err.Error(),
				})
				fmt.Fprintf(w, "data: %s\n\n", errPayload)
				flusher.Flush()
				return
			}

			// Persist to DB after streaming completes
			userTime := time.Now().UTC()
			stored := make([]store.Message, 0, len(req.Messages)+1)
			for _, msg := range req.Messages {
				var agentNamePtr *string
				var verbosityPtr *int
				if msg.Role == "assistant" {
					agentNamePtr = &agentName
					verbosityPtr = &verbosity
				}
				stored = append(stored, store.Message{
					Role:      msg.Role,
					Content:   msg.Content,
					Agent:     agentNamePtr,
					Verbosity: verbosityPtr,
					Time:      userTime,
				})
			}
			assistantTime := time.Now().UTC()
			stored = append(stored, store.Message{
				Role:      "assistant",
				Content:   reply,
				Agent:     &agentName,
				Verbosity: &verbosity,
				Time:      assistantTime,
			})

			if err := historyStore.AppendMessagesWithMeta(contextName, agentName, verbosity, stored); err != nil {
				// Log error but can't return it after headers sent
				fmt.Fprintf(os.Stderr, "[sidekick] failed to persist messages: %v\n", err)
			}

			// Send final done event
			finalPayload, _ := json.Marshal(map[string]any{
				"done": true,
				"context": map[string]any{
					"name":      contextName,
					"agent":     agentName,
					"verbosity": verbosity,
				},
			})
			fmt.Fprintf(w, "data: %s\n\n", finalPayload)
			flusher.Flush()
			return
		}

		// Non-streaming path
		reply, err = (&executor.OllamaExecutor{Model: model, Verbosity: verbosity}).Execute(execMessages)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		userTime := time.Now().UTC()
		stored := make([]store.Message, 0, len(req.Messages)+1)
		for _, msg := range req.Messages {
			var agentNamePtr *string
			var verbosityPtr *int
			if msg.Role == "assistant" {
				agentNamePtr = &agentName
				verbosityPtr = &verbosity
			}
			stored = append(stored, store.Message{
				Role:      msg.Role,
				Content:   msg.Content,
				Agent:     agentNamePtr,
				Verbosity: verbosityPtr,
				Time:      userTime,
			})
		}
		assistantTime := time.Now().UTC()
		stored = append(stored, store.Message{
			Role:      "assistant",
			Content:   reply,
			Agent:     &agentName,
			Verbosity: &verbosity,
			Time:      assistantTime,
		})

		if err := historyStore.AppendMessagesWithMeta(contextName, agentName, verbosity, stored); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		type contextResponse struct {
			Name      string `json:"name"`
			Agent     string `json:"agent"`
			Verbosity int    `json:"verbosity"`
		}
		type response struct {
			Reply   string          `json:"reply"`
			Context contextResponse `json:"context"`
			Warning string          `json:"warning,omitempty"`
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response{
			Reply: reply,
			Context: contextResponse{
				Name:      contextName,
				Agent:     agentName,
				Verbosity: verbosity,
			},
			Warning: warning,
		})
	}
}

func handleAPIAgents(agentRepo agent.AgentRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if agentRepo == nil {
			http.Error(w, "agent repository not configured", http.StatusInternalServerError)
			return
		}

		switch r.Method {
		case http.MethodGet:
			enabledOnly := false
			if v := strings.TrimSpace(r.URL.Query().Get("enabled")); v != "" {
				if v == "true" {
					enabledOnly = true
				} else if v != "false" {
					http.Error(w, "enabled must be 'true' or 'false'", http.StatusBadRequest)
					return
				}
			}

			var (
				agents []*agent.AgentRecord
				err    error
			)
			if enabledOnly {
				agents, err = agentRepo.ListEnabled()
			} else {
				agents, err = agentRepo.List()
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			type agentResponse struct {
				ID               string  `json:"id"`
				Name             string  `json:"name"`
				BaseAgent        *string `json:"base_agent"`
				Model            string  `json:"model"`
				SystemPrompt     string  `json:"system_prompt"`
				DefaultVerbosity int     `json:"default_verbosity"`
				Enabled          bool    `json:"enabled"`
				Revision         int     `json:"revision"`
				UpdatedAt        string  `json:"updated_at"`
			}

			response := make([]agentResponse, 0, len(agents))
			for _, a := range agents {
				response = append(response, agentResponse{
					ID:               a.ID,
					Name:             a.Name,
					BaseAgent:        a.BaseAgent,
					Model:            a.Model,
					SystemPrompt:     a.SystemPrompt,
					DefaultVerbosity: a.DefaultVerbosity,
					Enabled:          a.Enabled,
					Revision:         a.Revision,
					UpdatedAt:        a.UpdatedAt.UTC().Format(time.RFC3339),
				})
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		case http.MethodPost:
			var input struct {
				ID               string  `json:"id"`
				Name             string  `json:"name"`
				BaseAgent        *string `json:"base_agent"`
				Model            string  `json:"model"`
				SystemPrompt     string  `json:"system_prompt"`
				DefaultVerbosity *int    `json:"default_verbosity"`
				Enabled          *bool   `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}

			existing, err := agentRepo.Get(input.ID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if existing != nil {
				http.Error(w, "agent already exists", http.StatusConflict)
				return
			}

			verbosity := 2
			if input.DefaultVerbosity != nil {
				verbosity = *input.DefaultVerbosity
			}
			enabled := true
			if input.Enabled != nil {
				enabled = *input.Enabled
			}

			newAgent := &agent.AgentRecord{
				ID:               input.ID,
				Name:             input.Name,
				BaseAgent:        input.BaseAgent,
				Model:            input.Model,
				SystemPrompt:     input.SystemPrompt,
				DefaultVerbosity: verbosity,
				Enabled:          enabled,
			}

			if err := agentRepo.Create(newAgent); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                newAgent.ID,
				"name":              newAgent.Name,
				"base_agent":        newAgent.BaseAgent,
				"model":             newAgent.Model,
				"system_prompt":     newAgent.SystemPrompt,
				"default_verbosity": newAgent.DefaultVerbosity,
				"enabled":           newAgent.Enabled,
				"revision":          newAgent.Revision,
				"updated_at":        newAgent.UpdatedAt.UTC().Format(time.RFC3339),
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func handleAPIAgent(agentRepo agent.AgentRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if agentRepo == nil {
			http.Error(w, "agent repository not configured", http.StatusInternalServerError)
			return
		}

		id := strings.TrimPrefix(r.URL.Path, "/api/agents/")
		id = strings.TrimSuffix(id, "/")
		if id == "" {
			http.Error(w, "agent id required", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			a, err := agentRepo.Get(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if a == nil {
				http.Error(w, "agent not found", http.StatusNotFound)
				return
			}

			response := map[string]any{
				"id":                a.ID,
				"name":              a.Name,
				"base_agent":        a.BaseAgent,
				"model":             a.Model,
				"system_prompt":     a.SystemPrompt,
				"default_verbosity": a.DefaultVerbosity,
				"enabled":           a.Enabled,
				"revision":          a.Revision,
				"updated_at":        a.UpdatedAt.UTC().Format(time.RFC3339),
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		case http.MethodPatch:
			var input map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}

			existing, err := agentRepo.Get(id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if existing == nil {
				http.Error(w, "agent not found", http.StatusNotFound)
				return
			}

			if name, ok := input["name"]; ok {
				val, ok := name.(string)
				if !ok {
					http.Error(w, "name must be a string", http.StatusBadRequest)
					return
				}
				existing.Name = val
			}
			if model, ok := input["model"]; ok {
				val, ok := model.(string)
				if !ok {
					http.Error(w, "model must be a string", http.StatusBadRequest)
					return
				}
				existing.Model = val
			}
			if prompt, ok := input["system_prompt"]; ok {
				val, ok := prompt.(string)
				if !ok {
					http.Error(w, "system_prompt must be a string", http.StatusBadRequest)
					return
				}
				existing.SystemPrompt = val
			}
			if verbosity, ok := input["default_verbosity"]; ok {
				val, ok := verbosity.(float64)
				if !ok {
					http.Error(w, "default_verbosity must be a number", http.StatusBadRequest)
					return
				}
				existing.DefaultVerbosity = int(val)
			}
			if enabled, ok := input["enabled"]; ok {
				val, ok := enabled.(bool)
				if !ok {
					http.Error(w, "enabled must be a boolean", http.StatusBadRequest)
					return
				}
				existing.Enabled = val
			}
			if baseAgent, ok := input["base_agent"]; ok {
				if baseAgent == nil {
					existing.BaseAgent = nil
				} else {
					val, ok := baseAgent.(string)
					if !ok {
						http.Error(w, "base_agent must be a string or null", http.StatusBadRequest)
						return
					}
					existing.BaseAgent = &val
				}
			}

			if err := agentRepo.Update(existing); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                existing.ID,
				"name":              existing.Name,
				"base_agent":        existing.BaseAgent,
				"model":             existing.Model,
				"system_prompt":     existing.SystemPrompt,
				"default_verbosity": existing.DefaultVerbosity,
				"enabled":           existing.Enabled,
				"revision":          existing.Revision,
				"updated_at":        existing.UpdatedAt.UTC().Format(time.RFC3339),
			})
		case http.MethodDelete:
			if id == "default" {
				http.Error(w, "cannot delete default agent", http.StatusBadRequest)
				return
			}
			if err := agentRepo.Delete(id); err != nil {
				if strings.HasPrefix(err.Error(), "agent not found") {
					http.Error(w, "agent not found", http.StatusNotFound)
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
			Name         string `json:"name"`
			MessageCount int    `json:"message_count"`
			Agent        string `json:"agent"`
			Verbosity    int    `json:"verbosity"`
		}

		response := make([]contextResponse, 0, len(contexts))
		for _, ctx := range contexts {
			response = append(response, contextResponse{
				Name:         ctx.Name,
				MessageCount: ctx.MessageCount,
				Agent:        ctx.Agent,
				Verbosity:    ctx.Verbosity,
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
			if req.Verbosity != nil && (*req.Verbosity < 0 || *req.Verbosity > 5) {
				http.Error(w, "verbosity must be between 0 and 5", http.StatusBadRequest)
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

		type contextResponse struct {
			Name         string `json:"name"`
			MessageCount int    `json:"message_count"`
			Agent        string `json:"agent"`
			Verbosity    int    `json:"verbosity"`
		}

		response := make([]contextResponse, 0, len(contexts))
		for _, ctx := range contexts {
			response = append(response, contextResponse{
				Name:         ctx.Name,
				MessageCount: ctx.MessageCount,
				Agent:        ctx.Agent,
				Verbosity:    ctx.Verbosity,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}
}

func handleContextRoutes(historyStore *store.PostgresStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/contexts/")
		path = strings.TrimSuffix(path, "/")
		if path == "" {
			http.Error(w, "context name required", http.StatusBadRequest)
			return
		}

		if strings.HasSuffix(path, "/messages") {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			contextName := strings.TrimSuffix(path, "/messages")
			contextName = strings.TrimSuffix(contextName, "/")
			if contextName == "" {
				http.Error(w, "context name required", http.StatusBadRequest)
				return
			}

			ctxHist, err := historyStore.LoadContext(contextName)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if ctxHist.System == "" && len(ctxHist.Messages) == 0 {
				http.Error(w, "context not found", http.StatusNotFound)
				return
			}

			type messageResponse struct {
				Role      string  `json:"role"`
				Content   string  `json:"content"`
				Agent     *string `json:"agent,omitempty"`
				Verbosity *int    `json:"verbosity,omitempty"`
				CreatedAt string  `json:"created_at"`
			}

			response := make([]messageResponse, 0, len(ctxHist.Messages))
			for _, msg := range ctxHist.Messages {
				var agentName *string
				var verbosity *int
				if msg.Role == "assistant" {
					agentName = msg.Agent
					verbosity = msg.Verbosity
				}
				response = append(response, messageResponse{
					Role:      msg.Role,
					Content:   msg.Content,
					Agent:     agentName,
					Verbosity: verbosity,
					CreatedAt: msg.Time.UTC().Format(time.RFC3339),
				})
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
			return
		}

		contextName := path
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
			if req.Verbosity != nil && (*req.Verbosity < 0 || *req.Verbosity > 5) {
				http.Error(w, "verbosity must be between 0 and 5", http.StatusBadRequest)
				return
			}

			updated, err := historyStore.UpdateContext(contextName, req.Name, req.Agent, req.Verbosity)
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
			if err := historyStore.DeleteContext(contextName); err != nil {
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

func handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	defaultAgent := "default"
	defaultVerbosity := executor.DefaultVerbosity()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"default_agent":     defaultAgent,
		"default_verbosity": defaultVerbosity,
	})
}

func buildChatMessages(system string, history []store.Message, incoming []chat.Message) []chat.Message {
	messages := make([]chat.Message, 0, len(history)+len(incoming)+1)
	if system != "" {
		messages = append(messages, chat.Message{Role: "system", Content: system})
	}
	for _, msg := range history {
		messages = append(messages, chat.Message{Role: msg.Role, Content: msg.Content})
	}
	messages = append(messages, incoming...)
	return messages
}

func normalizeVerbosity(input *int, defaultLevel int) (int, string) {
	if input == nil {
		return defaultLevel, ""
	}
	value, clamped := executor.ClampVerbosity(*input)
	if !clamped {
		return value, ""
	}
	return value, fmt.Sprintf("verbosity %d clamped to %d", *input, value)
}

func joinWarnings(existing, next string) string {
	if existing == "" {
		return next
	}
	if next == "" {
		return existing
	}
	return existing + "; " + next
}

func latestUserMessage(messages []chat.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func applyVerbosityConstraint(messages []chat.Message, verbosity int) []chat.Message {
	constraint := executor.SystemConstraint(verbosity)
	if constraint == "" {
		return messages
	}

	out := make([]chat.Message, len(messages))
	copy(out, messages)
	for i := range out {
		if out[i].Role != "system" {
			continue
		}
		if strings.Contains(out[i].Content, constraint) {
			return out
		}
		if out[i].Content != "" {
			out[i].Content = out[i].Content + "\n\n" + constraint
		} else {
			out[i].Content = constraint
		}
		return out
	}

	return append([]chat.Message{{Role: "system", Content: constraint}}, out...)
}

