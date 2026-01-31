package commands

import "github.com/earlysvahn/sidekick/internal/store"

func resolveKeywordLister(historyStore store.HistoryStore) store.VerbosityKeywordLister {
	if lister, ok := historyStore.(store.VerbosityKeywordLister); ok {
		return lister
	}
	return store.NoopVerbosityKeywordStore{}
}

