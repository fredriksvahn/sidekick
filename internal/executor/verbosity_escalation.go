package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/earlysvahn/sidekick/internal/store"
)

// EscalationResult contains verbosity resolution details
type EscalationResult struct {
	EffectiveVerbosity int
	Warning            string
	Escalated          bool
	MatchedKeywords    []string
}

func ResolveVerbosity(ctx context.Context, requested *int, defaultLevel int, agentName string, lastUserMessage string, userID string, keywordStore store.VerbosityKeywordLister) (EscalationResult, error) {
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
	matchedKeywords := []string{}
	escalated := false

	if keywordStore != nil && strings.TrimSpace(lastUserMessage) != "" && userID != "" {
		keywords, err := keywordStore.ListVerbosityKeywords(ctx, userID)
		if err != nil {
			return EscalationResult{}, err
		}
		lowered := strings.ToLower(lastUserMessage)

		// Separate agent-specific and global keywords
		agentKeywords := []store.VerbosityKeyword{}
		globalKeywords := []store.VerbosityKeyword{}

		for _, kw := range keywords {
			if !kw.Enabled {
				continue
			}
			if kw.Agent != nil && *kw.Agent == agentName {
				agentKeywords = append(agentKeywords, kw)
			} else if kw.Agent == nil {
				globalKeywords = append(globalKeywords, kw)
			}
		}

		// Try agent-specific keywords first, then global
		candidateKeywords := append(agentKeywords, globalKeywords...)

		highestEscalateTo := effectiveVerbosity
		for _, kw := range candidateKeywords {
			if kw.Keyword == "" {
				continue
			}
			if !strings.Contains(lowered, strings.ToLower(kw.Keyword)) {
				continue
			}
			if requestedValue < kw.MinRequested {
				continue
			}
			if requestedValue >= kw.EscalateTo {
				continue
			}

			// Track this keyword as matched
			matchedKeywords = append(matchedKeywords, kw.Keyword)

			if kw.EscalateTo > highestEscalateTo {
				highestEscalateTo = kw.EscalateTo
			}
		}

		if highestEscalateTo > effectiveVerbosity {
			effectiveVerbosity = highestEscalateTo
			escalated = true
		}
	}

	if v, clamped := ClampVerbosity(effectiveVerbosity); clamped {
		effectiveVerbosity = v
	}

	if escalated {
		warning = joinWarning(warning, fmt.Sprintf("verbosity auto-escalated from %d to %d due to detected intent", requestedValue, effectiveVerbosity))
	}

	return EscalationResult{
		EffectiveVerbosity: effectiveVerbosity,
		Warning:            warning,
		Escalated:          escalated,
		MatchedKeywords:    matchedKeywords,
	}, nil
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

