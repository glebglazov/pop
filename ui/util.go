package ui

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// Shared color constants used across picker views
var (
	colorAccent    = lipgloss.Color("39")
	colorDim       = lipgloss.Color("241")
	colorPreview   = lipgloss.Color("252")
	colorSeparator = lipgloss.Color("238")
	colorAttention = lipgloss.Color("196")
	colorWorking   = lipgloss.Color("214")
	colorClear     = lipgloss.Color("241")

	indicatorStyle = lipgloss.NewStyle().Foreground(colorAccent)
	hintStyle      = lipgloss.NewStyle().Foreground(colorDim)
	dimStyle       = lipgloss.NewStyle().Foreground(colorDim)
	headerStyle    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	// IndicatorStyle is the shared cursor-row block indicator; exported for cross-package use.
	IndicatorStyle = indicatorStyle
	// HintStyle is the shared dimmed footer hint style; exported for cross-package use.
	HintStyle = hintStyle
)

// WriteInputBox writes a bordered input box to b; exported for cross-package use.
// content is rendered inside; use TextField.View() or a static string like " Help".
func WriteInputBox(b *strings.Builder, width int, content string) {
	writeInputBox(b, width, content)
}

// writeInputBox writes a bordered input box to b. content is rendered inside;
// use TextField.View() or a static string like " Help".
func writeInputBox(b *strings.Builder, width int, content string) {
	boxWidth := width
	if boxWidth < 20 {
		boxWidth = 40
	}
	innerWidth := boxWidth - 2

	b.WriteString("┌")
	b.WriteString(strings.Repeat("─", innerWidth))
	b.WriteString("┐\n")

	padding := innerWidth - lipgloss.Width(content)
	if padding < 0 {
		padding = 0
	}
	b.WriteString("│")
	b.WriteString(content)
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("│\n")

	b.WriteString("└")
	b.WriteString(strings.Repeat("─", innerWidth))
	b.WriteString("┘\n")
}

// renderUpdateNotice renders the dimmed Update notice anchored to the top-right
// of a width-wide line. It is unobtrusive: dimmed and right-aligned, with the
// text truncated if it would not fit. The caller reserves the line so the
// notice never shifts surrounding content.
func renderUpdateNotice(width int, text string) string {
	if text == "" {
		return ""
	}
	if width <= 0 {
		return dimStyle.Render(text)
	}
	text = truncateToWidth(text, width)
	padding := width - len([]rune(text))
	if padding < 0 {
		padding = 0
	}
	return strings.Repeat(" ", padding) + dimStyle.Render(text)
}

// TruncateToWidth trims s to at most width runes (plain text, no ANSI).
func TruncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width])
}

// truncateToWidth is an internal alias for TruncateToWidth; used by renderUpdateNotice.
func truncateToWidth(s string, width int) string {
	return TruncateToWidth(s, width)
}

// TruncateString truncates s to maxWidth visible characters, respecting ANSI escapes.
// Non-positive maxWidth leaves s unchanged (used when terminal width not yet available).
func TruncateString(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	visibleWidth := 0
	inEscape := false
	lastSafe := 0
	for i, r := range s {
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if visibleWidth >= maxWidth {
			return s[:lastSafe]
		}
		visibleWidth++
		lastSafe = i + len(string(r))
	}
	return s
}

// truncateString is an internal alias for TruncateString; used by ui/dashboard.go.
func truncateString(s string, maxWidth int) string {
	return TruncateString(s, maxWidth)
}

// adjustScroll ensures cursor is visible by adjusting scroll offset.
// margin is the number of extra lines to keep above the cursor (0 for no margin).
func adjustScroll(cursor, scroll, height, itemCount, margin int) (newScroll int) {
	visible := height
	if visible > itemCount {
		visible = itemCount
	}
	if visible == 0 {
		return 0
	}

	if margin >= visible {
		margin = visible - 1
	}

	newScroll = scroll
	if cursor-newScroll < margin {
		newScroll = cursor - margin
	}
	if cursor >= newScroll+visible {
		newScroll = cursor - visible + 1
	}
	if newScroll < 0 {
		newScroll = 0
	}
	maxScroll := itemCount - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if newScroll > maxScroll {
		newScroll = maxScroll
	}
	return newScroll
}

// fuzzyStringMatch pairs a string with its fuzzy match score.
type fuzzyStringMatch struct {
	value string
	score int
}

// fuzzyMatch runs fzf's FuzzyMatchV2 on candidates and returns them sorted
// by score ascending (best match last, for bottom-up display).
func fuzzyMatch(query string, candidates []string) []string {
	pattern := []rune(strings.ToLower(query))
	slab := util.MakeSlab(100*1024, 2048)

	var matches []fuzzyStringMatch
	for _, c := range candidates {
		chars := util.ToChars([]byte(strings.ToLower(c)))
		result, _ := algo.FuzzyMatchV2(false, true, true, &chars, pattern, false, slab)
		if result.Score > 0 {
			matches = append(matches, fuzzyStringMatch{value: c, score: result.Score})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score < matches[j].score
	})

	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.value
	}
	return out
}

// LastNSegments returns the last n segments of a path joined with "/".
// For n=2 and path="/a/b/c/d", returns "c/d".
// For n=1, equivalent to filepath.Base.
// For n<=0, returns filepath.Base.
func LastNSegments(path string, n int) string {
	if n <= 1 {
		return filepath.Base(path)
	}
	result := filepath.Base(path)
	dir := filepath.Dir(path)
	for i := 1; i < n; i++ {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		result = filepath.Base(dir) + "/" + result
		dir = parent
	}
	return result
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\[[\d;]*m`)

// StripANSI removes ANSI escape codes from a string
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}
