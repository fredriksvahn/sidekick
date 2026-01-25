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

	_, err := db.Exec(schema)
	return err
}

// Close closes the database connection
func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// LoadContext loads a context by name, creating it if it doesn't exist
func (s *PostgresStore) LoadContext(contextName string) (ContextHistory, error) {
	// Get or create context
	if err := s.getOrCreateContext(contextName); err != nil {
		return ContextHistory{}, fmt.Errorf("get or create context: %w", err)
	}

	// Load system prompt
	var systemPrompt sql.NullString
	err := s.db.QueryRow(`
		SELECT system_prompt FROM contexts WHERE name = $1
	`, contextName).Scan(&systemPrompt)
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

// SaveContext updates the system prompt for a context
func (s *PostgresStore) SaveContext(contextName string, h ContextHistory) error {
	// Get or create context
	if err := s.getOrCreateContext(contextName); err != nil {
		return fmt.Errorf("get or create context: %w", err)
	}

	// Update system prompt
	_, err := s.db.Exec(`
		UPDATE contexts SET system_prompt = $1 WHERE name = $2
	`, h.System, contextName)
	if err != nil {
		return fmt.Errorf("update system prompt: %w", err)
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

// getOrCreateContext ensures a context exists
func (s *PostgresStore) getOrCreateContext(name string) error {
	_, err := s.db.Exec(`
		INSERT INTO contexts (name, system_prompt) VALUES ($1, '')
		ON CONFLICT (name) DO NOTHING
	`, name)
	if err != nil {
		return fmt.Errorf("create context: %w", err)
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
			COUNT(m.id) as message_count,
			MAX(m.created_at) as last_used
		FROM contexts c
		LEFT JOIN messages m ON c.name = m.context_name
		GROUP BY c.name
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

		if err := rows.Scan(&info.Name, &info.MessageCount, &lastUsed); err != nil {
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
