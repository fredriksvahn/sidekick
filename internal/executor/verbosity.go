package executor

import "github.com/earlysvahn/sidekick/internal/agent"

const (
	minVerbosity = 0
	maxVerbosity = 5
)

// Effective determines the verbosity level to use.
// CLI override takes precedence.
func Effective(flagValue int, profile *agent.AgentProfile) int {
	_ = profile
	if flagValue >= minVerbosity && flagValue <= maxVerbosity {
		return flagValue
	}
	return DefaultVerbosity()
}

// DefaultVerbosity returns the global default verbosity.
func DefaultVerbosity() int {
	if profile := agent.GetProfile("default"); profile != nil {
		if profile.DefaultVerbosity >= minVerbosity && profile.DefaultVerbosity <= maxVerbosity {
			return profile.DefaultVerbosity
		}
	}
	return 2
}

// ClampVerbosity clamps to the valid range.
func ClampVerbosity(value int) (int, bool) {
	if value < minVerbosity {
		return minVerbosity, true
	}
	if value > maxVerbosity {
		return maxVerbosity, true
	}
	return value, false
}

// MaxTokens maps verbosity to a hard token budget.
// Keep 0 as smallest and 5 as largest for predictability.
func MaxTokens(verbosity int) int {
	switch verbosity {
	case 0:
		return 128
	case 1:
		return 256
	case 2:
		return 768
	case 3:
		return 2048
	case 4:
		return 4096
	case 5:
		return 8192
	default:
		return 768
	}
}

// SystemConstraint returns verbosity-specific system constraints.
func SystemConstraint(verbosity int) string {
	switch verbosity {
	case 0:
		return "IMPORTANT: Respond with extreme brevity. No explanations. No lists. No markdown headings. No code comments. Answer in at most 3 short lines. Do not explain."
	case 1:
		return "IMPORTANT: Respond concisely. Minimal explanation only. No step-by-step tutorials. Short code examples are allowed. Avoid adjectives and filler."
	case 2:
		return "Respond with balanced, normal detail."
	case 3:
		return "Respond with detailed, pedagogical explanations. Use sections and lists when helpful."
	case 4:
		return "Respond with full tutorial-level detail, including examples and edge cases."
	case 5:
		return "Respond with exhaustive detail, covering rationale, alternatives, and edge cases with examples."
	default:
		return ""
	}
}
