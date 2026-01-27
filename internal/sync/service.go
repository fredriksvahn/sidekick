package sync

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/earlysvahn/sidekick/internal/agent"
	"github.com/earlysvahn/sidekick/internal/db"
	"github.com/earlysvahn/sidekick/internal/store"
)

// SyncResult contains statistics about a sync operation
type SyncResult struct {
	ContextsSynced   int
	MessagesInserted int
	Errors           []string
}

// SyncContexts syncs contexts and messages from source to target store
func SyncContexts(source, target store.HistoryStore, sourceName, targetName string) (*SyncResult, error) {
	result := &SyncResult{}

	// Load all contexts from source
	sourceContexts, err := source.ListContexts()
	if err != nil {
		return nil, fmt.Errorf("failed to list source contexts: %w", err)
	}

	for _, ctxInfo := range sourceContexts {
		// Load full context from source
		sourceCtx, err := source.LoadContext(ctxInfo.Name)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to load context %s: %v", ctxInfo.Name, err))
			continue
		}

		// Upsert context in target (this will create if not exists, or update system prompt)
		if err := target.SaveContext(ctxInfo.Name, sourceCtx); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to save context %s: %v", ctxInfo.Name, err))
			continue
		}

		// Load target context to see what messages already exist
		targetCtx, err := target.LoadContext(ctxInfo.Name)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to load target context %s: %v", ctxInfo.Name, err))
			continue
		}

		// Find messages that don't exist in target
		newMessages := findNewMessages(sourceCtx.Messages, targetCtx.Messages)

		// Insert new messages
		for _, msg := range newMessages {
			if err := target.Append(ctxInfo.Name, msg); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("failed to append message to %s: %v", ctxInfo.Name, err))
				continue
			}
			result.MessagesInserted++
		}

		result.ContextsSynced++
	}

	if len(result.Errors) > 0 {
		return result, fmt.Errorf("sync completed with %d errors", len(result.Errors))
	}

	return result, nil
}

// findNewMessages returns messages from source that don't exist in target
// Messages are matched by: role, content, and created_at timestamp
func findNewMessages(sourceMessages, targetMessages []store.Message) []store.Message {
	// Build a set of existing messages for fast lookup
	existing := make(map[string]bool)
	for _, msg := range targetMessages {
		key := messageKey(msg)
		existing[key] = true
	}

	// Find messages that don't exist in target
	var newMessages []store.Message
	for _, msg := range sourceMessages {
		key := messageKey(msg)
		if !existing[key] {
			newMessages = append(newMessages, msg)
		}
	}

	return newMessages
}

// messageKey generates a unique key for a message based on role, content, and timestamp
func messageKey(msg store.Message) string {
	return fmt.Sprintf("%s|%s|%s", msg.Role, msg.Content, msg.Time.Format(time.RFC3339Nano))
}

// SyncAgents syncs agents between SQLite and Postgres
func SyncAgents(sqliteDB, postgresDB *sql.DB, direction string) error {
	if direction != "push" && direction != "pull" {
		return fmt.Errorf("direction must be 'push' or 'pull', got: %s", direction)
	}

	if direction == "push" {
		return agent.SyncToPostgres(sqliteDB, postgresDB)
	}
	return agent.PullFromPostgres(sqliteDB, postgresDB)
}

// AutoSyncAgents triggers postgres sync if DSN is configured.
// Returns nil if postgres is not configured (no error).
func AutoSyncAgents(sqliteDB *sql.DB) error {
	postgresDB, err := db.OpenPostgres()
	if err != nil {
		if _, ok := err.(*db.PostgresNotConfiguredError); ok {
			return nil // Not configured, skip silently
		}
		return err
	}
	defer postgresDB.Close()

	return agent.SyncToPostgres(sqliteDB, postgresDB)
}
