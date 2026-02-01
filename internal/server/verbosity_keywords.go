package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
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
				Keyword      string  `json:"keyword"`
				Agent        *string `json:"agent,omitempty"`
				MinRequested int     `json:"min_requested"`
				EscalateTo   int     `json:"escalate_to"`
				Enabled      bool    `json:"enabled"`
				CreatedAt    string  `json:"created_at"`
			}

			resp := make([]keywordResponse, 0, len(keywords))
			for _, kw := range keywords {
				resp = append(resp, keywordResponse{
					Keyword:      kw.Keyword,
					Agent:        kw.Agent,
					MinRequested: kw.MinRequested,
					EscalateTo:   kw.EscalateTo,
					Enabled:      kw.Enabled,
					CreatedAt:    kw.CreatedAt.UTC().Format(time.RFC3339),
				})
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case http.MethodPost:
			var input struct {
				Keyword      string  `json:"keyword"`
				Agent        *string `json:"agent,omitempty"`
				MinRequested *int    `json:"min_requested"`
				EscalateTo   *int    `json:"escalate_to"`
				Enabled      *bool   `json:"enabled"`
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

			enabled := true
			if input.Enabled != nil {
				enabled = *input.Enabled
			}

			if *input.EscalateTo < *input.MinRequested {
				http.Error(w, "escalate_to must be >= min_requested", http.StatusBadRequest)
				return
			}

			kw, err := keywordStore.CreateVerbosityKeyword(r.Context(), keyword, input.Agent, *input.MinRequested, *input.EscalateTo, enabled)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			resp := map[string]any{
				"keyword":       kw.Keyword,
				"min_requested": kw.MinRequested,
				"escalate_to":   kw.EscalateTo,
				"enabled":       kw.Enabled,
				"created_at":    kw.CreatedAt.UTC().Format(time.RFC3339),
			}
			if kw.Agent != nil {
				resp["agent"] = *kw.Agent
			}
			_ = json.NewEncoder(w).Encode(resp)
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

		keyword := strings.TrimPrefix(r.URL.Path, "/verbosity/keywords/")
		keyword = strings.TrimSuffix(keyword, "/")
		if keyword == "" {
			http.Error(w, "keyword required", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodPatch:
			var input struct {
				MinRequested *int  `json:"min_requested"`
				EscalateTo   *int  `json:"escalate_to"`
				Enabled      *bool `json:"enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}

			if input.MinRequested == nil && input.EscalateTo == nil && input.Enabled == nil {
				http.Error(w, "no fields to update", http.StatusBadRequest)
				return
			}

			// Fetch existing keyword to validate escalate_to >= min_requested
			keywords, err := keywordStore.ListVerbosityKeywords(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			var existing *store.VerbosityKeyword
			for i := range keywords {
				if keywords[i].Keyword == keyword {
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

			updated, err := keywordStore.UpdateVerbosityKeyword(r.Context(), keyword, store.VerbosityKeywordUpdate{
				MinRequested: input.MinRequested,
				EscalateTo:   input.EscalateTo,
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
				"keyword":       updated.Keyword,
				"min_requested": updated.MinRequested,
				"escalate_to":   updated.EscalateTo,
				"enabled":       updated.Enabled,
				"created_at":    updated.CreatedAt.UTC().Format(time.RFC3339),
			})
		case http.MethodDelete:
			if err := keywordStore.DeleteVerbosityKeyword(r.Context(), keyword); err != nil {
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

