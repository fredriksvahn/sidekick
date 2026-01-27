package cli

import (
	"fmt"
	"os"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// spinnerModel is a simple model for showing a loading spinner
type spinnerModel struct {
	spinner  spinner.Model
	message  string
	quitting bool
	result   interface{}
	err      error
}

func newSpinnerModel(message string) spinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = s.Style
	return spinnerModel{
		spinner: s,
		message: message,
	}
}

func (m spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	case resultMsg:
		m.result = msg.result
		m.quitting = true
		return m, tea.Quit
	case error:
		m.err = msg
		m.quitting = true
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m spinnerModel) View() string {
	if m.quitting {
		return ""
	}
	return fmt.Sprintf("%s %s", m.spinner.View(), m.message)
}

// resultMsg wraps a result to send through tea.Msg
type resultMsg struct {
	result interface{}
}

// ExecuteWithSpinner runs executeFn with a loading spinner (or simple message if no TTY)
func ExecuteWithSpinner[T any](message string, executeFn func() (T, error)) (T, error) {
	// Check if we have a TTY for the spinner
	if !IsATTY() {
		// No TTY, just show a simple message and run
		fmt.Fprintf(os.Stderr, "%s\n", message)
		return executeFn()
	}

	m := newSpinnerModel(message)

	var result T
	var execErr error
	var wg sync.WaitGroup
	wg.Add(1)

	// Run execution in background
	go func() {
		defer wg.Done()
		result, execErr = executeFn()
	}()

	// Run spinner in a goroutine and send result when done
	p := tea.NewProgram(m)
	go func() {
		wg.Wait()
		if execErr != nil {
			p.Send(execErr)
		} else {
			p.Send(resultMsg{result: result})
		}
	}()

	finalModel, err := p.Run()
	if err != nil {
		// Fallback to simple execution if spinner fails
		wg.Wait()
		return result, execErr
	}

	// Get result from final model
	if fm, ok := finalModel.(spinnerModel); ok {
		if fm.err != nil {
			var zero T
			return zero, fm.err
		}
		if fm.result != nil {
			return result, nil
		}
	}

	return result, execErr
}

// IsATTY checks if stdout is a terminal
func IsATTY() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
