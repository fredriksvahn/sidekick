package agent

import (
	"database/sql"
	"sync"
)

// Global repository instance (set by main on startup)
var (
	globalRepo *Repository
	repoMu     sync.RWMutex
)

// SetRepository sets the global agent repository.
// Must be called on startup before using GetProfile/ListProfiles.
func SetRepository(repo *Repository) {
	repoMu.Lock()
	defer repoMu.Unlock()
	globalRepo = repo
}

// GetProfile retrieves a profile by name from database.
// Falls back to hardcoded profiles if database is not initialized.
// Returns nil if not found.
func GetProfile(name string) *AgentProfile {
	repoMu.RLock()
	repo := globalRepo
	repoMu.RUnlock()

	// If database is initialized, load from database
	if repo != nil {
		agent, err := repo.Get(name)
		if err == nil && agent != nil && agent.Enabled {
			return agent.ToAgentProfile()
		}
	}

	// Fallback to hardcoded profiles
	if p, ok := Profiles[name]; ok {
		return &p
	}
	return nil
}

// ListProfiles returns all enabled profile names sorted alphabetically.
// Loads from database if initialized, otherwise uses hardcoded profiles.
func ListProfiles() []string {
	repoMu.RLock()
	repo := globalRepo
	repoMu.RUnlock()

	// If database is initialized, load from database
	if repo != nil {
		agents, err := repo.ListEnabled()
		if err == nil && len(agents) > 0 {
			names := make([]string, len(agents))
			for i, agent := range agents {
				names[i] = agent.ID
			}
			return names
		}
	}

	// Fallback to hardcoded profiles
	names := make([]string, 0, len(Profiles))
	for name := range Profiles {
		names = append(names, name)
	}
	// Simple sort
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// MigrateHardcodedAgents populates the database with hardcoded profiles.
// Only inserts agents that don't already exist in the database.
// This is called once on first run to seed the database.
func MigrateHardcodedAgents(db *sql.DB) error {
	repo := NewRepository(db)

	// Ensure schema exists
	if err := repo.InitSchema(); err != nil {
		return err
	}

	// Convert each hardcoded profile to AgentRecord and insert
	for id, profile := range Profiles {
		// Skip profiles with empty models (e.g., "default")
		// These are kept as hardcoded fallbacks only
		if profile.LocalModel == "" {
			continue
		}

		// Check if already exists
		existing, err := repo.Get(id)
		if err != nil {
			return err
		}
		if existing != nil {
			// Already exists, skip
			continue
		}

		// Create new agent record
		agent := &AgentRecord{
			ID:               id,
			Name:             profile.Name,
			BaseAgent:        nil,
			Model:            profile.LocalModel,
			SystemPrompt:     profile.SystemPrompt,
			DefaultVerbosity: profile.DefaultVerbosity,
			Enabled:          true,
		}

		if err := repo.Create(agent); err != nil {
			return err
		}
	}

	return nil
}
