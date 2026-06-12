package cmd

import (
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
