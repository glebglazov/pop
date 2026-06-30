package ui

import "fmt"

// KeyPress is a modifier-aware key event for quick-access decoding.
// Callers adapt from their input layer (e.g. bubbletea) without this package
// importing bubbletea.
type KeyPress struct {
	Code rune
	Alt  bool
	Ctrl bool
}

// QuickAccess decodes modifier+digit shortcuts and produces display labels.
type QuickAccess struct {
	modifier string
}

// NewQuickAccess constructs a quick-access helper for the given modifier
// ("alt", "ctrl", or "disabled"). Empty modifier defaults to "alt".
func NewQuickAccess(modifier string) *QuickAccess {
	if modifier == "" {
		modifier = "alt"
	}
	return &QuickAccess{modifier: modifier}
}

// Enabled reports whether quick-access shortcuts are active.
func (q *QuickAccess) Enabled() bool {
	return q.modifier != "" && q.modifier != "disabled"
}

// Digit extracts the digit (1-9) from a key press, or 0 if not a valid trigger.
func (q *QuickAccess) Digit(msg KeyPress) int {
	if !q.Enabled() || msg.Code < '1' || msg.Code > '9' {
		return 0
	}
	digit := int(msg.Code - '0')
	switch q.modifier {
	case "alt":
		if msg.Alt {
			return digit
		}
	case "ctrl":
		if msg.Ctrl {
			return digit
		}
	}
	return 0
}

// Label returns the display label for quick-access number n (e.g. "^1", "⌥2").
func (q *QuickAccess) Label(n int) string {
	switch q.modifier {
	case "ctrl":
		return fmt.Sprintf("^%d", n)
	case "alt":
		return fmt.Sprintf("⌥%d", n)
	default:
		return "  "
	}
}

// LabelFunc returns the QuickLabel closure for List, or nil when disabled.
func (q *QuickAccess) LabelFunc() func(int) string {
	if !q.Enabled() {
		return nil
	}
	return q.Label
}
