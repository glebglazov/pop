package monitor

import "strings"

// Detector analyzes captured pane content and returns the detected status.
// previousContent is the content from the last poll (empty string on first poll).
type Detector interface {
	Detect(content, previousContent string) PaneStatus
}

// IsKnownSource returns true if the source has a registered detector
func IsKnownSource(source Source) bool {
	switch source {
	case SourceClaudeCode:
		return true
	default:
		return false
	}
}

// DetectorForSource returns the appropriate detector for a given source
func DetectorForSource(source Source) Detector {
	switch source {
	case SourceClaudeCode:
		return &ClaudeCodeDetector{}
	default:
		return &NullDetector{}
	}
}

// ClaudeCodeDetector detects Claude Code state from pane content.
// Priority:
//  1. Content changed since last poll → working (always, even if input field visible)
//  2. Content unchanged + input field visible → needs_attention
//  3. Content unchanged + no input field → unknown
//
// The input field (❯ with ─ border) is always visible in Claude Code,
// even during active work. So content change is the primary signal.
type ClaudeCodeDetector struct{}

func (d *ClaudeCodeDetector) Detect(content, previousContent string) PaneStatus {
	// Content change is the strongest signal — if output is changing,
	// Claude is actively working regardless of what's on screen
	if previousContent != "" && content != previousContent {
		return StatusWorking
	}

	// Content is stable — check if the input field is visible,
	// meaning Claude is idle and waiting for user input
	lines := strings.Split(content, "\n")
	if hasInputField(lines) {
		return StatusNeedsAttention
	}

	return StatusUnknown
}

// hasInputField checks if Claude Code's input prompt is visible.
// The input field has a ─ border line directly above the ❯ prompt line.
func hasInputField(lines []string) bool {
	// Only check last 10 lines — the input field is always at the bottom
	start := len(lines) - 10
	if start < 0 {
		start = 0
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "❯") {
			if isBorderLine(lines[i-1]) {
				return true
			}
		}
	}
	return false
}

// isBorderLine checks if a line is a ─ border (at least 10 consecutive ─ chars)
func isBorderLine(line string) bool {
	count := 0
	for _, r := range line {
		if r == '─' {
			count++
			if count >= 10 {
				return true
			}
		} else {
			count = 0
		}
	}
	return false
}

// NullDetector always returns StatusUnknown
type NullDetector struct{}

func (d *NullDetector) Detect(content, previousContent string) PaneStatus {
	return StatusUnknown
}
