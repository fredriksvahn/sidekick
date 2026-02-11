package agent

import (
	"database/sql"
	"fmt"
	"time"
)

// PostgresRepository handles agent CRUD operations against Postgres.
// Used by the API server. CLI uses Repository (SQLite).
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository creates a new Postgres-backed agent repository.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// InitSchema creates the agents table if it doesn't exist, and adds any
// columns missing from older versions of the table.
func (r *PostgresRepository) InitSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		base_agent TEXT,
		model TEXT NOT NULL,
		system_prompt TEXT NOT NULL DEFAULT '',
		default_verbosity INTEGER NOT NULL DEFAULT 2 CHECK(default_verbosity >= 0 AND default_verbosity <= 4),
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		revision INTEGER NOT NULL DEFAULT 1,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Migrate existing tables that predate these columns.
	ALTER TABLE agents ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT TRUE;
	ALTER TABLE agents ADD COLUMN IF NOT EXISTS revision INTEGER NOT NULL DEFAULT 1;
	ALTER TABLE agents ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

	CREATE INDEX IF NOT EXISTS idx_agents_enabled ON agents(enabled);
	CREATE INDEX IF NOT EXISTS idx_agents_name ON agents(name);
	`
	_, err := r.db.Exec(schema)
	return err
}

// Create inserts a new agent. Sets revision=1 and updated_at=now.
func (r *PostgresRepository) Create(agent *AgentRecord) error {
	if err := validateAgent(agent); err != nil {
		return err
	}

	agent.Revision = 1
	agent.UpdatedAt = time.Now().UTC()

	query := `
	INSERT INTO agents (id, name, base_agent, model, system_prompt, default_verbosity, enabled, revision, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
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
func (r *PostgresRepository) Update(agent *AgentRecord) error {
	if err := validateAgent(agent); err != nil {
		return err
	}

	agent.Revision++
	agent.UpdatedAt = time.Now().UTC()

	query := `
	UPDATE agents
	SET name = $1, base_agent = $2, model = $3, system_prompt = $4,
	    default_verbosity = $5, enabled = $6, revision = $7, updated_at = $8
	WHERE id = $9
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
func (r *PostgresRepository) Get(id string) (*AgentRecord, error) {
	query := `
	SELECT id, name, base_agent, model, system_prompt, default_verbosity, enabled, revision, updated_at
	FROM agents
	WHERE id = $1
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
func (r *PostgresRepository) List() ([]*AgentRecord, error) {
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
func (r *PostgresRepository) ListEnabled() ([]*AgentRecord, error) {
	query := `
	SELECT id, name, base_agent, model, system_prompt, default_verbosity, enabled, revision, updated_at
	FROM agents
	WHERE enabled = TRUE
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
func (r *PostgresRepository) Delete(id string) error {
	query := `DELETE FROM agents WHERE id = $1`
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
