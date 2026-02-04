package cmd

import "testing"

func TestSanitizeSessionName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name unchanged",
			input:    "myproject",
			expected: "myproject",
		},
		{
			name:     "with slash unchanged",
			input:    "project/worktree",
			expected: "project/worktree",
		},
		{
			name:     "dots replaced with underscores",
			input:    "my.project",
			expected: "my_project",
		},
		{
			name:     "colons replaced with underscores",
			input:    "project:v1",
			expected: "project_v1",
		},
		{
			name:     "multiple dots and colons",
			input:    "my.project:v1.2.3",
			expected: "my_project_v1_2_3",
		},
		{
			name:     "worktree with dots",
			input:    "annual_calendar/feature.1",
			expected: "annual_calendar/feature_1",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special chars",
			input:    "...::",
			expected: "_____",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeSessionName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeSessionName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
