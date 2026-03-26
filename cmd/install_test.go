package cmd

import (
	"encoding/json"
	"os"
	"testing"
)

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

func TestRunInstallHooksWith_FreshSettings(t *testing.T) {
	var savedPath string
	var savedData []byte

	err := runInstallHooksWith(
		func() (string, error) { return "/mock/home", nil },
		func(path string) ([]byte, error) { return nil, os.ErrNotExist },
		func(path string, data []byte, perm os.FileMode) error {
			savedPath = path
			savedData = data
			return nil
		},
		func(path string, perm os.FileMode) error { return nil },
		nil, // suppress stdout
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if savedPath != "/mock/home/.claude/settings.json" {
		t.Errorf("wrote to %q, want /mock/home/.claude/settings.json", savedPath)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(savedData, &settings); err != nil {
		t.Fatalf("failed to parse written JSON: %v", err)
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("missing 'hooks' key in settings")
	}

	// Should have all 4 pop hook events
	for _, event := range []string{"UserPromptSubmit", "PreToolUse", "Stop", "Notification"} {
		eventHooks, ok := hooks[event].([]interface{})
		if !ok || len(eventHooks) == 0 {
			t.Errorf("missing hooks for event %q", event)
		}
	}
}

func TestRunInstallHooksWith_PreservesExistingSettings(t *testing.T) {
	existing := map[string]interface{}{
		"customKey": "customValue",
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "echo user hook",
						},
					},
				},
			},
		},
	}
	existingJSON, _ := json.Marshal(existing)

	var savedData []byte

	err := runInstallHooksWith(
		func() (string, error) { return "/mock/home", nil },
		func(path string) ([]byte, error) { return existingJSON, nil },
		func(path string, data []byte, perm os.FileMode) error {
			savedData = data
			return nil
		},
		func(path string, perm os.FileMode) error { return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(savedData, &settings); err != nil {
		t.Fatalf("failed to parse written JSON: %v", err)
	}

	// customKey should be preserved
	if settings["customKey"] != "customValue" {
		t.Error("existing customKey was not preserved")
	}

	// User hook should be preserved alongside pop hooks
	hooks := settings["hooks"].(map[string]interface{})
	eventHooks := hooks["UserPromptSubmit"].([]interface{})
	if len(eventHooks) < 2 {
		t.Errorf("expected at least 2 hooks for UserPromptSubmit (user + pop), got %d", len(eventHooks))
	}
}

func TestRunInstallHooksWith_ReplacesOldPopHooks(t *testing.T) {
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "pop pane set-status needs_attention 2>/dev/null || true",
						},
					},
				},
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "echo keep me",
						},
					},
				},
			},
		},
	}
	existingJSON, _ := json.Marshal(existing)

	var savedData []byte

	err := runInstallHooksWith(
		func() (string, error) { return "/mock/home", nil },
		func(path string) ([]byte, error) { return existingJSON, nil },
		func(path string, data []byte, perm os.FileMode) error {
			savedData = data
			return nil
		},
		func(path string, perm os.FileMode) error { return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var settings map[string]interface{}
	json.Unmarshal(savedData, &settings)

	hooks := settings["hooks"].(map[string]interface{})
	stopHooks := hooks["Stop"].([]interface{})

	// Should have the "echo keep me" hook + 1 new pop hook for Stop
	popCount := 0
	userCount := 0
	for _, h := range stopHooks {
		if isPopHook(h) {
			popCount++
		} else {
			userCount++
		}
	}
	if userCount != 1 {
		t.Errorf("expected 1 user hook preserved, got %d", userCount)
	}
	if popCount != 1 {
		t.Errorf("expected 1 pop hook (freshly installed), got %d", popCount)
	}
}

func TestRunInstallHooksWith_RemovesStaleEventKeys(t *testing.T) {
	// Simulate a previously installed hook event (e.g. SessionStart) that no longer
	// exists in popHooks. After re-install, the event key should be deleted entirely
	// rather than left as null in the JSON.
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"SessionStart": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "pop pane set-status working 2>/dev/null || true",
						},
					},
				},
			},
		},
	}
	existingJSON, _ := json.Marshal(existing)

	var savedData []byte

	err := runInstallHooksWith(
		func() (string, error) { return "/mock/home", nil },
		func(path string) ([]byte, error) { return existingJSON, nil },
		func(path string, data []byte, perm os.FileMode) error {
			savedData = data
			return nil
		},
		func(path string, perm os.FileMode) error { return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var settings map[string]interface{}
	json.Unmarshal(savedData, &settings)

	hooks := settings["hooks"].(map[string]interface{})

	// SessionStart should be completely gone, not null
	if val, exists := hooks["SessionStart"]; exists {
		t.Errorf("expected SessionStart to be deleted, got %v", val)
	}
}

func TestRunInstallHooksWith_WriteError(t *testing.T) {
	err := runInstallHooksWith(
		func() (string, error) { return "/mock/home", nil },
		func(path string) ([]byte, error) { return nil, os.ErrNotExist },
		func(path string, data []byte, perm os.FileMode) error {
			return os.ErrPermission
		},
		func(path string, perm os.FileMode) error { return nil },
		nil,
	)
	if err == nil {
		t.Error("expected error on write failure")
	}
}
