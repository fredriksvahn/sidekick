package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/earlysvahn/sidekick/internal/store"
)

func handleVerbosityKeywords(keywordStore store.VerbosityKeywordStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if keywordStore == nil {
			http.Error(w, "keyword store not configured", http.StatusInternalServerError)
			return
		}

		switch r.Method {
		case http.MethodGet:
			keywords, err := keywordStore.ListVerbosityKeywords(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			type keywordResponse struct {
				ID           int    `json:"id"`
				Keyword      string `json:"keyword"`
				MinRequested int    `json:"min_requested"`
				EscalateTo   int    `json:"escalate_to"`
				Enabled      bool   `json:"enabled"`
				Priority     int    `json:"priority"`
				CreatedAt    string `json:"created_at"`
			}

			resp := make([]keywordResponse, 0, len(keywords))
			for _, kw := range keywords {
				resp = append(resp, keywordResponse{
					ID:           kw.ID,
					Keyword:      kw.Keyword,
					MinRequested: kw.MinRequested,
					EscalateTo:   kw.EscalateTo,
					Enabled:      kw.Enabled,
					Priority:     kw.Priority,
					CreatedAt:    kw.CreatedAt.UTC().Format(time.RFC3339),
				})
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case http.MethodPost:
			var input struct {
				Keyword      string `json:"keyword"`
				MinRequested *int   `json:"min_requested"`
				EscalateTo   *int   `json:"escalate_to"`
				Priority     *int   `json:"priority"`
				Enabled      *bool  `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}

			keyword := strings.TrimSpace(input.Keyword)
			if keyword == "" {
				http.Error(w, "keyword required", http.StatusBadRequest)
				return
			}
			if input.MinRequested == nil {
				http.Error(w, "min_requested required", http.StatusBadRequest)
				return
			}
			if input.EscalateTo == nil {
				http.Error(w, "escalate_to required", http.StatusBadRequest)
				return
			}

			priority := 0
			if input.Priority != nil {
				priority = *input.Priority
			}
			enabled := true
			if input.Enabled != nil {
				enabled = *input.Enabled
			}

			if *input.EscalateTo < *input.MinRequested {
				http.Error(w, "escalate_to must be >= min_requested", http.StatusBadRequest)
				return
			}

			kw, err := keywordStore.CreateVerbosityKeyword(r.Context(), keyword, *input.MinRequested, *input.EscalateTo, priority, enabled)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            kw.ID,
				"keyword":       kw.Keyword,
				"min_requested": kw.MinRequested,
				"escalate_to":   kw.EscalateTo,
				"enabled":       kw.Enabled,
				"priority":      kw.Priority,
				"created_at":    kw.CreatedAt.UTC().Format(time.RFC3339),
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func handleVerbosityKeyword(keywordStore store.VerbosityKeywordStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if keywordStore == nil {
			http.Error(w, "keyword store not configured", http.StatusInternalServerError)
			return
		}

		idStr := strings.TrimPrefix(r.URL.Path, "/verbosity/keywords/")
		idStr = strings.TrimSuffix(idStr, "/")
		if idStr == "" {
			http.Error(w, "keyword id required", http.StatusBadRequest)
			return
		}
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "keyword id must be a number", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodPatch:
			var input struct {
				Keyword      *string `json:"keyword"`
				MinRequested *int    `json:"min_requested"`
				EscalateTo   *int    `json:"escalate_to"`
				Priority     *int    `json:"priority"`
				Enabled      *bool   `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}

			if input.Keyword == nil && input.MinRequested == nil && input.EscalateTo == nil && input.Priority == nil && input.Enabled == nil {
				http.Error(w, "no fields to update", http.StatusBadRequest)
				return
			}

			if input.Keyword != nil {
				trimmed := strings.TrimSpace(*input.Keyword)
				if trimmed == "" {
					http.Error(w, "keyword must be non-empty", http.StatusBadRequest)
					return
				}
				input.Keyword = &trimmed
			}

			keywords, err := keywordStore.ListVerbosityKeywords(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var existing *store.VerbosityKeyword
			for i := range keywords {
				if keywords[i].ID == id {
					existing = &keywords[i]
					break
				}
			}
			if existing == nil {
				http.Error(w, "keyword not found", http.StatusNotFound)
				return
			}

			minRequested := existing.MinRequested
			escalateTo := existing.EscalateTo
			if input.MinRequested != nil {
				minRequested = *input.MinRequested
			}
			if input.EscalateTo != nil {
				escalateTo = *input.EscalateTo
			}
			if escalateTo < minRequested {
				http.Error(w, "escalate_to must be >= min_requested", http.StatusBadRequest)
				return
			}

			updated, err := keywordStore.UpdateVerbosityKeyword(r.Context(), id, store.VerbosityKeywordUpdate{
				Keyword:      input.Keyword,
				MinRequested: input.MinRequested,
				EscalateTo:   input.EscalateTo,
				Priority:     input.Priority,
				Enabled:      input.Enabled,
			})
			if err != nil {
				if err == sql.ErrNoRows {
					http.Error(w, "keyword not found", http.StatusNotFound)
					return
				}
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            updated.ID,
				"keyword":       updated.Keyword,
				"min_requested": updated.MinRequested,
				"escalate_to":   updated.EscalateTo,
				"enabled":       updated.Enabled,
				"priority":      updated.Priority,
				"created_at":    updated.CreatedAt.UTC().Format(time.RFC3339),
			})
		case http.MethodDelete:
			if err := keywordStore.DeleteVerbosityKeyword(r.Context(), id); err != nil {
				if err == sql.ErrNoRows {
					http.Error(w, "keyword not found", http.StatusNotFound)
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

