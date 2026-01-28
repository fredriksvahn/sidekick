package agent

import (
	"database/sql"
	"fmt"
	"time"
)

// AgentRecord represents an agent stored in the database.
// SQLite is the PRIMARY source of truth.
// Postgres is a SYNC TARGET only (push-only, no direct writes).
type AgentRecord struct {
	ID               string    // Stable identifier
	Name             string    // Display name
	BaseAgent        *string   // Optional parent agent to inherit from
	Model            string    // Ollama model name
	SystemPrompt     string    // System prompt text
	DefaultVerbosity int       // 0=minimal, 1=concise, 2=normal, 3=verbose, 4=very verbose
	Enabled          bool      // Whether agent is active
	Revision         int       // Monotonic version counter for sync
	UpdatedAt        time.Time // Last modification timestamp
}

// Repository handles agent CRUD operations against SQLite.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a new agent repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// InitSchema creates the agents table if it doesn't exist.
// SQLite schema - PRIMARY source of truth.
func (r *Repository) InitSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		base_agent TEXT,
		model TEXT NOT NULL,
		system_prompt TEXT NOT NULL DEFAULT '',
		default_verbosity INTEGER NOT NULL DEFAULT 2 CHECK(default_verbosity >= 0 AND default_verbosity <= 4),
		enabled INTEGER NOT NULL DEFAULT 1,
		revision INTEGER NOT NULL DEFAULT 1,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_agents_enabled ON agents(enabled);
	CREATE INDEX IF NOT EXISTS idx_agents_name ON agents(name);
	`
	_, err := r.db.Exec(schema)
	return err
}

// Create inserts a new agent. Sets revision=1 and updated_at=now.
func (r *Repository) Create(agent *AgentRecord) error {
	// Validate
	if err := validateAgent(agent); err != nil {
		return err
	}

	agent.Revision = 1
	agent.UpdatedAt = time.Now().UTC()

	query := `
	INSERT INTO agents (id, name, base_agent, model, system_prompt, default_verbosity, enabled, revision, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := r.db.Exec(query,
		agent.ID,
		agent.Name,
		agent.BaseAgent,
		agent.Model,
		agent.SystemPrompt,
		agent.DefaultVerbosity,
		agent.Enabled,
		agent.Revision,
		agent.UpdatedAt,
	)
	return err
}

// Update modifies an existing agent. Increments revision and updates timestamp.
func (r *Repository) Update(agent *AgentRecord) error {
	// Validate
	if err := validateAgent(agent); err != nil {
		return err
	}

	// Increment revision and update timestamp
	agent.Revision++
	agent.UpdatedAt = time.Now().UTC()

	query := `
	UPDATE agents
	SET name = ?, base_agent = ?, model = ?, system_prompt = ?,
	    default_verbosity = ?, enabled = ?, revision = ?, updated_at = ?
	WHERE id = ?
	`
	result, err := r.db.Exec(query,
		agent.Name,
		agent.BaseAgent,
		agent.Model,
		agent.SystemPrompt,
		agent.DefaultVerbosity,
		agent.Enabled,
		agent.Revision,
		agent.UpdatedAt,
		agent.ID,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("agent not found: %s", agent.ID)
	}

	return nil
}

// Get retrieves an agent by ID.
func (r *Repository) Get(id string) (*AgentRecord, error) {
	query := `
	SELECT id, name, base_agent, model, system_prompt, default_verbosity, enabled, revision, updated_at
	FROM agents
	WHERE id = ?
	`
	agent := &AgentRecord{}
	err := r.db.QueryRow(query, id).Scan(
		&agent.ID,
		&agent.Name,
		&agent.BaseAgent,
		&agent.Model,
		&agent.SystemPrompt,
		&agent.DefaultVerbosity,
		&agent.Enabled,
		&agent.Revision,
		&agent.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return agent, nil
}

// List returns all agents (enabled or disabled).
func (r *Repository) List() ([]*AgentRecord, error) {
	query := `
	SELECT id, name, base_agent, model, system_prompt, default_verbosity, enabled, revision, updated_at
	FROM agents
	ORDER BY name
	`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*AgentRecord
	for rows.Next() {
		agent := &AgentRecord{}
		err := rows.Scan(
			&agent.ID,
			&agent.Name,
			&agent.BaseAgent,
			&agent.Model,
			&agent.SystemPrompt,
			&agent.DefaultVerbosity,
			&agent.Enabled,
			&agent.Revision,
			&agent.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

// ListEnabled returns only enabled agents.
func (r *Repository) ListEnabled() ([]*AgentRecord, error) {
	query := `
	SELECT id, name, base_agent, model, system_prompt, default_verbosity, enabled, revision, updated_at
	FROM agents
	WHERE enabled = 1
	ORDER BY name
	`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*AgentRecord
	for rows.Next() {
		agent := &AgentRecord{}
		err := rows.Scan(
			&agent.ID,
			&agent.Name,
			&agent.BaseAgent,
			&agent.Model,
			&agent.SystemPrompt,
			&agent.DefaultVerbosity,
			&agent.Enabled,
			&agent.Revision,
			&agent.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

// Delete removes an agent by ID.
func (r *Repository) Delete(id string) error {
	query := `DELETE FROM agents WHERE id = ?`
	result, err := r.db.Exec(query, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("agent not found: %s", id)
	}

	return nil
}

// validateAgent checks required fields and constraints.
func validateAgent(agent *AgentRecord) error {
	if agent.ID == "" {
		return fmt.Errorf("agent ID is required")
	}
	if agent.Name == "" {
		return fmt.Errorf("agent name is required")
	}
	if agent.Model == "" {
		return fmt.Errorf("agent model is required")
	}
	if agent.DefaultVerbosity < 0 || agent.DefaultVerbosity > 4 {
		return fmt.Errorf("default verbosity must be 0-4, got %d", agent.DefaultVerbosity)
	}
	return nil
}

// ToAgentProfile converts an AgentRecord to an AgentProfile for runtime use.
func (a *AgentRecord) ToAgentProfile() *AgentProfile {
	return &AgentProfile{
		Name:             a.Name,
		LocalModel:       a.Model,
		RemoteModel:      a.Model, // For now, same model for local/remote
		SystemPrompt:     a.SystemPrompt,
		DefaultVerbosity: a.DefaultVerbosity,
	}
}
