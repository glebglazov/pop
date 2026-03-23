package monitor

import "testing"

func TestIsKnownSource(t *testing.T) {
	tests := []struct {
		source Source
		known  bool
	}{
		{SourceClaudeCode, true},
		{Source("unknown"), false},
		{Source(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			if got := IsKnownSource(tt.source); got != tt.known {
				t.Errorf("IsKnownSource(%q) = %v, want %v", tt.source, got, tt.known)
			}
		})
	}
}
