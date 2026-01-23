package executor

import "github.com/earlysvahn/sidekick/internal/chat"

type Executor interface {
	Execute(messages []chat.Message) (string, error)
}
