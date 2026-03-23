package monitor

import "testing"

func TestClaudeCodeDetector_Working(t *testing.T) {
	d := &ClaudeCodeDetector{}
	previousContent := "old content from last poll"
	content := `⏺ Let me check the file structure.
⏺ Bash(ls -la)
  Running
`
	result := d.Detect(content, previousContent)
	if result != StatusWorking {
		t.Errorf("Detect() = %q, want %q", result, StatusWorking)
	}
}

func TestClaudeCodeDetector_WorkingContentChanged(t *testing.T) {
	d := &ClaudeCodeDetector{}
	prev := "line 1\nline 2\n"
	curr := "line 1\nline 2\nline 3\n"
	result := d.Detect(curr, prev)
	if result != StatusWorking {
		t.Errorf("Detect() = %q, want %q", result, StatusWorking)
	}
}

func TestClaudeCodeDetector_IdlePrompt(t *testing.T) {
	d := &ClaudeCodeDetector{}
	border := "──────────────────────────────────────────"
	content := `⏺ Here's what I found in the codebase.

  The config package handles TOML loading.

` + border + "\n❯\n" + border + "\n"

	// Content unchanged (same as previous) + input field visible → needs_attention
	result := d.Detect(content, content)
	if result != StatusNeedsAttention {
		t.Errorf("Detect() = %q, want %q", result, StatusNeedsAttention)
	}
}

func TestClaudeCodeDetector_IdlePromptWithUserInput(t *testing.T) {
	d := &ClaudeCodeDetector{}
	border := "──────────────────────────────────────────"
	content := `⏺ Done.

` + border + "\n❯ what about the tests?\n" + border + "\n"

	// Content unchanged + input field visible → needs_attention
	result := d.Detect(content, content)
	if result != StatusNeedsAttention {
		t.Errorf("Detect() = %q, want %q", result, StatusNeedsAttention)
	}
}

func TestClaudeCodeDetector_PromptWithoutBorderIgnored(t *testing.T) {
	d := &ClaudeCodeDetector{}
	// ❯ appears in conversation text without a border above — not the input field
	prev := "old content"
	content := `Some output mentioning the ❯ character
more output here
`
	result := d.Detect(content, prev)
	if result != StatusWorking {
		t.Errorf("Detect() = %q, want %q (❯ without border should not trigger attention)", result, StatusWorking)
	}
}

func TestClaudeCodeDetector_Unknown(t *testing.T) {
	tests := []struct {
		name    string
		content string
		prev    string
	}{
		{
			name:    "empty content no previous",
			content: "",
			prev:    "",
		},
		{
			name:    "first poll (no previous content)",
			content: "Loading Claude Code...\nInitializing...\n",
			prev:    "",
		},
		{
			name:    "content unchanged",
			content: "same content\n",
			prev:    "same content\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &ClaudeCodeDetector{}
			result := d.Detect(tt.content, tt.prev)
			if result != StatusUnknown {
				t.Errorf("Detect() = %q, want %q", result, StatusUnknown)
			}
		})
	}
}

func TestClaudeCodeDetector_ContentChangeTakesPrecedence(t *testing.T) {
	d := &ClaudeCodeDetector{}
	border := "──────────────────────────────────────────"
	// Content changed AND has input field — working wins because
	// input field is always visible in Claude Code, even during work
	prev := "old output\n"
	content := `⏺ Here's the answer.

` + border + "\n❯\n" + border + "\n"

	result := d.Detect(content, prev)
	if result != StatusWorking {
		t.Errorf("Detect() = %q, want %q (content change should take precedence)", result, StatusWorking)
	}
}

func TestHasInputField(t *testing.T) {
	border := "──────────────────────────────────────────"

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "standard input field",
			content:  "some output\n" + border + "\n❯\n" + border,
			expected: true,
		},
		{
			name:     "input field with user text",
			content:  "some output\n" + border + "\n❯ hello\n" + border,
			expected: true,
		},
		{
			name:     "❯ without border above",
			content:  "some output\n❯\nmore stuff",
			expected: false,
		},
		{
			name:     "border without ❯",
			content:  "some output\n" + border + "\nno prompt here",
			expected: false,
		},
		{
			name:     "empty content",
			content:  "",
			expected: false,
		},
		{
			name:     "short border (less than 10 ─)",
			content:  "output\n─────────\n❯\n",
			expected: false,
		},
		{
			name:     "exactly 10 ─ chars",
			content:  "output\n──────────\n❯\n",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := splitLines(tt.content)
			result := hasInputField(lines)
			if result != tt.expected {
				t.Errorf("hasInputField() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	result := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func TestDetectorForSource(t *testing.T) {
	tests := []struct {
		name   string
		source Source
		isNull bool
	}{
		{
			name:   "claude-code returns ClaudeCodeDetector",
			source: SourceClaudeCode,
			isNull: false,
		},
		{
			name:   "unknown source returns NullDetector",
			source: Source("unknown_tool"),
			isNull: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := DetectorForSource(tt.source)
			_, isNull := d.(*NullDetector)
			if isNull != tt.isNull {
				t.Errorf("DetectorForSource(%q) isNull = %v, want %v", tt.source, isNull, tt.isNull)
			}
		})
	}
}

func TestNullDetector(t *testing.T) {
	d := &NullDetector{}
	result := d.Detect("anything", "previous")
	if result != StatusUnknown {
		t.Errorf("NullDetector.Detect() = %q, want %q", result, StatusUnknown)
	}
}
