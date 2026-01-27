package agent

import (
	"database/sql"
	"fmt"
)

// SyncToPostgres pushes local SQLite agents to Postgres.
// ONLY overwrites Postgres rows if local revision is newer.
// This is a PUSH-ONLY operation. Postgres is NOT a write target.
//
// Sync guarantees:
// - SQLite is the source of truth
// - Postgres is a mirror/backup
// - Revision-based conflict resolution (last-write-wins)
// - No merge logic, no UI for conflicts
func SyncToPostgres(sqliteDB, postgresDB *sql.DB) error {
	// Ensure Postgres schema exists
	if err := initPostgresSchema(postgresDB); err != nil {
		return fmt.Errorf("init postgres schema: %w", err)
	}

	// Get all agents from SQLite
	repo := NewRepository(sqliteDB)
	agents, err := repo.List()
	if err != nil {
		return fmt.Errorf("list local agents: %w", err)
	}

	// Push each agent to Postgres (upsert with revision check)
	for _, agent := range agents {
		if err := upsertToPostgres(postgresDB, agent); err != nil {
			return fmt.Errorf("upsert agent %s: %w", agent.ID, err)
		}
	}

	return nil
}

// PullFromPostgres optionally pulls agents from Postgres on startup.
// ONLY applies if Postgres revision > local revision.
// This is a PULL-ON-START operation, not continuous sync.
//
// Sync guarantees:
// - Only pulls if Postgres is newer (by revision)
// - No merge logic beyond revision comparison
// - Local changes are never lost (unless Postgres has higher revision)
func PullFromPostgres(sqliteDB, postgresDB *sql.DB) error {
	// Get all agents from Postgres
	pgAgents, err := listFromPostgres(postgresDB)
	if err != nil {
		return fmt.Errorf("list postgres agents: %w", err)
	}

	repo := NewRepository(sqliteDB)

	// For each Postgres agent, check if we should pull it
	for _, pgAgent := range pgAgents {
		localAgent, err := repo.Get(pgAgent.ID)
		if err != nil {
			return fmt.Errorf("get local agent %s: %w", pgAgent.ID, err)
		}

		// If local doesn't exist, create it
		if localAgent == nil {
			if err := repo.Create(pgAgent); err != nil {
				return fmt.Errorf("create agent %s from postgres: %w", pgAgent.ID, err)
			}
			continue
		}

		// If Postgres revision is newer, update local
		if pgAgent.Revision > localAgent.Revision {
			// Preserve local ID but use Postgres data
			pgAgent.ID = localAgent.ID
			if err := repo.Update(pgAgent); err != nil {
				return fmt.Errorf("update agent %s from postgres: %w", pgAgent.ID, err)
			}
		}
	}

	return nil
}

// initPostgresSchema creates the agents table in Postgres if it doesn't exist.
// Schema matches SQLite for compatibility.
func initPostgresSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		base_agent TEXT,
		model TEXT NOT NULL,
		system_prompt TEXT NOT NULL DEFAULT '',
		default_verbosity INTEGER NOT NULL DEFAULT 2 CHECK(default_verbosity >= 0 AND default_verbosity <= 3),
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		revision INTEGER NOT NULL DEFAULT 1,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_agents_enabled ON agents(enabled);
	CREATE INDEX IF NOT EXISTS idx_agents_name ON agents(name);
	`
	_, err := db.Exec(schema)
	return err
}

// upsertToPostgres inserts or updates an agent in Postgres.
// ONLY overwrites if local revision >= Postgres revision.
func upsertToPostgres(db *sql.DB, agent *AgentRecord) error {
	query := `
	INSERT INTO agents (id, name, base_agent, model, system_prompt, default_verbosity, enabled, revision, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	ON CONFLICT (id) DO UPDATE SET
		name = EXCLUDED.name,
		base_agent = EXCLUDED.base_agent,
		model = EXCLUDED.model,
		system_prompt = EXCLUDED.system_prompt,
		default_verbosity = EXCLUDED.default_verbosity,
		enabled = EXCLUDED.enabled,
		revision = EXCLUDED.revision,
		updated_at = EXCLUDED.updated_at
	WHERE EXCLUDED.revision >= agents.revision
	`
	_, err := db.Exec(query,
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

// listFromPostgres retrieves all agents from Postgres.
func listFromPostgres(db *sql.DB) ([]*AgentRecord, error) {
	query := `
	SELECT id, name, base_agent, model, system_prompt, default_verbosity, enabled, revision, updated_at
	FROM agents
	ORDER BY name
	`
	rows, err := db.Query(query)
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
