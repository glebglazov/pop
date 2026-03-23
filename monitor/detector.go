package monitor

// IsKnownSource returns true if the source is recognized
func IsKnownSource(source Source) bool {
	switch source {
	case SourceClaudeCode:
		return true
	default:
		return false
	}
}
