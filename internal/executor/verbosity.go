package executor

import (
	"strings"

	"github.com/earlysvahn/sidekick/internal/agent"
)

// Effective determines the verbosity level to use.
// CLI override takes precedence over agent default.
func Effective(flagValue int, profile *agent.AgentProfile) int {
	// CLI override takes precedence
	if flagValue >= 0 && flagValue <= 3 {
		return flagValue
	}
	// Use agent default if available
	if profile != nil {
		return profile.DefaultVerbosity
	}
	// Fallback to normal
	return 2
}

// SystemConstraint returns system constraint for low verbosity modes.
// WHY: Models ignore instructions to be concise, this hard guard enforces discipline.
func SystemConstraint(verbosity int) string {
	switch verbosity {
	case 0:
		return "CRITICAL: Output ONLY the requested code or answer. NO explanations. NO examples beyond what's requested. NO extra sections."
	case 1:
		return "Be concise. Provide the requested code/answer with minimal explanation. Avoid verbose examples or extra context."
	default:
		return ""
	}
}

// PostProcess strips excess output for low verbosity modes.
// WHY: Last-resort safety net because models ignore system constraints.
func PostProcess(text string, verbosity int) string {
	if verbosity > 1 {
		return text // No post-processing for normal/verbose modes
	}

	lines := strings.Split(text, "\n")

	// Find first and last code block markers
	firstCodeBlock := -1
	lastCodeBlock := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if firstCodeBlock == -1 {
				firstCodeBlock = i
			}
			lastCodeBlock = i
		}
	}

	// If no code blocks found, return as-is
	if firstCodeBlock == -1 {
		return text
	}

	switch verbosity {
	case 0:
		// Minimal: strip everything before first code block and after last code block
		return strings.Join(lines[firstCodeBlock:lastCodeBlock+1], "\n")
	case 1:
		// Concise: keep brief intro before code, strip trailing explanations
		startLine := 0
		if firstCodeBlock > 3 {
			startLine = firstCodeBlock - 3 // Keep up to 3 lines before code
		}
		return strings.Join(lines[startLine:lastCodeBlock+1], "\n")
	default:
		return text
	}
}
