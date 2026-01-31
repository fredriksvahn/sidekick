package store

import "context"

func (s *FileStore) ListVerbosityKeywords(ctx context.Context) ([]VerbosityKeyword, error) {
	return []VerbosityKeyword{}, nil
}

func (s *SQLiteStore) ListVerbosityKeywords(ctx context.Context) ([]VerbosityKeyword, error) {
	return []VerbosityKeyword{}, nil
}

