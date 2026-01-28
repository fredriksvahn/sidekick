package store

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

type PostgresStore struct {
	db *sql.DB
}

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
		name TEXT PRIMARY KEY,
		system_prompt TEXT,
		agent TEXT,
		verbosity INTEGER DEFAULT 2,
		created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS messages (
		id BIGSERIAL PRIMARY KEY,
		context_name TEXT NOT NULL REFERENCES contexts(name) ON DELETE CASCADE,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_messages_context_name ON messages(context_name);
	CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Ensure new columns exist on older databases.
	_, err := db.Exec(`
		ALTER TABLE contexts ADD COLUMN IF NOT EXISTS agent TEXT;
		ALTER TABLE contexts ADD COLUMN IF NOT EXISTS verbosity INTEGER DEFAULT 2;
	`)
	return err
}

// Close closes the database connection
func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// LoadContext loads a context by name.
func (s *PostgresStore) LoadContext(contextName string) (ContextHistory, error) {
	// Load system prompt
	var systemPrompt sql.NullString
	err := s.db.QueryRow(`
		SELECT system_prompt FROM contexts WHERE name = $1
	`, contextName).Scan(&systemPrompt)
	if err == sql.ErrNoRows {
		return ContextHistory{Messages: []Message{}}, nil
	}
	if err != nil {
		return ContextHistory{}, fmt.Errorf("load system prompt: %w", err)
	}

	// Load all messages
	rows, err := s.db.Query(`
		SELECT role, content, created_at
		FROM messages
		WHERE context_name = $1
		ORDER BY created_at ASC
	`, contextName)
	if err != nil {
		return ContextHistory{}, fmt.Errorf("load messages: %w", err)
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.Role, &msg.Content, &msg.Time); err != nil {
			return ContextHistory{}, fmt.Errorf("scan message: %w", err)
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
func (s *PostgresStore) SaveContext(contextName string, h ContextHistory) error {
	// Update system prompt
	result, err := s.db.Exec(`
		UPDATE contexts SET system_prompt = $1 WHERE name = $2
	`, h.System, contextName)
	if err != nil {
		return fmt.Errorf("update system prompt: %w", err)
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Load loads the last N messages for a context
func (s *PostgresStore) Load(contextName string, limit int) ([]Message, error) {
	if limit <= 0 {
		return []Message{}, nil
	}

	// Check if context exists
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM contexts WHERE name = $1)`, contextName).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check context exists: %w", err)
	}
	if !exists {
		return []Message{}, nil
	}

	// Load all messages
	rows, err := s.db.Query(`
		SELECT role, content, created_at
		FROM messages
		WHERE context_name = $1
		ORDER BY created_at ASC
	`, contextName)
	if err != nil {
		return nil, fmt.Errorf("load messages: %w", err)
	}
	defer rows.Close()

	var allMessages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.Role, &msg.Content, &msg.Time); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
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

// Append adds a message to a context
func (s *PostgresStore) Append(contextName string, msg Message) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get or create context within transaction
	if err := s.getOrCreateContextTx(tx, contextName); err != nil {
		return fmt.Errorf("get or create context: %w", err)
	}

	// Insert message with explicit timestamp
	_, err = tx.Exec(`
		INSERT INTO messages (context_name, role, content, created_at)
		VALUES ($1, $2, $3, $4)
	`, contextName, msg.Role, msg.Content, msg.Time)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// AppendMessagesWithMeta appends messages and creates the context implicitly on first write.
func (s *PostgresStore) AppendMessagesWithMeta(contextName, agent string, verbosity int, messages []Message) error {
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
		INSERT INTO contexts (name, system_prompt, agent, verbosity)
		VALUES ($1, '', $2, $3)
		ON CONFLICT (name) DO NOTHING
	`, contextName, agent, verbosity); err != nil {
		return fmt.Errorf("create context: %w", err)
	}

	for _, msg := range messages {
		if _, err := tx.Exec(`
			INSERT INTO messages (context_name, role, content, created_at)
			VALUES ($1, $2, $3, $4)
		`, contextName, msg.Role, msg.Content, msg.Time); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// UpdateContext updates context metadata and/or renames the context.
// If name is changed, all messages are moved to the new context name.
func (s *PostgresStore) UpdateContext(name string, newName, agent *string, verbosity *int) (ContextInfo, error) {
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
		WHERE name = $1
	`, name).Scan(&currentName, &currentAgent, &currentVerbosity)
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
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM contexts WHERE name = $1)`, updatedName).Scan(&exists); err != nil {
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
			INSERT INTO contexts (name, system_prompt, agent, verbosity)
			SELECT $1, system_prompt, $2, $3
			FROM contexts
			WHERE name = $4
		`, updatedName, updatedAgent, updatedVerbosity, currentName); err != nil {
			return ContextInfo{}, fmt.Errorf("create renamed context: %w", err)
		}

		if _, err := tx.Exec(`
			UPDATE messages SET context_name = $1 WHERE context_name = $2
		`, updatedName, currentName); err != nil {
			return ContextInfo{}, fmt.Errorf("move messages: %w", err)
		}

		if _, err := tx.Exec(`DELETE FROM contexts WHERE name = $1`, currentName); err != nil {
			return ContextInfo{}, fmt.Errorf("delete old context: %w", err)
		}
	} else {
		if _, err := tx.Exec(`
			UPDATE contexts SET agent = $1, verbosity = $2 WHERE name = $3
		`, updatedAgent, updatedVerbosity, currentName); err != nil {
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

// DeleteContext removes a context and all of its messages.
func (s *PostgresStore) DeleteContext(name string) error {
	result, err := s.db.Exec(`DELETE FROM contexts WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("delete context: %w", err)
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// getOrCreateContextTx ensures a context exists within a transaction
func (s *PostgresStore) getOrCreateContextTx(tx *sql.Tx, name string) error {
	_, err := tx.Exec(`
		INSERT INTO contexts (name, system_prompt) VALUES ($1, '')
		ON CONFLICT (name) DO NOTHING
	`, name)
	if err != nil {
		return fmt.Errorf("create context: %w", err)
	}
	return nil
}

// ListContexts returns information about all contexts
func (s *PostgresStore) ListContexts() ([]ContextInfo, error) {
	rows, err := s.db.Query(`
		SELECT
			c.name,
			COALESCE(c.agent, ''),
			COALESCE(c.verbosity, 2),
			COUNT(m.id) as message_count,
			MAX(m.created_at) as last_used
		FROM contexts c
		JOIN messages m ON c.name = m.context_name
		GROUP BY c.name, c.agent, c.verbosity
		ORDER BY c.name
	`)
	if err != nil {
		return nil, fmt.Errorf("query contexts: %w", err)
	}
	defer rows.Close()

	var contexts []ContextInfo
	for rows.Next() {
		var info ContextInfo
		var lastUsed sql.NullTime

		if err := rows.Scan(&info.Name, &info.Agent, &info.Verbosity, &info.MessageCount, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan context info: %w", err)
		}

		if lastUsed.Valid {
			info.LastUsed = lastUsed.Time
		}

		contexts = append(contexts, info)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contexts: %w", err)
	}

	return contexts, nil
}
