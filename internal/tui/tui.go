package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/store"
)

type ExecutionResult struct {
	Reply  string
	Source string
}

type Config struct {
	ContextName   string
	SystemPrompt  string
	History       []store.Message
	HistoryStore  store.HistoryStore
	HistoryLimit  int
	ModelOverride string
	RemoteURL     string
	LocalOnly     bool
	RemoteOnly    bool
	AgentName     string
	AgentProfile  interface{} // Will hold *agent.AgentProfile
	ExecuteFn     func(messages []chat.Message) (ExecutionResult, error)
}

type model struct {
	config         Config
	viewport       viewport.Model
	textarea       textarea.Model
	messages       []store.Message
	waiting        bool
	err            error
	width          int
	height         int
	currentAgent   string
	currentProfile interface{}
	lastSource     string
}

type responseMsg struct {
	content string
	source  string
	err     error
}

func Run(cfg Config) error {
	p := tea.NewProgram(newModel(cfg), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel(cfg Config) model {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.Focus()
	ta.CharLimit = 0
	ta.SetHeight(3)

	vp := viewport.New(80, 20)
	vp.SetContent("")

	// Set default agent if not provided
	agentName := cfg.AgentName
	if agentName == "" {
		agentName = "default"
	}

	m := model{
		config:         cfg,
		viewport:       vp,
		textarea:       ta,
		messages:       cfg.History,
		waiting:        false,
		currentAgent:   agentName,
		currentProfile: cfg.AgentProfile,
		lastSource:     "",
	}

	return m
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	m.textarea, tiCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			if m.waiting {
				return m, nil
			}
			userInput := strings.TrimSpace(m.textarea.Value())
			if userInput == "" {
				return m, nil
			}

			// Check for /agent command
			if strings.HasPrefix(userInput, "/agent ") {
				newAgent := strings.TrimSpace(strings.TrimPrefix(userInput, "/agent"))
				// Note: We can't import agent package here without circular dependency
				// For now, just update the agent name - the profile switching
				// will be handled in main.go
				m.currentAgent = newAgent
				m.textarea.Reset()

				// Add a system message about the switch
				now := time.Now().UTC()
				sysMsg := store.Message{
					Role:    "system",
					Content: fmt.Sprintf("Agent switched to: %s", newAgent),
					Time:    now,
				}
				m.messages = append(m.messages, sysMsg)
				m.viewport.SetContent(m.renderMessages())
				m.viewport.GotoBottom()
				return m, nil
			}

			// Add user message
			now := time.Now().UTC()
			userMsg := store.Message{Role: "user", Content: userInput, Time: now}
			m.messages = append(m.messages, userMsg)

			// Clear input
			m.textarea.Reset()

			// Set waiting state
			m.waiting = true

			// Update view
			m.viewport.SetContent(m.renderMessages())
			m.viewport.GotoBottom()

			// Execute in background
			return m, m.executeChat(userMsg)
		}

	case responseMsg:
		m.waiting = false
		now := time.Now().UTC()

		var assistantMsg store.Message
		if msg.err != nil {
			// Show error as assistant message
			assistantMsg = store.Message{
				Role:    "assistant",
				Content: fmt.Sprintf("[error] %v", msg.err),
				Time:    now,
			}
		} else {
			assistantMsg = store.Message{
				Role:    "assistant",
				Content: msg.content,
				Time:    now,
			}
			// Store execution source
			m.lastSource = msg.source
		}

		m.messages = append(m.messages, assistantMsg)

		// Persist both messages
		if msg.err == nil {
			_ = m.config.HistoryStore.Append(m.config.ContextName, m.messages[len(m.messages)-2])
			_ = m.config.HistoryStore.Append(m.config.ContextName, assistantMsg)
		}

		// Update view
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()

		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 2
		footerHeight := 4
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - headerHeight - footerHeight
		m.textarea.SetWidth(msg.Width - 2)

		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()

		return m, nil
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m model) View() string {
	header := fmt.Sprintf("Context: %s | Agent: %s", m.config.ContextName, m.currentAgent)
	if m.config.SystemPrompt != "" {
		header += fmt.Sprintf(" | System: %s", m.config.SystemPrompt)
	}
	header += "\n" + strings.Repeat("─", m.width)

	footer := strings.Repeat("─", m.width) + "\n"
	if m.waiting {
		footer += "Waiting for response...\n"
	} else {
		// Show last execution source if available
		if m.lastSource != "" {
			footer += fmt.Sprintf("(last source: %s)\n", m.lastSource)
		}
		footer += m.textarea.View()
	}

	return header + "\n" + m.viewport.View() + "\n" + footer
}

func (m model) renderMessages() string {
	var sb strings.Builder

	// Show system prompt if present
	if m.config.SystemPrompt != "" {
		sb.WriteString("[system] ")
		sb.WriteString(m.config.SystemPrompt)
		sb.WriteString("\n\n")
	}

	// Show all messages
	for _, msg := range m.messages {
		sb.WriteString(fmt.Sprintf("[%s] %s\n\n", msg.Role, msg.Content))
	}

	return sb.String()
}

func (m model) executeChat(userMsg store.Message) tea.Cmd {
	return func() tea.Msg {
		// Build messages array (reuse existing logic pattern)
		messages := make([]chat.Message, 0, len(m.messages)+1)
		if m.config.SystemPrompt != "" {
			messages = append(messages, chat.Message{Role: "system", Content: m.config.SystemPrompt})
		}

		// Apply history limit
		historyToSend := m.messages
		if m.config.HistoryLimit > 0 && len(historyToSend) > m.config.HistoryLimit {
			historyToSend = historyToSend[len(historyToSend)-m.config.HistoryLimit:]
		}

		for _, msg := range historyToSend {
			messages = append(messages, chat.Message{Role: msg.Role, Content: msg.Content})
		}

		// Execute
		result, err := m.config.ExecuteFn(messages)
		if err != nil {
			return responseMsg{content: "", source: "", err: err}
		}
		return responseMsg{content: result.Reply, source: result.Source, err: nil}
	}
}
