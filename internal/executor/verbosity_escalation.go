package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/earlysvahn/sidekick/internal/store"
)

func ResolveVerbosity(ctx context.Context, requested *int, defaultLevel int, agentName string, lastUserMessage string, keywordStore store.VerbosityKeywordLister) (int, string, error) {
	warning := ""
	requestedValue := defaultLevel
	if requested != nil {
		requestedValue = *requested
	}
	if v, clamped := ClampVerbosity(requestedValue); clamped {
		warning = fmt.Sprintf("verbosity %d clamped to %d", requestedValue, v)
		requestedValue = v
	}

	biasedVerbosity := requestedValue
	if bias := agentBaselineBias(agentName); bias > 0 {
		if requestedValue+bias > biasedVerbosity {
			biasedVerbosity = requestedValue + bias
		}
	}

	effectiveVerbosity := biasedVerbosity
	autoEscalated := false

	if keywordStore != nil && strings.TrimSpace(lastUserMessage) != "" {
		keywords, err := keywordStore.ListVerbosityKeywords(ctx)
		if err != nil {
			return 0, warning, err
		}
		lowered := strings.ToLower(lastUserMessage)
		for _, kw := range keywords {
			if !kw.Enabled {
				continue
			}
			if requestedValue < kw.MinRequested {
				continue
			}
			if kw.Keyword == "" {
				continue
			}
			if !strings.Contains(lowered, strings.ToLower(kw.Keyword)) {
				continue
			}
			if kw.EscalateTo > effectiveVerbosity {
				effectiveVerbosity = kw.EscalateTo
			}
			if effectiveVerbosity > biasedVerbosity {
				autoEscalated = true
			}
			break
		}
	}

	if v, clamped := ClampVerbosity(effectiveVerbosity); clamped {
		effectiveVerbosity = v
	}

	if autoEscalated {
		warning = joinWarning(warning, fmt.Sprintf("Verbosity auto-escalated from %d to %d based on prompt analysis", requestedValue, effectiveVerbosity))
	}

	return effectiveVerbosity, warning, nil
}

func agentBaselineBias(agentName string) int {
	switch strings.TrimSpace(strings.ToLower(agentName)) {
	case "go-dev":
		return 1
	case "go-architect":
		return 2
	case "fitness":
		return 0
	default:
		return 0
	}
}

func joinWarning(existing, next string) string {
	if existing == "" {
		return next
	}
	if next == "" {
		return existing
	}
	return existing + "; " + next
}

