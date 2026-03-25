package cmd

import "testing"

func TestIsPopHook(t *testing.T) {
	tests := []struct {
		name     string
		entry    interface{}
		expected bool
	}{
		{
			name: "pop monitor hook",
			entry: map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{
						"command": "pop monitor set-status $PANE_ID working",
					},
				},
			},
			expected: true,
		},
		{
			name: "pop pane set-status hook",
			entry: map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{
						"command": "pop pane set-status $PANE_ID needs_attention",
					},
				},
			},
			expected: true,
		},
		{
			name: "non-pop hook",
			entry: map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{
						"command": "echo done",
					},
				},
			},
			expected: false,
		},
		{
			name:     "non-map entry",
			entry:    "not a map",
			expected: false,
		},
		{
			name: "no hooks key",
			entry: map[string]interface{}{
				"other": "value",
			},
			expected: false,
		},
		{
			name: "empty hooks array",
			entry: map[string]interface{}{
				"hooks": []interface{}{},
			},
			expected: false,
		},
		{
			name: "mixed hooks only one is pop",
			entry: map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{
						"command": "echo hello",
					},
					map[string]interface{}{
						"command": "pop pane set-status #{pane_id} read 2>/dev/null || true",
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPopHook(tt.entry)
			if result != tt.expected {
				t.Errorf("isPopHook() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRemovePopHooks(t *testing.T) {
	popHook := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"command": "pop pane set-status #{pane_id} read",
			},
		},
	}
	otherHook := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"command": "echo done",
			},
		},
	}

	tests := []struct {
		name     string
		entries  []interface{}
		expected int
	}{
		{
			name:     "removes pop hooks keeps others",
			entries:  []interface{}{popHook, otherHook, popHook},
			expected: 1,
		},
		{
			name:     "no pop hooks returns all",
			entries:  []interface{}{otherHook, otherHook},
			expected: 2,
		},
		{
			name:     "all pop hooks returns empty",
			entries:  []interface{}{popHook},
			expected: 0,
		},
		{
			name:     "nil input",
			entries:  nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removePopHooks(tt.entries)
			if len(result) != tt.expected {
				t.Errorf("removePopHooks() returned %d entries, want %d", len(result), tt.expected)
			}
		})
	}
}
