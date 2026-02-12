package store

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

type PostgresStore struct {
	db *sql.DB
}

// CLI_DEFAULT_USER_ID is used for CLI operations that don't have multi-user authentication
const CLI_DEFAULT_USER_ID = "00000000-0000-0000-0000-000000000000"

// NewPostgresStore creates a Postgres-backed store with the given DSN
func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	// Run schema initialization
	if err := initPostgresSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &PostgresStore{db: db}, nil
}

func initPostgresSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS contexts (
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		system_prompt TEXT,
		agent TEXT,
		verbosity INTEGER DEFAULT 2,
		deleted_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, name)
	);

	CREATE TABLE IF NOT EXISTS messages (
		id BIGSERIAL PRIMARY KEY,
		user_id TEXT NOT NULL,
		context_name TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		agent TEXT,
		verbosity INTEGER,
		created_at TIMESTAMPTZ NOT NULL,
		FOREIGN KEY (user_id, context_name) REFERENCES contexts(user_id, name) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_messages_user_context ON messages(user_id, context_name);
	CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);

	CREATE TABLE IF NOT EXISTS verbosity_escalation_keywords (
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		keyword TEXT NOT NULL,
		agent TEXT,
		min_requested_verbosity INTEGER NOT NULL DEFAULT 0,
		escalate_to INTEGER NOT NULL DEFAULT 2,
		enabled BOOLEAN NOT NULL DEFAULT true,
		created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, keyword)
	);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Migrate old single-tenant data if it exists
	// This is safe to run on fresh databases (no-op if tables don't exist yet)
	_, err := db.Exec(`
		DO $$
		BEGIN
			-- Check if old single-tenant contexts table exists and has data
			IF EXISTS (
				SELECT FROM information_schema.columns
				WHERE table_name = 'contexts'
				AND column_name = 'name'
				AND table_schema = 'public'
			) AND NOT EXISTS (
				SELECT FROM information_schema.columns
				WHERE table_name = 'contexts'
				AND column_name = 'user_id'
				AND table_schema = 'public'
			) THEN
				-- Old schema detected, add user_id column
				ALTER TABLE contexts ADD COLUMN IF NOT EXISTS user_id UUID;
				ALTER TABLE messages ADD COLUMN IF NOT EXISTS user_id UUID;

				-- For migration, we'd need to assign a default user_id
				-- This would require coordination with the bootstrap user creation
			END IF;
		END $$;
	`)
	return err
}

// Close closes the database connection
func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// LoadContext loads a context by name for a specific user.
func (s *PostgresStore) LoadContext(userID, contextName string) (ContextHistory, error) {
	// Load system prompt
	var systemPrompt sql.NullString
	err := s.db.QueryRow(`
		SELECT system_prompt FROM contexts WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL
	`, userID, contextName).Scan(&systemPrompt)
	if err == sql.ErrNoRows {
		return ContextHistory{Messages: []Message{}}, nil
	}
	if err != nil {
		return ContextHistory{}, fmt.Errorf("load system prompt: %w", err)
	}

	// Load all messages
	rows, err := s.db.Query(`
		SELECT role, content, agent, verbosity, created_at
		FROM messages
		WHERE user_id = $1 AND context_name = $2
		ORDER BY created_at ASC, id ASC
	`, userID, contextName)
	if err != nil {
		return ContextHistory{}, fmt.Errorf("load messages: %w", err)
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var msg Message
		var agent sql.NullString
		var verbosity sql.NullInt64
		if err := rows.Scan(&msg.Role, &msg.Content, &agent, &verbosity, &msg.Time); err != nil {
			return ContextHistory{}, fmt.Errorf("scan message: %w", err)
		}
		if agent.Valid {
			agentValue := agent.String
			msg.Agent = &agentValue
		}
		if verbosity.Valid {
			v := int(verbosity.Int64)
			msg.Verbosity = &v
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return ContextHistory{}, fmt.Errorf("iterate messages: %w", err)
	}

	return ContextHistory{
		System:   systemPrompt.String,
		Messages: messages,
	}, nil
}

// SaveContext updates the system prompt for an existing context.
func (s *PostgresStore) SaveContext(userID, contextName string, h ContextHistory) error {
	// Update system prompt
	result, err := s.db.Exec(`
		UPDATE contexts SET system_prompt = $1 WHERE user_id = $2 AND name = $3
	`, h.System, userID, contextName)
	if err != nil {
		return fmt.Errorf("update system prompt: %w", err)
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Load loads the last N messages for a context for a specific user
func (s *PostgresStore) Load(userID, contextName string, limit int) ([]Message, error) {
	if limit <= 0 {
		return []Message{}, nil
	}

	// Check if context exists
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM contexts WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL)`, userID, contextName).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check context exists: %w", err)
	}
	if !exists {
		return []Message{}, nil
	}

	// Load all messages
	rows, err := s.db.Query(`
		SELECT role, content, agent, verbosity, created_at
		FROM messages
		WHERE user_id = $1 AND context_name = $2
		ORDER BY created_at ASC, id ASC
	`, userID, contextName)
	if err != nil {
		return nil, fmt.Errorf("load messages: %w", err)
	}
	defer rows.Close()

	var allMessages []Message
	for rows.Next() {
		var msg Message
		var agent sql.NullString
		var verbosity sql.NullInt64
		if err := rows.Scan(&msg.Role, &msg.Content, &agent, &verbosity, &msg.Time); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if agent.Valid {
			agentValue := agent.String
			msg.Agent = &agentValue
		}
		if verbosity.Valid {
			v := int(verbosity.Int64)
			msg.Verbosity = &v
		}
		allMessages = append(allMessages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	// Return last N messages
	if len(allMessages) > limit {
		allMessages = allMessages[len(allMessages)-limit:]
	}

	return allMessages, nil
}

// Append adds a message to a context for a specific user
func (s *PostgresStore) Append(userID, contextName string, msg Message) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get or create context within transaction
	if err := s.getOrCreateContextTx(tx, userID, contextName); err != nil {
		return fmt.Errorf("get or create context: %w", err)
	}

	if msg.Role == "user" {
		msg.Agent = nil
		msg.Verbosity = nil
	}

	// Insert message with explicit timestamp
	_, err = tx.Exec(`
		INSERT INTO messages (user_id, context_name, role, content, agent, verbosity, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, userID, contextName, msg.Role, msg.Content, msg.Agent, msg.Verbosity, msg.Time)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// AppendMessagesWithMeta appends messages and creates the context implicitly on first write for a specific user.
func (s *PostgresStore) AppendMessagesWithMeta(userID, contextName, agent string, verbosity int, messages []Message) error {
	if len(messages) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Contexts are created implicitly on the first message write (same transaction).
	if _, err := tx.Exec(`
		INSERT INTO contexts (user_id, name, system_prompt, agent, verbosity, deleted_at)
		VALUES ($1, $2, '', $3, $4, NULL)
		ON CONFLICT (user_id, name) DO UPDATE SET
			agent = EXCLUDED.agent,
			verbosity = EXCLUDED.verbosity,
			deleted_at = NULL
	`, userID, contextName, agent, verbosity); err != nil {
		return fmt.Errorf("create context: %w", err)
	}

	for _, msg := range messages {
		if msg.Role == "user" {
			msg.Agent = nil
			msg.Verbosity = nil
		}
		if _, err := tx.Exec(`
			INSERT INTO messages (user_id, context_name, role, content, agent, verbosity, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, userID, contextName, msg.Role, msg.Content, msg.Agent, msg.Verbosity, msg.Time); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// UpdateContext updates context metadata and/or renames the context for a specific user.
// If name is changed, all messages are moved to the new context name.
func (s *PostgresStore) UpdateContext(userID, name string, newName, agent *string, verbosity *int) (ContextInfo, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ContextInfo{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	var currentName string
	var currentAgent sql.NullString
	var currentVerbosity sql.NullInt64
	err = tx.QueryRow(`
		SELECT name, agent, verbosity
		FROM contexts
		WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL
	`, userID, name).Scan(&currentName, &currentAgent, &currentVerbosity)
	if err != nil {
		if err == sql.ErrNoRows {
			return ContextInfo{}, sql.ErrNoRows
		}
		return ContextInfo{}, fmt.Errorf("load context: %w", err)
	}

	updatedName := currentName
	if newName != nil && *newName != "" && *newName != currentName {
		updatedName = *newName
		var exists bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM contexts WHERE user_id = $1 AND name = $2)`, userID, updatedName).Scan(&exists); err != nil {
			return ContextInfo{}, fmt.Errorf("check name exists: %w", err)
		}
		if exists {
			return ContextInfo{}, fmt.Errorf("context already exists")
		}
	}

	updatedAgent := currentAgent.String
	if agent != nil {
		updatedAgent = *agent
	}

	updatedVerbosity := 2
	if currentVerbosity.Valid {
		updatedVerbosity = int(currentVerbosity.Int64)
	}
	if verbosity != nil {
		updatedVerbosity = *verbosity
	}

	if updatedName != currentName {
		// Insert new context row and move messages, then delete old context.
		if _, err := tx.Exec(`
			INSERT INTO contexts (user_id, name, system_prompt, agent, verbosity, deleted_at)
			SELECT user_id, $1, system_prompt, $2, $3, NULL
			FROM contexts
			WHERE user_id = $4 AND name = $5
		`, updatedName, updatedAgent, updatedVerbosity, userID, currentName); err != nil {
			return ContextInfo{}, fmt.Errorf("create renamed context: %w", err)
		}

		if _, err := tx.Exec(`
			UPDATE messages SET context_name = $1 WHERE user_id = $2 AND context_name = $3
		`, updatedName, userID, currentName); err != nil {
			return ContextInfo{}, fmt.Errorf("move messages: %w", err)
		}

		if _, err := tx.Exec(`DELETE FROM contexts WHERE user_id = $1 AND name = $2`, userID, currentName); err != nil {
			return ContextInfo{}, fmt.Errorf("delete old context: %w", err)
		}
	} else {
		if _, err := tx.Exec(`
			UPDATE contexts SET agent = $1, verbosity = $2 WHERE user_id = $3 AND name = $4
		`, updatedAgent, updatedVerbosity, userID, currentName); err != nil {
			return ContextInfo{}, fmt.Errorf("update context: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return ContextInfo{}, fmt.Errorf("commit transaction: %w", err)
	}

	return ContextInfo{
		Name:      updatedName,
		Agent:     updatedAgent,
		Verbosity: updatedVerbosity,
	}, nil
}

// DeleteContext removes a context and all of its messages for a specific user.
func (s *PostgresStore) DeleteContext(userID, name string) error {
	result, err := s.db.Exec(`UPDATE contexts SET deleted_at = NOW() WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL`, userID, name)
	if err != nil {
		return fmt.Errorf("delete context: %w", err)
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// getOrCreateContextTx ensures a context exists within a transaction for a specific user
func (s *PostgresStore) getOrCreateContextTx(tx *sql.Tx, userID, name string) error {
	_, err := tx.Exec(`
		INSERT INTO contexts (user_id, name, system_prompt, deleted_at) VALUES ($1, $2, '', NULL)
		ON CONFLICT (user_id, name) DO UPDATE SET deleted_at = NULL
	`, userID, name)
	if err != nil {
		return fmt.Errorf("create context: %w", err)
	}
	return nil
}

// ListContexts returns information about all contexts for a specific user
func (s *PostgresStore) ListContexts(userID string) ([]ContextInfo, error) {
	rows, err := s.db.Query(`
		SELECT
			c.name,
			COUNT(m.id) as message_count,
			MAX(m.created_at) as last_used,
			la.agent,
			la.verbosity
		FROM contexts c
		JOIN messages m ON c.user_id = m.user_id AND c.name = m.context_name
		JOIN LATERAL (
			SELECT agent, verbosity
			FROM messages
			WHERE user_id = c.user_id AND context_name = c.name AND role = 'assistant'
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		) la ON true
		WHERE c.user_id = $1 AND c.deleted_at IS NULL
		GROUP BY c.name, la.agent, la.verbosity
		ORDER BY c.name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query contexts: %w", err)
	}
	defer rows.Close()

	var contexts []ContextInfo
	for rows.Next() {
		var info ContextInfo
		var lastUsed sql.NullTime
		var agent sql.NullString
		var verbosity sql.NullInt64

		if err := rows.Scan(&info.Name, &info.MessageCount, &lastUsed, &agent, &verbosity); err != nil {
			return nil, fmt.Errorf("scan context info: %w", err)
		}

		if lastUsed.Valid {
			info.LastUsed = lastUsed.Time
		}
		if agent.Valid {
			info.Agent = agent.String
		}
		if verbosity.Valid {
			info.Verbosity = int(verbosity.Int64)
		}

		contexts = append(contexts, info)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contexts: %w", err)
	}

	return contexts, nil
}

// GetContextMeta returns context metadata if the context exists and is not deleted for a specific user.
func (s *PostgresStore) GetContextMeta(userID, name string) (ContextInfo, bool, error) {
	var info ContextInfo
	err := s.db.QueryRow(`
		SELECT name, COALESCE(agent, ''), COALESCE(verbosity, 2)
		FROM contexts
		WHERE user_id = $1 AND name = $2 AND deleted_at IS NULL
	`, userID, name).Scan(&info.Name, &info.Agent, &info.Verbosity)
	if err == sql.ErrNoRows {
		return ContextInfo{}, false, nil
	}
	if err != nil {
		return ContextInfo{}, false, fmt.Errorf("load context meta: %w", err)
	}
	return info, true, nil
}

// CLIPostgresAdapter wraps PostgresStore to implement HistoryStore for CLI use
// It uses CLI_DEFAULT_USER_ID for all operations to maintain single-user CLI compatibility
type CLIPostgresAdapter struct {
	store *PostgresStore
}

// NewCLIPostgresAdapter creates a CLI-compatible wrapper around PostgresStore
func NewCLIPostgresAdapter(store *PostgresStore) *CLIPostgresAdapter {
	return &CLIPostgresAdapter{store: store}
}

func (a *CLIPostgresAdapter) LoadContext(contextName string) (ContextHistory, error) {
	return a.store.LoadContext(CLI_DEFAULT_USER_ID, contextName)
}

func (a *CLIPostgresAdapter) SaveContext(contextName string, h ContextHistory) error {
	return a.store.SaveContext(CLI_DEFAULT_USER_ID, contextName, h)
}

func (a *CLIPostgresAdapter) Load(contextName string, limit int) ([]Message, error) {
	return a.store.Load(CLI_DEFAULT_USER_ID, contextName, limit)
}

func (a *CLIPostgresAdapter) Append(contextName string, msg Message) error {
	return a.store.Append(CLI_DEFAULT_USER_ID, contextName, msg)
}

func (a *CLIPostgresAdapter) ListContexts() ([]ContextInfo, error) {
	return a.store.ListContexts(CLI_DEFAULT_USER_ID)
}
