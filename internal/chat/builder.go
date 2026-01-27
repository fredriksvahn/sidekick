package chat

import "github.com/earlysvahn/sidekick/internal/store"

// BuildMessages constructs the message array for LLM execution.
// It applies history limit, adds system prompt if present, and appends user prompt.
func BuildMessages(system string, history []store.Message, historyLimit int, userPrompt string) []Message {
	// Apply history limit
	limitedHistory := history
	if historyLimit > 0 && len(limitedHistory) > historyLimit {
		limitedHistory = limitedHistory[len(limitedHistory)-historyLimit:]
	}

	// Build message array
	messages := make([]Message, 0, len(limitedHistory)+2)
	if system != "" {
		messages = append(messages, Message{Role: "system", Content: system})
	}
	for _, m := range limitedHistory {
		messages = append(messages, Message{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, Message{Role: "user", Content: userPrompt})
	return messages
}
