package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/earlysvahn/sidekick/internal/chat"
	"github.com/earlysvahn/sidekick/internal/executor"
	"github.com/earlysvahn/sidekick/internal/render"
	"github.com/earlysvahn/sidekick/internal/store"
)

type ExecutionResult struct {
	Reply  string
	Source string
}

type Config struct {
	ContextName     string
	SystemPrompt    string
	History         []store.Message
	HistoryStore    store.HistoryStore
	HistoryLimit    int
	ModelOverride   string
	RemoteURL       string
	LocalOnly       bool
	RemoteOnly      bool
	AgentName       string
	AgentProfile    interface{} // Will hold *agent.AgentProfile
	AvailableAgents []string
	Verbosity       int
	ExecuteFn       func(messages []chat.Message, verbosity int) (ExecutionResult, error)
}

type model struct {
	config         Config
	viewport       viewport.Model
	textarea       textarea.Model
	spinner        spinner.Model
	messages       []store.Message
	waiting        bool
	err            error
	width          int
	height         int
	currentAgent   string
	currentProfile interface{}
	lastSource     string
	verbosity      int
}

type responseMsg struct {
	content string
	source  string
	err     error
}

func Run(cfg Config) error {
	p := tea.NewProgram(newModel(cfg))
	_, err := p.Run()
	return err
}

func newModel(cfg Config) model {
	ta := textarea.New()
	ta.Placeholder = "Type your message..."
	ta.Focus()
	ta.CharLimit = 0
	ta.SetHeight(3)
	ta.ShowLineNumbers = false

	vp := viewport.New(80, 20)
	vp.SetContent("")

	s := spinner.New()
	s.Spinner = spinner.Dot

	// Set default agent if not provided
	agentName := cfg.AgentName
	if agentName == "" {
		agentName = "default"
	}

	m := model{
		config:         cfg,
		viewport:       vp,
		textarea:       ta,
		spinner:        s,
		messages:       cfg.History,
		waiting:        false,
		currentAgent:   agentName,
		currentProfile: cfg.AgentProfile,
		lastSource:     "",
		verbosity:      cfg.Verbosity,
	}

	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		spCmd tea.Cmd
	)

	m.textarea, tiCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	m.spinner, spCmd = m.spinner.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyCtrlJ:
			if !m.waiting {
				m.textarea.InsertString("\n")
				return m, nil
			}
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
				m.textarea.Reset()

				// Validate agent
				validAgent := false
				for _, available := range m.config.AvailableAgents {
					if available == newAgent {
						validAgent = true
						break
					}
				}

				now := time.Now().UTC()
				var sysMsg store.Message
				if !validAgent {
					sysMsg = store.Message{
						Role:    "system",
						Content: fmt.Sprintf("Unknown agent: %s\nAvailable agents: %s", newAgent, strings.Join(m.config.AvailableAgents, ", ")),
						Time:    now,
					}
				} else {
					m.currentAgent = newAgent
					sysMsg = store.Message{
						Role:    "system",
						Content: fmt.Sprintf("Agent switched to: %s", newAgent),
						Time:    now,
					}
				}

				m.messages = append(m.messages, sysMsg)
				m.viewport.SetContent(m.renderMessages())
				m.viewport.GotoBottom()
				return m, nil
			}

			// Check for /verbosity command
			if strings.HasPrefix(userInput, "/verbosity ") {
				levelStr := strings.TrimSpace(strings.TrimPrefix(userInput, "/verbosity"))
				m.textarea.Reset()

				now := time.Now().UTC()
				var sysMsg store.Message

				var newLevel int
				_, err := fmt.Sscanf(levelStr, "%d", &newLevel)
				if err != nil || newLevel < 0 || newLevel > 4 {
					sysMsg = store.Message{
						Role:    "system",
						Content: "Invalid verbosity level. Use 0 (minimal), 1 (concise), 2 (normal), 3 (verbose), or 4 (very verbose)",
						Time:    now,
					}
				} else {
					m.verbosity = newLevel
					sysMsg = store.Message{
						Role:    "system",
						Content: fmt.Sprintf("Verbosity set to: %d", newLevel),
						Time:    now,
					}
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

	return m, tea.Batch(tiCmd, vpCmd, spCmd)
}

func (m model) View() string {
	header := fmt.Sprintf("Context: %s | Agent: %s", m.config.ContextName, m.currentAgent)
	header += "\n" + strings.Repeat("─", m.width)

	footer := strings.Repeat("─", m.width) + "\n"
	if m.waiting {
		footer += fmt.Sprintf("\n  %s  Loading...\n\n", m.spinner.View())
	} else {
		// Show last execution source if available
		if m.lastSource != "" {
			footer += fmt.Sprintf("(last source: %s)\n", m.lastSource)
		}
		footer += m.textarea.View()
	}

	return header + "\n" + m.viewport.View() + "\n" + footer
}

var (
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	systemStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
)

func (m model) wrapMessage(role, text string) string {
	if m.width == 0 {
		return fmt.Sprintf("[%s]\n%s", role, text)
	}

	if role != "user" && role != "system" {
		text = render.Markdown(text)
	}

	roleLabel := fmt.Sprintf("[%s]", role)
	availableWidth := m.viewport.Width - 2
	if availableWidth < 20 {
		availableWidth = 20
	}

	var roleStyle lipgloss.Style
	switch role {
	case "user":
		roleStyle = userStyle
	case "system":
		roleStyle = systemStyle
	default:
		roleStyle = assistantStyle
	}

	wrapStyle := lipgloss.NewStyle().Width(availableWidth)
	wrapped := wrapStyle.Render(text)

	return roleStyle.Render(roleLabel) + "\n" + wrapped
}

func (m model) renderMessages() string {
	var sb strings.Builder

	if m.config.SystemPrompt != "" {
		wrapped := m.wrapMessage("system", m.config.SystemPrompt)
		sb.WriteString(wrapped)
		sb.WriteString("\n\n")
	}

	for _, msg := range m.messages {
		role := msg.Role
		if role == "assistant" {
			role = m.currentAgent
		}
		wrapped := m.wrapMessage(role, msg.Content)
		sb.WriteString(wrapped)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

func (m model) executeChat(userMsg store.Message) tea.Cmd {
	return func() tea.Msg {
		// Inject system constraint based on current verbosity
		systemPrompt := m.config.SystemPrompt
		if constraint := executor.SystemConstraint(m.verbosity); constraint != "" {
			if systemPrompt != "" {
				systemPrompt = systemPrompt + "\n\n" + constraint
			} else {
				systemPrompt = constraint
			}
		}

		// Build messages array
		messages := make([]chat.Message, 0, len(m.messages)+1)
		if systemPrompt != "" {
			messages = append(messages, chat.Message{Role: "system", Content: systemPrompt})
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
		result, err := m.config.ExecuteFn(messages, m.verbosity)
		if err != nil {
			return responseMsg{content: "", source: "", err: err}
		}
		return responseMsg{content: result.Reply, source: result.Source, err: nil}
	}
}
