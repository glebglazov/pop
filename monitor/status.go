package monitor

// NormalizeStatus maps CLI input and legacy aliases to canonical pane statuses.
func NormalizeStatus(raw string) PaneStatus {
	return normalizeStatus(PaneStatus(raw))
}

func normalizeStatus(s PaneStatus) PaneStatus {
	switch s {
	case StatusClear, legacyStatusIdle, legacyStatusRead:
		return StatusClear
	case StatusUnread, legacyStatusNeedsAttention:
		return StatusUnread
	default:
		return s
	}
}

// IsClear reports whether s represents a clear pane, including legacy aliases
// that may appear before load migration rewrites them.
func IsClear(s PaneStatus) bool {
	switch s {
	case StatusClear, legacyStatusIdle, legacyStatusRead:
		return true
	default:
		return false
	}
}
