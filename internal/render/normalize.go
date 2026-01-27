package render

import (
	"fmt"
	"regexp"
	"strings"
)

// NormalizeModelOutput preprocesses LLM output before markdown rendering.
// WHY: Ollama/models emit pre-wrapped text with hard newlines MID-PARAGRAPH
// and even MID-WORD (e.g., "kom\nmunicera", "Go-ap\nplikation"). Glamour
// renders these literally, breaking prose. This unwraps paragraphs while
// preserving code blocks EXACTLY so Glamour can reflow properly.
func NormalizeModelOutput(text string) string {
	// Extract and preserve code blocks verbatim
	codeBlockPattern := regexp.MustCompile("(?s)```.*?```")
	codeBlocks := codeBlockPattern.FindAllString(text, -1)
	placeholder := "\x00CODE_BLOCK_%d\x00"

	for i, block := range codeBlocks {
		text = strings.Replace(text, block, fmt.Sprintf(placeholder, i), 1)
	}

	// Trim trailing whitespace per line
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	text = strings.Join(lines, "\n")

	// Collapse excessive blank lines (>2 consecutive newlines → 2)
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")

	// Fix mid-word hyphenation breaks: "word-\nword" → "wordword"
	text = regexp.MustCompile(`-\n`).ReplaceAllString(text, "")

	// Fix mid-paragraph breaks: single newline between non-empty lines → space
	// This unwraps hard-wrapped prose so Glamour can reflow naturally
	text = regexp.MustCompile(`([^\n])\n([^\n])`).ReplaceAllString(text, "$1 $2")

	// Restore code blocks
	for i, block := range codeBlocks {
		text = strings.Replace(text, fmt.Sprintf(placeholder, i), block, 1)
	}

	return text
}
