package ui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/glamour"
)

// documentWidthFallback is the wrap width used when a surface has not learned its
// own width yet (no WindowSizeMsg, or a non-terminal stdout).
const documentWidthFallback = 80

// glamourFrameWidth is the horizontal room glamour's own style takes outside the
// wrapped text — a two-column document margin on each side. Wrapping at the raw
// surface width would push every line that reaches the wrap point past it.
const glamourFrameWidth = 4

// RendersMarkdown reports whether a document at path is rendered as Markdown.
// Extension is the whole test (ADR-0222): pop's own progress record is a `.txt`
// whose `---` separators a Markdown renderer would turn into a rule per entry, so
// nothing sniffs content.
func RendersMarkdown(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".md")
}

// RenderMarkdown renders Markdown for a terminal of the given width and
// appearance, wrapping to fit inside glamour's own margins. A width of zero or
// less falls back to 80.
//
// The appearance is asked for rather than inferred: glamour's automatic style is
// a tmux-blind background guess that always answers dark, so pop resolves the
// fact itself and glamour is told (ADR-0230).
//
// Rendering is best-effort: a document glamour cannot parse comes back as the text
// that went in, because a reader losing the document is worse than reading its
// markup.
func RenderMarkdown(text string, width int, appearance Appearance) string {
	renderer, err := newMarkdownRenderer(width, appearance)
	if err != nil {
		return text
	}
	out, err := renderer.Render(text)
	if err != nil {
		return text
	}
	return trimBlankEdgeLines(out)
}

func newMarkdownRenderer(width int, appearance Appearance) (*glamour.TermRenderer, error) {
	if width <= 0 {
		width = documentWidthFallback
	}
	wrap := width - glamourFrameWidth
	if wrap < 20 {
		wrap = 20
	}
	return glamour.NewTermRenderer(
		glamour.WithStandardStyle(appearance.glamourStyle()),
		glamour.WithWordWrap(wrap),
	)
}

// trimBlankEdgeLines drops the blank lines glamour pads a document with. The
// surfaces scroll over lines, so left in place the padding is scrollable
// emptiness at the top and bottom of every document — and glamour's padding is
// whitespace, not empty, so trimming newlines alone leaves it.
func trimBlankEdgeLines(text string) string {
	lines := strings.Split(text, "\n")
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}
