package executor

import (
	"github.com/earlysvahn/sidekick/internal/chat"
)

// TokenBudget represents estimated token usage for a request
type TokenBudget struct {
	EstimatedPromptTokens int `json:"estimated_prompt_tokens"`
	MaxCompletionTokens   int `json:"max_completion_tokens"`
	TotalEstimatedTokens  int `json:"total_estimated_tokens"`
}

// EstimateTokenBudget provides a lightweight heuristic estimate
// of token usage before model execution.
// This is NOT precise - it's a rough approximation for progress reporting.
func EstimateTokenBudget(messages []chat.Message, verbosity int) TokenBudget {
	// Heuristic: ~4 characters per token (rough average)
	const charsPerToken = 4

	totalChars := 0
	for _, msg := range messages {
		// Count content
		totalChars += len(msg.Content)
		// Add overhead for role and formatting (~20 chars per message)
		totalChars += 20
	}

	estimatedPromptTokens := totalChars / charsPerToken
	maxCompletionTokens := MaxTokens(verbosity)
	totalEstimatedTokens := estimatedPromptTokens + maxCompletionTokens

	return TokenBudget{
		EstimatedPromptTokens: estimatedPromptTokens,
		MaxCompletionTokens:   maxCompletionTokens,
		TotalEstimatedTokens:  totalEstimatedTokens,
	}
}
