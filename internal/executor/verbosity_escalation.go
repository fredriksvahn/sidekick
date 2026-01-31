package executor

import (
	"context"
	"fmt"
	"log"
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

	if keywordStore != nil && strings.TrimSpace(lastUserMessage) != "" {
		keywords, err := keywordStore.ListVerbosityKeywords(ctx)
		if err != nil {
			return 0, warning, err
		}
		log.Printf("[ESCALATION DEBUG] Loaded %d keywords from DB", len(keywords))
		log.Printf("[ESCALATION DEBUG] User message: %q", lastUserMessage)
		log.Printf("[ESCALATION DEBUG] Requested verbosity: %d, Effective verbosity: %d", requestedValue, effectiveVerbosity)

		lowered := strings.ToLower(lastUserMessage)
		highestEscalateTo := effectiveVerbosity
		matchedCount := 0
		for _, kw := range keywords {
			kwLower := strings.ToLower(kw.Keyword)

			if !kw.Enabled {
				log.Printf("[ESCALATION DEBUG] Skipping disabled keyword: %q", kw.Keyword)
				continue
			}
			if kw.Keyword == "" {
				continue
			}
			if !strings.Contains(lowered, kwLower) {
				continue
			}
			log.Printf("[ESCALATION DEBUG] MATCHED keyword: %q (min_requested=%d, escalate_to=%d)", kw.Keyword, kw.MinRequested, kw.EscalateTo)
			matchedCount++

			if requestedValue < kw.MinRequested {
				log.Printf("[ESCALATION DEBUG] - Skipped: requested (%d) < min_requested (%d)", requestedValue, kw.MinRequested)
				continue
			}
			if requestedValue >= kw.EscalateTo {
				log.Printf("[ESCALATION DEBUG] - Skipped: requested (%d) >= escalate_to (%d)", requestedValue, kw.EscalateTo)
				continue
			}
			if kw.EscalateTo > highestEscalateTo {
				log.Printf("[ESCALATION DEBUG] - CANDIDATE: escalate_to (%d) > current highest (%d)", kw.EscalateTo, highestEscalateTo)
				highestEscalateTo = kw.EscalateTo
			}
		}
		log.Printf("[ESCALATION DEBUG] Total keywords matched: %d, Highest escalate_to: %d", matchedCount, highestEscalateTo)
		if highestEscalateTo > effectiveVerbosity {
			log.Printf("[ESCALATION DEBUG] ESCALATING from %d to %d", effectiveVerbosity, highestEscalateTo)
			effectiveVerbosity = highestEscalateTo
		}
	}

	if v, clamped := ClampVerbosity(effectiveVerbosity); clamped {
		effectiveVerbosity = v
	}

	if effectiveVerbosity > biasedVerbosity {
		warning = joinWarning(warning, fmt.Sprintf("verbosity auto-escalated from %d to %d due to detected intent", requestedValue, effectiveVerbosity))
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

