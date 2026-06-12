package cmd

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/monitor"
)

// TestPaneAttentionName_TopicPrecedence locks the descriptive parenthetical
// precedence: Note → Topic → Label → pane_current_command.
func TestPaneAttentionName_TopicPrecedence(t *testing.T) {
	paneCommands := map[string]string{"%1": "node"}

	cases := []struct {
		name  string
		entry *monitor.PaneEntry
		want  string
	}{
		{
			name:  "note wins over topic",
			entry: &monitor.PaneEntry{PaneID: "%1", Session: "s", Note: "a note", Topic: "a topic", Label: "claude"},
			want:  "s (a note)",
		},
		{
			name:  "topic wins when no note",
			entry: &monitor.PaneEntry{PaneID: "%1", Session: "s", Topic: "a topic", Label: "claude"},
			want:  "s (a topic)",
		},
		{
			name:  "label used when no note or topic",
			entry: &monitor.PaneEntry{PaneID: "%1", Session: "s", Label: "claude"},
			want:  "s (%1, claude)",
		},
		{
			name:  "pane_current_command used when nothing else",
			entry: &monitor.PaneEntry{PaneID: "%1", Session: "s"},
			want:  "s (%1, node)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := paneAttentionName(tc.entry, paneCommands); got != tc.want {
				t.Errorf("paneAttentionName = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPaneTopicDerived confirms the dimming flag is set only when a Topic shows
// without a Note overriding it.
func TestPaneTopicDerived(t *testing.T) {
	cases := []struct {
		name  string
		entry *monitor.PaneEntry
		want  bool
	}{
		{"topic without note", &monitor.PaneEntry{Topic: "t"}, true},
		{"note overrides topic", &monitor.PaneEntry{Note: "n", Topic: "t"}, false},
		{"no topic", &monitor.PaneEntry{Label: "claude"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := paneTopicDerived(tc.entry); got != tc.want {
				t.Errorf("paneTopicDerived = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestParseSetTopicArgs covers the optional leading pane_id, env fallback, and
// --clear handling.
func TestParseSetTopicArgs(t *testing.T) {
	t.Setenv("TMUX_PANE", "%env")

	t.Run("explicit pane id and topic", func(t *testing.T) {
		pane, topic, err := parseSetTopicArgs(false, []string{"%7", "refactor auth"})
		if err != nil {
			t.Fatal(err)
		}
		if pane != "%7" || topic != "refactor auth" {
			t.Errorf("got pane=%q topic=%q", pane, topic)
		}
	})

	t.Run("topic only falls back to env pane", func(t *testing.T) {
		pane, topic, err := parseSetTopicArgs(false, []string{"writing", "tests"})
		if err != nil {
			t.Fatal(err)
		}
		if pane != "%env" || topic != "writing tests" {
			t.Errorf("got pane=%q topic=%q", pane, topic)
		}
	})

	t.Run("clear with explicit pane id", func(t *testing.T) {
		pane, topic, err := parseSetTopicArgs(true, []string{"%9"})
		if err != nil {
			t.Fatal(err)
		}
		if pane != "%9" || topic != "" {
			t.Errorf("got pane=%q topic=%q", pane, topic)
		}
	})

	t.Run("clear with no args uses env pane", func(t *testing.T) {
		pane, topic, err := parseSetTopicArgs(true, nil)
		if err != nil {
			t.Fatal(err)
		}
		if pane != "%env" || topic != "" {
			t.Errorf("got pane=%q topic=%q", pane, topic)
		}
	})

	t.Run("missing topic without clear errors", func(t *testing.T) {
		if _, _, err := parseSetTopicArgs(false, nil); err == nil {
			t.Error("expected error for missing topic")
		}
	})
}

// TestExtractPromptFromPayload covers parsing the Claude UserPromptSubmit JSON
// and the unparseable/missing cases.
func TestExtractPromptFromPayload(t *testing.T) {
	t.Run("extracts prompt field", func(t *testing.T) {
		payload := `{"hook_event_name":"UserPromptSubmit","session_id":"abc","prompt":"refactor the auth layer"}`
		got, err := extractPromptFromPayload([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if got != "refactor the auth layer" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("absent prompt field yields empty", func(t *testing.T) {
		got, err := extractPromptFromPayload([]byte(`{"hook_event_name":"UserPromptSubmit"}`))
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("unparseable payload errors", func(t *testing.T) {
		if _, err := extractPromptFromPayload([]byte("not json")); err == nil {
			t.Error("expected error for unparseable payload")
		}
	})
}

// TestTruncateTopic locks the truncation boundaries: word cap, char cap,
// whitespace collapse, ellipsis only when cut, and the empty case.
func TestTruncateTopic(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   string
	}{
		{"short prompt passes through whole", "fix the bug", "fix the bug"},
		{"whitespace collapsed", "fix   the\n\tbug", "fix the bug"},
		{"empty prompt", "", ""},
		{"whitespace only", "   \n\t ", ""},
		{
			name:   "over word cap truncates with ellipsis",
			prompt: "one two three four five six seven eight nine ten",
			want:   "one two three four five six seven eight…",
		},
		{
			name:   "exactly word cap passes whole",
			prompt: "one two three four five six seven eight",
			want:   "one two three four five six seven eight",
		},
		{
			name:   "over char cap truncates with ellipsis",
			prompt: "supercalifragilisticexpialidocioussupercalifragilisticexpialidocious",
			want:   "supercalifragilisticexpialidocioussupercalifragilisticexpial…",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateTopic(tc.prompt)
			if got != tc.want {
				t.Errorf("truncateTopic(%q) = %q, want %q", tc.prompt, got, tc.want)
			}
			if tc.want != "" && tc.want != strings.TrimSuffix(tc.want, "…") {
				// ellipsis cases: ensure the visible body (minus ellipsis) is within caps
				body := strings.TrimSuffix(got, "…")
				if len([]rune(body)) > topicMaxChars {
					t.Errorf("body %q exceeds char cap", body)
				}
			}
		})
	}
}

// TestDeriveTopicFromStdin covers the end-to-end derive helper: pane id
// resolution, truncation, and no-op on bad/empty input.
func TestDeriveTopicFromStdin(t *testing.T) {
	t.Setenv("TMUX_PANE", "%env")

	t.Run("derives truncated topic with env pane", func(t *testing.T) {
		payload := `{"prompt":"one two three four five six seven eight nine"}`
		pane, topic, ok := deriveTopicFromStdin(strings.NewReader(payload), nil)
		if !ok {
			t.Fatal("expected ok")
		}
		if pane != "%env" {
			t.Errorf("pane = %q", pane)
		}
		if topic != "one two three four five six seven eight…" {
			t.Errorf("topic = %q", topic)
		}
	})

	t.Run("explicit pane id overrides env", func(t *testing.T) {
		_, _, _ = deriveTopicFromStdin(strings.NewReader(`{"prompt":"hi"}`), []string{"%7"})
		pane, topic, ok := deriveTopicFromStdin(strings.NewReader(`{"prompt":"hi"}`), []string{"%7"})
		if !ok || pane != "%7" || topic != "hi" {
			t.Errorf("got pane=%q topic=%q ok=%v", pane, topic, ok)
		}
	})

	t.Run("unparseable payload is no-op", func(t *testing.T) {
		if _, _, ok := deriveTopicFromStdin(strings.NewReader("garbage"), nil); ok {
			t.Error("expected ok=false for unparseable payload")
		}
	})

	t.Run("empty prompt is no-op", func(t *testing.T) {
		if _, _, ok := deriveTopicFromStdin(strings.NewReader(`{"prompt":"   "}`), nil); ok {
			t.Error("expected ok=false for empty prompt")
		}
	})
}
