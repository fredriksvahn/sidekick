package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteTimeFormat = "2006-01-02 15:04:05"

type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a SQLite-backed store at the given path
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable foreign keys (not enabled by default in SQLite)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	// Run schema migrations
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS contexts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		system_prompt TEXT,
		agent TEXT,
		verbosity INTEGER DEFAULT 2,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		context_id INTEGER NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (context_id) REFERENCES contexts(id)
	);

	CREATE INDEX IF NOT EXISTS idx_messages_context_id ON messages(context_id);
	CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);
	`

	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	if err := ensureContextColumn(db, "agent", "TEXT"); err != nil {
		return err
	}
	if err := ensureContextColumn(db, "verbosity", "INTEGER DEFAULT 2"); err != nil {
		return err
	}

	return nil
}

func ensureContextColumn(db *sql.DB, name, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(contexts)`)
	if err != nil {
		return fmt.Errorf("inspect contexts table: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var colName string
		var colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &colName, &colType, &notNull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("scan contexts columns: %w", err)
		}
		if colName == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate contexts columns: %w", err)
	}

	if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE contexts ADD COLUMN %s %s`, name, definition)); err != nil {
		return fmt.Errorf("add contexts.%s: %w", name, err)
	}
	return nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// LoadContext loads a context by name, creating it if it doesn't exist
func (s *SQLiteStore) LoadContext(contextName string) (ContextHistory, error) {
	// Get or create context
	contextID, err := s.getOrCreateContext(contextName)
	if err != nil {
		return ContextHistory{}, fmt.Errorf("get or create context: %w", err)
	}

	// Load system prompt
	var systemPrompt sql.NullString
	err = s.db.QueryRow(`
		SELECT system_prompt FROM contexts WHERE id = ?
	`, contextID).Scan(&systemPrompt)
	if err != nil {
		return ContextHistory{}, fmt.Errorf("load system prompt: %w", err)
	}

	// Load all messages
	rows, err := s.db.Query(`
		SELECT role, content, created_at
		FROM messages
		WHERE context_id = ?
		ORDER BY created_at ASC
	`, contextID)
	if err != nil {
		return ContextHistory{}, fmt.Errorf("load messages: %w", err)
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var msg Message
		var createdAt string
		if err := rows.Scan(&msg.Role, &msg.Content, &createdAt); err != nil {
			return ContextHistory{}, fmt.Errorf("scan message: %w", err)
		}

		msg.Time, err = parseTimestamp(createdAt)
		if err != nil {
			return ContextHistory{}, fmt.Errorf("parse timestamp: %w", err)
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
func (s *SQLiteStore) SaveContext(contextName string, h ContextHistory) error {
	// Get or create context
	contextID, err := s.getOrCreateContext(contextName)
	if err != nil {
		return fmt.Errorf("get or create context: %w", err)
	}

	// Update system prompt
	_, err = s.db.Exec(`
		UPDATE contexts SET system_prompt = ? WHERE id = ?
	`, h.System, contextID)
	if err != nil {
		return fmt.Errorf("update system prompt: %w", err)
	}

	return nil
}

// Load loads the last N messages for a context
func (s *SQLiteStore) Load(contextName string, limit int) ([]Message, error) {
	if limit <= 0 {
		return []Message{}, nil
	}

	// Get context ID
	var contextID int64
	err := s.db.QueryRow(`SELECT id FROM contexts WHERE name = ?`, contextName).Scan(&contextID)
	if err == sql.ErrNoRows {
		return []Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get context id: %w", err)
	}

	// Load all messages
	rows, err := s.db.Query(`
		SELECT role, content, created_at
		FROM messages
		WHERE context_id = ?
		ORDER BY created_at ASC
	`, contextID)
	if err != nil {
		return nil, fmt.Errorf("load messages: %w", err)
	}
	defer rows.Close()

	var allMessages []Message
	for rows.Next() {
		var msg Message
		var createdAt string
		if err := rows.Scan(&msg.Role, &msg.Content, &createdAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}

		msg.Time, err = parseTimestamp(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse timestamp: %w", err)
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
func (s *SQLiteStore) Append(contextName string, msg Message) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get or create context within transaction
	contextID, err := s.getOrCreateContextTx(tx, contextName)
	if err != nil {
		return fmt.Errorf("get or create context: %w", err)
	}

	// Insert message with explicit timestamp
	_, err = tx.Exec(`
		INSERT INTO messages (context_id, role, content, created_at)
		VALUES (?, ?, ?, ?)
	`, contextID, msg.Role, msg.Content, msg.Time.Format(sqliteTimeFormat))
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// getOrCreateContext ensures a context exists and returns its ID
func (s *SQLiteStore) getOrCreateContext(name string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM contexts WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("query context: %w", err)
	}

	result, err := s.db.Exec(`
		INSERT INTO contexts (name, system_prompt) VALUES (?, '')
	`, name)
	if err != nil {
		return 0, fmt.Errorf("create context: %w", err)
	}

	return result.LastInsertId()
}

// getOrCreateContextTx ensures a context exists within a transaction
func (s *SQLiteStore) getOrCreateContextTx(tx *sql.Tx, name string) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM contexts WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("query context: %w", err)
	}

	result, err := tx.Exec(`
		INSERT INTO contexts (name, system_prompt) VALUES (?, '')
	`, name)
	if err != nil {
		return 0, fmt.Errorf("create context: %w", err)
	}

	return result.LastInsertId()
}

// ListContexts returns information about all contexts
func (s *SQLiteStore) ListContexts() ([]ContextInfo, error) {
	rows, err := s.db.Query(`
		SELECT
			c.name,
			COALESCE(c.agent, ''),
			COALESCE(c.verbosity, 2),
			COUNT(m.id) as message_count,
			MAX(m.created_at) as last_used
		FROM contexts c
		JOIN messages m ON c.id = m.context_id
		GROUP BY c.id, c.name, c.agent, c.verbosity
		ORDER BY c.name
	`)
	if err != nil {
		return nil, fmt.Errorf("query contexts: %w", err)
	}
	defer rows.Close()

	var contexts []ContextInfo
	for rows.Next() {
		var info ContextInfo
		var lastUsed sql.NullString

		if err := rows.Scan(&info.Name, &info.Agent, &info.Verbosity, &info.MessageCount, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan context info: %w", err)
		}

		if lastUsed.Valid {
			info.LastUsed, err = parseTimestamp(lastUsed.String)
			if err != nil {
				return nil, fmt.Errorf("parse last used: %w", err)
			}
		}

		contexts = append(contexts, info)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contexts: %w", err)
	}

	return contexts, nil
}

// parseTimestamp handles both SQLite default format and custom formats
func parseTimestamp(s string) (time.Time, error) {
	t, err := time.Parse(sqliteTimeFormat, s)
	if err == nil {
		return t, nil
	}

	t, err = time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %s", s)
}
