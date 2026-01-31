package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type VerbosityKeyword struct {
	ID           int       `json:"id"`
	Keyword      string    `json:"keyword"`
	MinRequested int       `json:"min_requested"`
	EscalateTo   int       `json:"escalate_to"`
	Enabled      bool      `json:"enabled"`
	Priority     int       `json:"priority"`
	CreatedAt    time.Time `json:"created_at"`
}

type VerbosityKeywordUpdate struct {
	Keyword      *string
	MinRequested *int
	EscalateTo   *int
	Enabled      *bool
	Priority     *int
}

type VerbosityKeywordLister interface {
	ListVerbosityKeywords(ctx context.Context) ([]VerbosityKeyword, error)
}

type VerbosityKeywordStore interface {
	VerbosityKeywordLister
	CreateVerbosityKeyword(ctx context.Context, keyword string, minRequested, escalateTo, priority int, enabled bool) (VerbosityKeyword, error)
	UpdateVerbosityKeyword(ctx context.Context, id int, input VerbosityKeywordUpdate) (VerbosityKeyword, error)
	DeleteVerbosityKeyword(ctx context.Context, id int) error
}

type NoopVerbosityKeywordStore struct{}

func (NoopVerbosityKeywordStore) ListVerbosityKeywords(ctx context.Context) ([]VerbosityKeyword, error) {
	return []VerbosityKeyword{}, nil
}

func (s *PostgresStore) ListVerbosityKeywords(ctx context.Context) ([]VerbosityKeyword, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, keyword, min_requested, escalate_to, enabled, priority, created_at
		FROM verbosity_escalation_keywords
		ORDER BY priority DESC, length(keyword) DESC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list verbosity keywords: %w", err)
	}
	defer rows.Close()

	var keywords []VerbosityKeyword
	for rows.Next() {
		var kw VerbosityKeyword
		if err := rows.Scan(&kw.ID, &kw.Keyword, &kw.MinRequested, &kw.EscalateTo, &kw.Enabled, &kw.Priority, &kw.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan verbosity keyword: %w", err)
		}
		keywords = append(keywords, kw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate verbosity keywords: %w", err)
	}
	return keywords, nil
}

func (s *PostgresStore) CreateVerbosityKeyword(ctx context.Context, keyword string, minRequested, escalateTo, priority int, enabled bool) (VerbosityKeyword, error) {
	var kw VerbosityKeyword
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO verbosity_escalation_keywords (keyword, min_requested, escalate_to, priority, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, keyword, min_requested, escalate_to, enabled, priority, created_at
	`, keyword, minRequested, escalateTo, priority, enabled).Scan(
		&kw.ID,
		&kw.Keyword,
		&kw.MinRequested,
		&kw.EscalateTo,
		&kw.Enabled,
		&kw.Priority,
		&kw.CreatedAt,
	)
	if err != nil {
		return VerbosityKeyword{}, fmt.Errorf("create verbosity keyword: %w", err)
	}
	return kw, nil
}

func (s *PostgresStore) UpdateVerbosityKeyword(ctx context.Context, id int, input VerbosityKeywordUpdate) (VerbosityKeyword, error) {
	sets := make([]string, 0, 5)
	args := make([]any, 0, 6)
	argID := 1

	if input.Keyword != nil {
		sets = append(sets, fmt.Sprintf("keyword = $%d", argID))
		args = append(args, *input.Keyword)
		argID++
	}
	if input.MinRequested != nil {
		sets = append(sets, fmt.Sprintf("min_requested = $%d", argID))
		args = append(args, *input.MinRequested)
		argID++
	}
	if input.EscalateTo != nil {
		sets = append(sets, fmt.Sprintf("escalate_to = $%d", argID))
		args = append(args, *input.EscalateTo)
		argID++
	}
	if input.Enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", argID))
		args = append(args, *input.Enabled)
		argID++
	}
	if input.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", argID))
		args = append(args, *input.Priority)
		argID++
	}

	if len(sets) == 0 {
		return VerbosityKeyword{}, fmt.Errorf("no fields to update")
	}

	args = append(args, id)
	query := fmt.Sprintf(`
		UPDATE verbosity_escalation_keywords
		SET %s
		WHERE id = $%d
		RETURNING id, keyword, min_requested, escalate_to, enabled, priority, created_at
	`, strings.Join(sets, ", "), argID)

	var kw VerbosityKeyword
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&kw.ID,
		&kw.Keyword,
		&kw.MinRequested,
		&kw.EscalateTo,
		&kw.Enabled,
		&kw.Priority,
		&kw.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return VerbosityKeyword{}, sql.ErrNoRows
		}
		return VerbosityKeyword{}, fmt.Errorf("update verbosity keyword: %w", err)
	}
	return kw, nil
}

func (s *PostgresStore) DeleteVerbosityKeyword(ctx context.Context, id int) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM verbosity_escalation_keywords WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete verbosity keyword: %w", err)
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return err
}

