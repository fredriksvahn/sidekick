package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type VerbosityKeyword struct {
	Keyword      string    `json:"keyword"`
	MinRequested int       `json:"min_requested"`
	EscalateTo   int       `json:"escalate_to"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

type VerbosityKeywordUpdate struct {
	MinRequested *int
	EscalateTo   *int
	Enabled      *bool
}

type VerbosityKeywordLister interface {
	ListVerbosityKeywords(ctx context.Context) ([]VerbosityKeyword, error)
}

type VerbosityKeywordStore interface {
	VerbosityKeywordLister
	CreateVerbosityKeyword(ctx context.Context, keyword string, minRequested, escalateTo int, enabled bool) (VerbosityKeyword, error)
	UpdateVerbosityKeyword(ctx context.Context, keyword string, input VerbosityKeywordUpdate) (VerbosityKeyword, error)
	DeleteVerbosityKeyword(ctx context.Context, keyword string) error
}

type NoopVerbosityKeywordStore struct{}

func (NoopVerbosityKeywordStore) ListVerbosityKeywords(ctx context.Context) ([]VerbosityKeyword, error) {
	return []VerbosityKeyword{}, nil
}

func (s *PostgresStore) ListVerbosityKeywords(ctx context.Context) ([]VerbosityKeyword, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT keyword, min_requested_verbosity, escalate_to, enabled, created_at
		FROM verbosity_escalation_keywords
		ORDER BY length(keyword) DESC, keyword ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list verbosity keywords: %w", err)
	}
	defer rows.Close()

	var keywords []VerbosityKeyword
	for rows.Next() {
		var kw VerbosityKeyword
		if err := rows.Scan(&kw.Keyword, &kw.MinRequested, &kw.EscalateTo, &kw.Enabled, &kw.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan verbosity keyword: %w", err)
		}
		keywords = append(keywords, kw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate verbosity keywords: %w", err)
	}
	return keywords, nil
}

func (s *PostgresStore) CreateVerbosityKeyword(ctx context.Context, keyword string, minRequested, escalateTo int, enabled bool) (VerbosityKeyword, error) {
	var kw VerbosityKeyword
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO verbosity_escalation_keywords (keyword, min_requested_verbosity, escalate_to, enabled)
		VALUES ($1, $2, $3, $4)
		RETURNING keyword, min_requested_verbosity, escalate_to, enabled, created_at
	`, keyword, minRequested, escalateTo, enabled).Scan(
		&kw.Keyword,
		&kw.MinRequested,
		&kw.EscalateTo,
		&kw.Enabled,
		&kw.CreatedAt,
	)
	if err != nil {
		return VerbosityKeyword{}, fmt.Errorf("create verbosity keyword: %w", err)
	}
	return kw, nil
}

func (s *PostgresStore) UpdateVerbosityKeyword(ctx context.Context, keyword string, input VerbosityKeywordUpdate) (VerbosityKeyword, error) {
	sets := make([]string, 0, 3)
	args := make([]any, 0, 4)
	argID := 1

	if input.MinRequested != nil {
		sets = append(sets, fmt.Sprintf("min_requested_verbosity = $%d", argID))
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

	if len(sets) == 0 {
		return VerbosityKeyword{}, fmt.Errorf("no fields to update")
	}

	args = append(args, keyword)
	query := fmt.Sprintf(`
		UPDATE verbosity_escalation_keywords
		SET %s
		WHERE keyword = $%d
		RETURNING keyword, min_requested_verbosity, escalate_to, enabled, created_at
	`, strings.Join(sets, ", "), argID)

	var kw VerbosityKeyword
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&kw.Keyword,
		&kw.MinRequested,
		&kw.EscalateTo,
		&kw.Enabled,
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

func (s *PostgresStore) DeleteVerbosityKeyword(ctx context.Context, keyword string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM verbosity_escalation_keywords WHERE keyword = $1`, keyword)
	if err != nil {
		return fmt.Errorf("delete verbosity keyword: %w", err)
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return sql.ErrNoRows
	}
	return err
}

