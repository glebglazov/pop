package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/monitor"
)

// noPrevTopic is a prevTopicLookup that reports no existing Topic/session.
func noPrevTopic(string) (string, string) { return "", "" }

// withTopicCommand builds a config whose topic_command is set to command.
func withTopicCommand(command string) *config.Config {
	return &config.Config{PaneMonitoring: &config.PaneMonitoringConfig{TopicCommand: command}}
}

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

// TestParseTopicPayload covers parsing the Claude UserPromptSubmit JSON,
// the optional transcript_path, and the unparseable/missing cases.
func TestParseTopicPayload(t *testing.T) {
	t.Run("extracts prompt and transcript path", func(t *testing.T) {
		payload := `{"hook_event_name":"UserPromptSubmit","session_id":"abc","prompt":"refactor the auth layer","transcript_path":"/tmp/abc.jsonl"}`
		prompt, transcript, err := parseTopicPayload([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if prompt != "refactor the auth layer" {
			t.Errorf("prompt = %q", prompt)
		}
		if transcript != "/tmp/abc.jsonl" {
			t.Errorf("transcript = %q", transcript)
		}
	})

	t.Run("absent fields yield empty", func(t *testing.T) {
		prompt, transcript, err := parseTopicPayload([]byte(`{"hook_event_name":"UserPromptSubmit"}`))
		if err != nil {
			t.Fatal(err)
		}
		if prompt != "" || transcript != "" {
			t.Errorf("got prompt=%q transcript=%q, want empty", prompt, transcript)
		}
	})

	t.Run("unparseable payload errors", func(t *testing.T) {
		if _, _, err := parseTopicPayload([]byte("not json")); err == nil {
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

// TestDeriveTopic_Truncation covers the no-command (slice 02) derive path:
// pane id resolution, truncation, and no-op on bad/empty input. With no
// topic_command set, the command runner and prev-topic lookup are never used.
func TestDeriveTopic_Truncation(t *testing.T) {
	t.Setenv("TMUX_PANE", "%env")
	cfg := &config.Config{}
	failRun := func(context.Context, string, []byte) (string, error) {
		t.Fatal("command runner must not be called without topic_command")
		return "", nil
	}

	t.Run("derives truncated topic with env pane", func(t *testing.T) {
		payload := `{"prompt":"one two three four five six seven eight nine"}`
		pane, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, cfg, noPrevTopic, failRun)
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
		pane, topic, ok := deriveTopicWith(strings.NewReader(`{"prompt":"hi"}`), []string{"%7"}, cfg, noPrevTopic, failRun)
		if !ok || pane != "%7" || topic != "hi" {
			t.Errorf("got pane=%q topic=%q ok=%v", pane, topic, ok)
		}
	})

	t.Run("unparseable payload is no-op", func(t *testing.T) {
		if _, _, ok := deriveTopicWith(strings.NewReader("garbage"), nil, cfg, noPrevTopic, failRun); ok {
			t.Error("expected ok=false for unparseable payload")
		}
	})

	t.Run("empty prompt is no-op", func(t *testing.T) {
		if _, _, ok := deriveTopicWith(strings.NewReader(`{"prompt":"   "}`), nil, cfg, noPrevTopic, failRun); ok {
			t.Error("expected ok=false for empty prompt")
		}
	})
}

// TestDeriveTopic_Command covers the topic_command delegation path with stub
// commands: success, the JSON contract piped on stdin, capping, and the
// failure/timeout/empty-output fallbacks.
func TestDeriveTopic_Command(t *testing.T) {
	t.Setenv("TMUX_PANE", "%env")
	payload := `{"prompt":"refactor the auth layer","transcript_path":"/tmp/abc.jsonl"}`

	t.Run("uses command stdout as topic", func(t *testing.T) {
		run := func(_ context.Context, _ string, _ []byte) (string, error) {
			return "auth refactor\n", nil
		}
		pane, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicCommand("x"), noPrevTopic, run)
		if !ok || pane != "%env" || topic != "auth refactor" {
			t.Errorf("got pane=%q topic=%q ok=%v", pane, topic, ok)
		}
	})

	t.Run("pipes the normalized JSON payload on stdin", func(t *testing.T) {
		var got topicCommandPayload
		run := func(_ context.Context, _ string, stdin []byte) (string, error) {
			if err := json.Unmarshal(stdin, &got); err != nil {
				t.Fatalf("payload not valid JSON: %v", err)
			}
			return "topic", nil
		}
		lookup := func(string) (string, string) { return "old topic", "sess" }
		_, _, ok := deriveTopicWith(strings.NewReader(payload), []string{"%5"}, withTopicCommand("x"), lookup, run)
		if !ok {
			t.Fatal("expected ok")
		}
		if got.PrevTopic != "old topic" || got.Prompt != "refactor the auth layer" ||
			got.TranscriptPath != "/tmp/abc.jsonl" || got.PaneID != "%5" || got.Session != "sess" {
			t.Errorf("payload = %+v", got)
		}
	})

	t.Run("transcript_path omitted when absent", func(t *testing.T) {
		var stdin []byte
		run := func(_ context.Context, _ string, in []byte) (string, error) {
			stdin = in
			return "topic", nil
		}
		_, _, _ = deriveTopicWith(strings.NewReader(`{"prompt":"hi"}`), nil, withTopicCommand("x"), noPrevTopic, run)
		if strings.Contains(string(stdin), "transcript_path") {
			t.Errorf("transcript_path should be omitted: %s", stdin)
		}
	})

	t.Run("caps long command output", func(t *testing.T) {
		long := strings.Repeat("a", topicMaxChars+10)
		run := func(_ context.Context, _ string, _ []byte) (string, error) { return long, nil }
		_, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicCommand("x"), noPrevTopic, run)
		if !ok {
			t.Fatal("expected ok")
		}
		body := strings.TrimSuffix(topic, "…")
		if len([]rune(body)) > topicMaxChars || !strings.HasSuffix(topic, "…") {
			t.Errorf("topic = %q not capped", topic)
		}
	})

	failureCases := []struct {
		name string
		run  topicCommandRunner
	}{
		{"non-zero exit", func(context.Context, string, []byte) (string, error) { return "", fmt.Errorf("exit 1") }},
		{"timeout", func(ctx context.Context, _ string, _ []byte) (string, error) { return "", context.DeadlineExceeded }},
		{"empty output", func(context.Context, string, []byte) (string, error) { return "  \n", nil }},
	}
	for _, fc := range failureCases {
		t.Run("keeps previous topic on "+fc.name, func(t *testing.T) {
			lookup := func(string) (string, string) { return "kept topic", "sess" }
			_, _, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicCommand("x"), lookup, fc.run)
			if ok {
				t.Errorf("expected ok=false (keep previous) on %s", fc.name)
			}
		})

		t.Run("falls back to truncation with no previous topic on "+fc.name, func(t *testing.T) {
			pane, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicCommand("x"), noPrevTopic, fc.run)
			if !ok || pane != "%env" || topic != "refactor the auth layer" {
				t.Errorf("got pane=%q topic=%q ok=%v on %s", pane, topic, ok, fc.name)
			}
		})
	}
}

// TestRunTopicCommand exercises the real `sh -c` runner end-to-end with stub
// shell commands: success, non-zero exit, and timeout.
func TestRunTopicCommand(t *testing.T) {
	t.Run("success returns stdout", func(t *testing.T) {
		out, err := runTopicCommand(context.Background(), "cat", []byte("hello"))
		if err != nil {
			t.Fatal(err)
		}
		if out != "hello" {
			t.Errorf("out = %q", out)
		}
	})

	t.Run("non-zero exit errors", func(t *testing.T) {
		if _, err := runTopicCommand(context.Background(), "exit 3", nil); err == nil {
			t.Error("expected error for non-zero exit")
		}
	})

	t.Run("timeout errors", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50_000_000) // 50ms
		defer cancel()
		if _, err := runTopicCommand(ctx, "sleep 5", nil); err == nil {
			t.Error("expected error for timeout")
		}
	})
}
