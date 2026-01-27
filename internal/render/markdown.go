package render

import (
	"os"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// Markdown renders markdown text with syntax highlighting for terminal.
func Markdown(text string) string {
	// Normalize model output before rendering
	text = NormalizeModelOutput(text)

	// Configure based on TTY availability
	var opts []glamour.TermRendererOption

	if isTTY() {
		// Terminal: enable styling but let terminal handle wrapping
		// Setting wrap to 0 prevents Glamour from wrapping code blocks
		opts = []glamour.TermRendererOption{
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(0), // Disable Glamour wrapping entirely
		}
	} else {
		// Non-TTY: plain output
		opts = []glamour.TermRendererOption{
			glamour.WithStandardStyle("notty"),
			glamour.WithWordWrap(0),
		}
	}

	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		// Fallback to plain text if renderer creation fails
		return text
	}

	rendered, err := r.Render(text)
	if err != nil {
		// Fallback to plain text if rendering fails
		return text
	}

	return rendered
}

// isTTY checks if stdout is a terminal.
func isTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}
