package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/monitor"
)

// noPrevTopic is a prevTopicLookup that reports no existing Topic/session.
func noPrevTopic(string) (string, string) { return "", "" }

// withTopicAgents builds a config whose topic_agents recipe list is agents.
func withTopicAgents(agents ...string) *config.Config {
	return &config.Config{PaneMonitoring: &config.PaneMonitoringConfig{TopicAgents: agents}}
}

// TestPaneAttentionName_TopicPrecedence locks the descriptive parenthetical
// precedence: Note → Topic → Label → pane_current_command. The Topic is read
// from the @pop_topic option map (ADR 0058), not from monitor state, so a Note
// in monitor state still overrides a Topic on the pane.
func TestPaneAttentionName_TopicPrecedence(t *testing.T) {
	paneCommands := map[string]string{"%1": "node"}

	cases := []struct {
		name   string
		entry  *monitor.PaneEntry
		topics map[string]string
		want   string
	}{
		{
			name:   "note wins over topic",
			entry:  &monitor.PaneEntry{PaneID: "%1", Session: "s", Note: "a note", Label: "claude"},
			topics: map[string]string{"%1": "a topic"},
			want:   "s (a note)",
		},
		{
			name:   "topic wins when no note",
			entry:  &monitor.PaneEntry{PaneID: "%1", Session: "s", Label: "claude"},
			topics: map[string]string{"%1": "a topic"},
			want:   "s (a topic)",
		},
		{
			name:   "label used when no note or topic",
			entry:  &monitor.PaneEntry{PaneID: "%1", Session: "s", Label: "claude"},
			topics: nil,
			want:   "s (%1, claude)",
		},
		{
			name:   "pane_current_command used when nothing else",
			entry:  &monitor.PaneEntry{PaneID: "%1", Session: "s"},
			topics: nil,
			want:   "s (%1, node)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := paneAttentionName(tc.entry, paneCommands, tc.topics); got != tc.want {
				t.Errorf("paneAttentionName = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPaneTopicDerived confirms the dimming flag is set only when a Topic
// (from @pop_topic) shows without a Note overriding it.
func TestPaneTopicDerived(t *testing.T) {
	cases := []struct {
		name   string
		entry  *monitor.PaneEntry
		topics map[string]string
		want   bool
	}{
		{"topic without note", &monitor.PaneEntry{PaneID: "%1"}, map[string]string{"%1": "t"}, true},
		{"note overrides topic", &monitor.PaneEntry{PaneID: "%1", Note: "n"}, map[string]string{"%1": "t"}, false},
		{"no topic", &monitor.PaneEntry{PaneID: "%1", Label: "claude"}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := paneTopicDerived(tc.entry, tc.topics); got != tc.want {
				t.Errorf("paneTopicDerived = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSetPaneTopicOption verifies the Topic is written to the @pop_topic
// per-pane user-option via set-option -p, and that an empty topic (--clear)
// empties the option rather than touching monitor state.
func TestSetPaneTopicOption(t *testing.T) {
	t.Run("writes topic to @pop_topic", func(t *testing.T) {
		var got []string
		tmux := &deps.MockTmux{CommandFunc: func(args ...string) (string, error) {
			got = args
			return "", nil
		}}
		if err := setPaneTopicOption(tmux, "%7", "auth refactor"); err != nil {
			t.Fatal(err)
		}
		want := []string{"set-option", "-p", "-t", "%7", "@pop_topic", "auth refactor"}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("tmux args = %v, want %v", got, want)
		}
	})

	t.Run("clear empties @pop_topic", func(t *testing.T) {
		var got []string
		tmux := &deps.MockTmux{CommandFunc: func(args ...string) (string, error) {
			got = args
			return "", nil
		}}
		if err := setPaneTopicOption(tmux, "%7", ""); err != nil {
			t.Fatal(err)
		}
		want := []string{"set-option", "-p", "-t", "%7", "@pop_topic", ""}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("tmux args = %v, want %v", got, want)
		}
	})

	t.Run("no pane id is a no-op", func(t *testing.T) {
		tmux := &deps.MockTmux{CommandFunc: func(args ...string) (string, error) {
			t.Errorf("tmux must not be called without a pane id: %v", args)
			return "", nil
		}}
		if err := setPaneTopicOption(tmux, "", "topic"); err != nil {
			t.Fatal(err)
		}
	})
}

// TestPreSeedTopicFromTitle covers the drain pre-seed hook (ADR 0058): at drain
// spawn pop slugifies the task Title (with the same slugifyTopic normalizer
// recipe-derived Topics use) and writes @pop_topic, guarding on the existing
// option so the first task in a whole-set drain wins and a pane outside tmux or
// an unsluggable Title is a no-op.
func TestPreSeedTopicFromTitle(t *testing.T) {
	t.Run("seeds @pop_topic from the slugified Title", func(t *testing.T) {
		t.Setenv("TMUX_PANE", "%7")
		var wrote []string
		tmux := &deps.MockTmux{CommandFunc: func(args ...string) (string, error) {
			switch args[0] {
			case "display-message": // once-per-pane guard read: no prior Topic
				return "proj-x\t\n", nil
			case "set-option":
				wrote = args
				return "", nil
			}
			t.Fatalf("unexpected tmux call: %v", args)
			return "", nil
		}}
		preSeedTopicFromTitle(tmux, 5)("Drain pre-seeds Topic from task Title")
		// slugifyTopic with maxWords=5 keeps the first 5 words; "pre-seeds" splits
		// into two, so the kebab slug is drain/pre/seeds/topic/from.
		want := []string{"set-option", "-p", "-t", "%7", "@pop_topic", "drain-pre-seeds-topic-from"}
		if strings.Join(wrote, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("set-option args = %v, want %v", wrote, want)
		}
	})

	t.Run("uses the same format as recipe-derived Topics", func(t *testing.T) {
		t.Setenv("TMUX_PANE", "%7")
		var seeded string
		tmux := &deps.MockTmux{CommandFunc: func(args ...string) (string, error) {
			if args[0] == "display-message" {
				return "proj-x\t\n", nil
			}
			if args[0] == "set-option" {
				seeded = args[len(args)-1]
			}
			return "", nil
		}}
		title := "Refactor the Auth Layer!"
		preSeedTopicFromTitle(tmux, 5)(title)
		// The pre-seed and the derive path normalize through the one slugifyTopic.
		if want := slugifyTopic(title, 5); seeded != want {
			t.Errorf("pre-seeded slug = %q, want slugifyTopic output %q", seeded, want)
		}
	})

	t.Run("no-op when @pop_topic is already set (first task wins)", func(t *testing.T) {
		t.Setenv("TMUX_PANE", "%7")
		tmux := &deps.MockTmux{CommandFunc: func(args ...string) (string, error) {
			switch args[0] {
			case "display-message":
				return "proj-x\tearlier-topic\n", nil // pane already carries a Topic
			case "set-option":
				t.Errorf("must not re-seed a pane that already has a Topic: %v", args)
			}
			return "", nil
		}}
		preSeedTopicFromTitle(tmux, 5)("A Later Task Title")
	})

	t.Run("no-op outside tmux", func(t *testing.T) {
		t.Setenv("TMUX_PANE", "")
		tmux := &deps.MockTmux{CommandFunc: func(args ...string) (string, error) {
			t.Errorf("tmux must not be touched without a pane: %v", args)
			return "", nil
		}}
		preSeedTopicFromTitle(tmux, 5)("Some Title")
	})

	t.Run("unsluggable Title is a no-op", func(t *testing.T) {
		t.Setenv("TMUX_PANE", "%7")
		tmux := &deps.MockTmux{CommandFunc: func(args ...string) (string, error) {
			if args[0] == "set-option" {
				t.Errorf("punctuation-only Title must not write a Topic: %v", args)
			}
			return "proj-x\t\n", nil
		}}
		preSeedTopicFromTitle(tmux, 5)("!?-.,")
	})
}

// TestDeriveTopic_SkipsPreSeededPane confirms the payoff of the pre-seed (ADR
// 0058): once pop has written @pop_topic at drain spawn, the agent's
// `set-topic --derive` hook sees it via the once-per-pane guard and no-ops —
// no recipe runs, no model call — so a drained pane never re-derives over its
// pre-seeded Topic.
func TestDeriveTopic_SkipsPreSeededPane(t *testing.T) {
	t.Setenv("TMUX_PANE", "%7")
	preSeeded := "drain-pre-seeds-topic-from-task"
	lookup := func(string) (string, string) { return preSeeded, "proj-x" }
	run := func(context.Context, []string, []byte) (string, error) {
		t.Fatal("no recipe must run for a pre-seeded pane")
		return "", nil
	}
	payload := `{"prompt":"refactor the auth layer"}`
	_, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicAgents("claude"), "claude", lookup, run)
	if ok || topic != "" {
		t.Errorf("derive on a pre-seeded pane = (topic=%q ok=%v), want no-op", topic, ok)
	}
}

// TestTopicOptionLookup confirms the guard's source: it reads @pop_topic and
// the session name off the pane via tmux, and yields empties on a tmux error
// so a fresh/gone pane re-derives.
func TestTopicOptionLookup(t *testing.T) {
	t.Run("reads topic and session", func(t *testing.T) {
		tmux := &deps.MockTmux{CommandFunc: func(args ...string) (string, error) {
			if args[0] != "display-message" || args[len(args)-1] != "#{session_name}\t#{@pop_topic}" {
				t.Fatalf("unexpected tmux call: %v", args)
			}
			return "proj-x\tauth refactor\n", nil
		}}
		topic, session := topicOptionLookup(tmux, "%7")
		if topic != "auth refactor" || session != "proj-x" {
			t.Errorf("got topic=%q session=%q", topic, session)
		}
	})

	t.Run("empty option yields empty topic", func(t *testing.T) {
		tmux := &deps.MockTmux{CommandFunc: func(...string) (string, error) {
			return "proj-x\t\n", nil
		}}
		topic, session := topicOptionLookup(tmux, "%7")
		if topic != "" || session != "proj-x" {
			t.Errorf("got topic=%q session=%q, want empty topic", topic, session)
		}
	})

	t.Run("tmux error yields empties", func(t *testing.T) {
		tmux := &deps.MockTmux{CommandFunc: func(...string) (string, error) {
			return "", fmt.Errorf("no such pane")
		}}
		if topic, session := topicOptionLookup(tmux, "%9"); topic != "" || session != "" {
			t.Errorf("got topic=%q session=%q, want empties", topic, session)
		}
	})
}

// TestTmuxPaneTopics confirms the dashboard reads each pane's Topic from
// @pop_topic via list-panes, keeping Topics with spaces intact and omitting
// panes with no Topic.
func TestTmuxPaneTopics(t *testing.T) {
	tmux := &deps.MockTmux{CommandFunc: func(args ...string) (string, error) {
		if args[0] != "list-panes" || args[len(args)-1] != "#{pane_id}\t#{@pop_topic}" {
			t.Fatalf("unexpected tmux call: %v", args)
		}
		return "%1\tauth refactor\n%2\t\n%3\twrite the tests\n", nil
	}}
	topics := tmuxPaneTopicsWith(tmux)
	if topics["%1"] != "auth refactor" {
		t.Errorf("%%1 topic = %q", topics["%1"])
	}
	if _, ok := topics["%2"]; ok {
		t.Errorf("%%2 has no topic but is present: %q", topics["%2"])
	}
	if topics["%3"] != "write the tests" {
		t.Errorf("%%3 topic = %q", topics["%3"])
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
		prompt, transcript, err := parseTopicPayload([]byte(payload), "claude")
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
		prompt, transcript, err := parseTopicPayload([]byte(`{"hook_event_name":"UserPromptSubmit"}`), "claude")
		if err != nil {
			t.Fatal(err)
		}
		if prompt != "" || transcript != "" {
			t.Errorf("got prompt=%q transcript=%q, want empty", prompt, transcript)
		}
	})

	t.Run("unparseable payload errors", func(t *testing.T) {
		if _, _, err := parseTopicPayload([]byte("not json"), "claude"); err == nil {
			t.Error("expected error for unparseable payload")
		}
	})
}

// TestParseTopicPayload_PerAgent locks the per-agent adapter mapping: each
// label maps its hook payload to the prompt text, transcript_path rides only
// for agents that expose one (Claude), and agents/labels with no prompt text
// degrade to an empty prompt without error.
func TestParseTopicPayload_PerAgent(t *testing.T) {
	cases := []struct {
		name           string
		label          string
		payload        string
		wantPrompt     string
		wantTranscript string
		wantErr        bool
	}{
		{
			name:           "claude forwards transcript_path",
			label:          "claude",
			payload:        `{"prompt":"refactor auth","transcript_path":"/tmp/c.jsonl"}`,
			wantPrompt:     "refactor auth",
			wantTranscript: "/tmp/c.jsonl",
		},
		{
			name:       "empty label defaults to claude",
			label:      "",
			payload:    `{"prompt":"hello","transcript_path":"/tmp/c.jsonl"}`,
			wantPrompt: "hello", wantTranscript: "/tmp/c.jsonl",
		},
		{
			name:    "label is case-insensitive",
			label:   "Claude",
			payload: `{"prompt":"hi","transcript_path":"/t.jsonl"}`,
			// Mixed case still resolves to the claude adapter.
			wantPrompt: "hi", wantTranscript: "/t.jsonl",
		},
		{
			name:    "codex maps prompt and drops transcript",
			label:   "codex",
			payload: `{"prompt":"fix the build","transcript_path":"/should/be/ignored.jsonl"}`,
			// codex exposes no transcript_path: even if present it is not forwarded.
			wantPrompt: "fix the build", wantTranscript: "",
		},
		{
			name:       "cursor maps prompt and drops transcript",
			label:      "cursor",
			payload:    `{"hook_event_name":"beforeSubmitPrompt","prompt":"add tests","conversation_id":"x"}`,
			wantPrompt: "add tests", wantTranscript: "",
		},
		{
			name:       "pi maps prompt and drops transcript",
			label:      "pi",
			payload:    `{"prompt":"write docs"}`,
			wantPrompt: "write docs", wantTranscript: "",
		},
		{
			name:       "opencode maps prompt and drops transcript",
			label:      "opencode",
			payload:    `{"prompt":"profile startup"}`,
			wantPrompt: "profile startup", wantTranscript: "",
		},
		{
			name:    "agent without prompt-text exposure degrades to empty",
			label:   "opencode",
			payload: `{"sessionID":"abc"}`,
			// No prompt field: empty prompt, no error — caller degrades to no Topic.
			wantPrompt: "", wantTranscript: "",
		},
		{
			name:    "unknown label degrades to empty without error",
			label:   "future-agent",
			payload: `{"prompt":"anything"}`,
			// No adapter registered: degrade rather than error.
			wantPrompt: "", wantTranscript: "",
		},
		{
			name:    "malformed json errors for claude",
			label:   "claude",
			payload: "not json",
			wantErr: true,
		},
		{
			name:    "malformed json errors for prompt-only agents",
			label:   "codex",
			payload: "{not json",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompt, transcript, err := parseTopicPayload([]byte(tc.payload), tc.label)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got prompt=%q transcript=%q", prompt, transcript)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if prompt != tc.wantPrompt {
				t.Errorf("prompt = %q, want %q", prompt, tc.wantPrompt)
			}
			if transcript != tc.wantTranscript {
				t.Errorf("transcript = %q, want %q", transcript, tc.wantTranscript)
			}
		})
	}
}

// TestDeriveTopic_DegradesWhenNoPromptText confirms the full derive path is a
// silent no-op when the agent's payload exposes no prompt text — no Topic, no
// error — for both an unknown label and a known agent with an empty payload.
func TestDeriveTopic_DegradesWhenNoPromptText(t *testing.T) {
	t.Setenv("TMUX_PANE", "%env")
	cfg := &config.Config{}
	failRun := func(context.Context, []string, []byte) (string, error) {
		t.Fatal("command runner must not be called when degrading")
		return "", nil
	}

	t.Run("opencode payload without prompt", func(t *testing.T) {
		_, _, ok := deriveTopicWith(strings.NewReader(`{"sessionID":"abc"}`), nil, cfg, "opencode", noPrevTopic, failRun)
		if ok {
			t.Error("expected no Topic for prompt-less payload")
		}
	})

	t.Run("unknown label", func(t *testing.T) {
		_, _, ok := deriveTopicWith(strings.NewReader(`{"prompt":"hi"}`), nil, cfg, "future-agent", noPrevTopic, failRun)
		if ok {
			t.Error("expected no Topic for unknown label")
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

// TestSlugifyTopic exercises the normalizer in isolation (ADR 0057): lowercase,
// punctuation stripping, whitespace collapse, the word cap, and empty inputs.
func TestSlugifyTopic(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		maxWords int
		want     string
	}{
		{"lowercases and joins with dashes", "Debugging Auth Middleware", 5, "debugging-auth-middleware"},
		{"strips punctuation", "Fix: the (auth) bug!", 5, "fix-the-auth-bug"},
		{"collapses extra whitespace", "fix   the\t\nbug", 5, "fix-the-bug"},
		{"caps at max words", "one two three four five six seven", 5, "one-two-three-four-five"},
		{"fewer than max words passes through", "fix the bug", 5, "fix-the-bug"},
		{"keeps digits", "issue 42 retry logic", 5, "issue-42-retry-logic"},
		{"empty input yields empty", "", 5, ""},
		{"whitespace-only yields empty", "   \n\t ", 5, ""},
		{"punctuation-only yields empty", "!?-.,", 5, ""},
		{"punctuation between words splits them", "auth-middleware bug", 5, "auth-middleware-bug"},
		{"non-positive max words clamps to one", "alpha beta gamma", 0, "alpha"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := slugifyTopic(tc.text, tc.maxWords); got != tc.want {
				t.Errorf("slugifyTopic(%q, %d) = %q, want %q", tc.text, tc.maxWords, got, tc.want)
			}
		})
	}
}

// TestDeriveTopic_TopicWordsBound confirms the configured topic_words bounds the
// derived slug through the full derive path, and that the default (5) applies
// when the knob is unset.
func TestDeriveTopic_TopicWordsBound(t *testing.T) {
	t.Setenv("TMUX_PANE", "%env")
	failRun := func(context.Context, []string, []byte) (string, error) {
		t.Fatal("recipe runner must not be called with no recipes configured")
		return "", nil
	}
	payload := `{"prompt":"alpha beta gamma delta epsilon zeta eta"}`

	t.Run("default caps at 5 words", func(t *testing.T) {
		_, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, &config.Config{}, "claude", noPrevTopic, failRun)
		if !ok || topic != "alpha-beta-gamma-delta-epsilon" {
			t.Errorf("got topic=%q ok=%v", topic, ok)
		}
	})

	t.Run("topic_words bounds the slug", func(t *testing.T) {
		cfg := &config.Config{PaneMonitoring: &config.PaneMonitoringConfig{TopicWords: 2}}
		_, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, cfg, "claude", noPrevTopic, failRun)
		if !ok || topic != "alpha-beta" {
			t.Errorf("got topic=%q ok=%v", topic, ok)
		}
	})
}

// TestDeriveTopic_Truncation covers the no-recipe (slice 02) derive path:
// pane id resolution, truncation, and no-op on bad/empty input. With no
// topic_agents configured, the recipe runner is never called.
func TestDeriveTopic_Truncation(t *testing.T) {
	t.Setenv("TMUX_PANE", "%env")
	cfg := &config.Config{}
	failRun := func(context.Context, []string, []byte) (string, error) {
		t.Fatal("recipe runner must not be called with no recipes configured")
		return "", nil
	}

	t.Run("derives normalized slug with env pane", func(t *testing.T) {
		payload := `{"prompt":"one two three four five six seven eight nine"}`
		pane, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, cfg, "claude", noPrevTopic, failRun)
		if !ok {
			t.Fatal("expected ok")
		}
		if pane != "%env" {
			t.Errorf("pane = %q", pane)
		}
		// Slug caps at topic_words (default 5), so only the first five words land.
		if topic != "one-two-three-four-five" {
			t.Errorf("topic = %q", topic)
		}
	})

	t.Run("explicit pane id overrides env", func(t *testing.T) {
		pane, topic, ok := deriveTopicWith(strings.NewReader(`{"prompt":"hi"}`), []string{"%7"}, cfg, "claude", noPrevTopic, failRun)
		if !ok || pane != "%7" || topic != "hi" {
			t.Errorf("got pane=%q topic=%q ok=%v", pane, topic, ok)
		}
	})

	t.Run("unparseable payload is no-op", func(t *testing.T) {
		if _, _, ok := deriveTopicWith(strings.NewReader("garbage"), nil, cfg, "claude", noPrevTopic, failRun); ok {
			t.Error("expected ok=false for unparseable payload")
		}
	})

	t.Run("empty prompt is no-op", func(t *testing.T) {
		if _, _, ok := deriveTopicWith(strings.NewReader(`{"prompt":"   "}`), nil, cfg, "claude", noPrevTopic, failRun); ok {
			t.Error("expected ok=false for empty prompt")
		}
	})
}

// TestResolveTopicRecipe locks the recipe vocabulary (ADR 0057): the curated
// claude/ollama recipes, the ollama model argument and its default, the
// cmd:/sh: escape hatch (colons in the command survive), and the
// unknown/empty references that do not resolve.
func TestResolveTopicRecipe(t *testing.T) {
	modelPrompt := "name-this"
	payload := []byte(`{"prompt":"p"}`)

	t.Run("claude runs claude -p --output-format json with the prompt on stdin", func(t *testing.T) {
		r, ok := resolveTopicRecipe("claude")
		if !ok {
			t.Fatal("claude should resolve")
		}
		argv, stdin := r.build(modelPrompt, payload)
		if strings.Join(argv, " ") != "claude -p --output-format json" {
			t.Errorf("argv = %v", argv)
		}
		if string(stdin) != modelPrompt {
			t.Errorf("stdin = %q, want the model prompt", stdin)
		}
	})

	t.Run("ollama uses its model argument", func(t *testing.T) {
		r, ok := resolveTopicRecipe("ollama:llama3.2")
		if !ok {
			t.Fatal("ollama:llama3.2 should resolve")
		}
		argv, stdin := r.build(modelPrompt, payload)
		if strings.Join(argv, " ") != "ollama run llama3.2" {
			t.Errorf("argv = %v", argv)
		}
		if string(stdin) != modelPrompt {
			t.Errorf("stdin = %q, want the model prompt", stdin)
		}
	})

	t.Run("bare ollama falls back to the default model", func(t *testing.T) {
		r, _ := resolveTopicRecipe("ollama")
		argv, _ := r.build(modelPrompt, payload)
		if argv[len(argv)-1] != defaultOllamaModel {
			t.Errorf("argv = %v, want default model %q", argv, defaultOllamaModel)
		}
	})

	t.Run("cmd escape hatch runs sh -c with the JSON payload on stdin", func(t *testing.T) {
		r, ok := resolveTopicRecipe("cmd:my-tool --flag a:b")
		if !ok {
			t.Fatal("cmd: should resolve")
		}
		argv, stdin := r.build(modelPrompt, payload)
		// Only the first colon splits the reference, so colons in the command survive.
		if len(argv) != 3 || argv[0] != "sh" || argv[1] != "-c" || argv[2] != "my-tool --flag a:b" {
			t.Errorf("argv = %v", argv)
		}
		if string(stdin) != string(payload) {
			t.Errorf("stdin = %q, want the JSON payload", stdin)
		}
	})

	t.Run("sh alias resolves the same as cmd", func(t *testing.T) {
		if _, ok := resolveTopicRecipe("sh:echo hi"); !ok {
			t.Error("sh: should resolve")
		}
	})

	t.Run("unknown and empty references do not resolve", func(t *testing.T) {
		for _, ref := range []string{"gpt", "cmd:", "sh:  ", "", "  "} {
			if _, ok := resolveTopicRecipe(ref); ok {
				t.Errorf("ref %q should not resolve", ref)
			}
		}
	})
}

// TestParseClaudeResult confirms the claude recipe extracts only the result
// text from `claude -p --output-format json` and never branches on the error
// shape (ADR 0057): is_error is ignored, a missing result or non-JSON yields "".
func TestParseClaudeResult(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   string
	}{
		{"extracts result", `{"type":"result","is_error":false,"result":"auth refactor"}`, "auth refactor"},
		{"ignores is_error true, still reads result", `{"is_error":true,"result":"some topic"}`, "some topic"},
		{"missing result yields empty", `{"type":"result","is_error":false}`, ""},
		{"non-JSON yields empty", "auth refactor", ""},
		{"surrounding whitespace tolerated", "  {\"result\":\"x\"}\n", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseClaudeResult(tc.stdout); got != tc.want {
				t.Errorf("parseClaudeResult(%q) = %q, want %q", tc.stdout, got, tc.want)
			}
		})
	}
}

// TestDeriveTopic_Recipes covers the pop-owned recipe chain (ADR 0057) with stub
// runners: JSON extraction for the claude recipe, recipe ordering with
// first-non-empty-wins, reason-blind fallthrough on failure/timeout/empty, the
// per-derive JSON payload fed to the cmd: escape hatch, the derive-once guard,
// and the all-fail-with-no-prior-topic truncation fallback.
func TestDeriveTopic_Recipes(t *testing.T) {
	t.Setenv("TMUX_PANE", "%env")
	payload := `{"prompt":"refactor the auth layer","transcript_path":"/tmp/abc.jsonl"}`

	// claudeJSON wraps a result string in claude -p --output-format json shape.
	claudeJSON := func(result string) string {
		return fmt.Sprintf(`{"type":"result","subtype":"success","is_error":false,"result":%q}`, result)
	}

	t.Run("extracts the result text from claude JSON output", func(t *testing.T) {
		run := func(_ context.Context, argv []string, _ []byte) (string, error) {
			if argv[0] != "claude" {
				t.Fatalf("unexpected argv: %v", argv)
			}
			return claudeJSON("Auth Refactor"), nil
		}
		pane, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicAgents("claude"), "claude", noPrevTopic, run)
		if !ok || pane != "%env" || topic != "auth-refactor" {
			t.Errorf("got pane=%q topic=%q ok=%v", pane, topic, ok)
		}
	})

	t.Run("runs recipes in order; first non-empty wins and stops the chain", func(t *testing.T) {
		var calls []string
		run := func(_ context.Context, argv []string, _ []byte) (string, error) {
			calls = append(calls, argv[0])
			switch argv[0] {
			case "claude":
				return "", fmt.Errorf("exit 1") // first recipe fails reason-blind
			case "ollama":
				return "Local Topic\n", nil // second recipe succeeds
			}
			return "", nil
		}
		_, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicAgents("claude", "ollama:llama3.2"), "claude", noPrevTopic, run)
		if !ok || topic != "local-topic" {
			t.Errorf("got topic=%q ok=%v", topic, ok)
		}
		if strings.Join(calls, ",") != "claude,ollama" {
			t.Errorf("recipe call order = %v, want claude then ollama", calls)
		}
	})

	t.Run("first recipe success skips the rest", func(t *testing.T) {
		ran := map[string]bool{}
		run := func(_ context.Context, argv []string, _ []byte) (string, error) {
			ran[argv[0]] = true
			if argv[0] == "claude" {
				return claudeJSON("auth refactor"), nil
			}
			return "should not run", nil
		}
		_, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicAgents("claude", "ollama:llama3.2"), "claude", noPrevTopic, run)
		if !ok || topic != "auth-refactor" {
			t.Errorf("got topic=%q ok=%v", topic, ok)
		}
		if ran["ollama"] {
			t.Error("ollama recipe must not run after claude succeeds")
		}
	})

	t.Run("a recipe whose output normalizes to empty falls through", func(t *testing.T) {
		run := func(_ context.Context, argv []string, _ []byte) (string, error) {
			switch argv[0] {
			case "claude":
				return claudeJSON("!?-.,"), nil // punctuation only → slug ""
			case "ollama":
				return "real topic", nil
			}
			return "", nil
		}
		_, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicAgents("claude", "ollama"), "claude", noPrevTopic, run)
		if !ok || topic != "real-topic" {
			t.Errorf("got topic=%q ok=%v", topic, ok)
		}
	})

	t.Run("unknown recipe references are skipped reason-blind", func(t *testing.T) {
		run := func(_ context.Context, argv []string, _ []byte) (string, error) {
			if argv[0] != "ollama" {
				t.Fatalf("only the ollama recipe should run, got %v", argv)
			}
			return "topic", nil
		}
		_, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicAgents("nope", "ollama:llama3.2"), "claude", noPrevTopic, run)
		if !ok || topic != "topic" {
			t.Errorf("got topic=%q ok=%v", topic, ok)
		}
	})

	t.Run("claude recipe feeds the model prompt on stdin, not the JSON payload", func(t *testing.T) {
		var stdin string
		run := func(_ context.Context, _ []string, in []byte) (string, error) {
			stdin = string(in)
			return claudeJSON("topic"), nil
		}
		_, _, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicAgents("claude"), "claude", noPrevTopic, run)
		if !ok {
			t.Fatal("expected ok")
		}
		if !strings.Contains(stdin, "refactor the auth layer") {
			t.Errorf("model prompt missing the user prompt: %q", stdin)
		}
		// The model prompt is not the JSON payload — only recipes that want it
		// (the cmd: escape hatch) see transcript_path/pane_id.
		if strings.Contains(stdin, "transcript_path") || strings.Contains(stdin, "pane_id") {
			t.Errorf("claude stdin should be the model prompt, not the JSON payload: %q", stdin)
		}
	})

	t.Run("cmd escape hatch receives the per-derive JSON payload on stdin", func(t *testing.T) {
		var got topicRecipePayload
		run := func(_ context.Context, argv []string, in []byte) (string, error) {
			if argv[0] != "sh" || argv[1] != "-c" || argv[2] != "my-tool --topic" {
				t.Fatalf("unexpected escape-hatch argv: %v", argv)
			}
			if err := json.Unmarshal(in, &got); err != nil {
				t.Fatalf("payload not valid JSON: %v", err)
			}
			return "from script\n", nil
		}
		// prev_topic is always empty on a fresh pane (ADR 0025); session rides through.
		lookup := func(string) (string, string) { return "", "sess" }
		_, topic, ok := deriveTopicWith(strings.NewReader(payload), []string{"%5"}, withTopicAgents("cmd:my-tool --topic"), "claude", lookup, run)
		if !ok || topic != "from-script" {
			t.Errorf("got topic=%q ok=%v", topic, ok)
		}
		if got.PrevTopic != "" || got.Prompt != "refactor the auth layer" ||
			got.TranscriptPath != "/tmp/abc.jsonl" || got.PaneID != "%5" || got.Session != "sess" {
			t.Errorf("payload = %+v", got)
		}
	})

	t.Run("derives once: skips every recipe when a Topic already exists", func(t *testing.T) {
		run := func(context.Context, []string, []byte) (string, error) {
			t.Fatal("no recipe must run when a Topic already exists")
			return "", nil
		}
		lookup := func(string) (string, string) { return "existing topic", "sess" }
		_, _, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicAgents("claude"), "claude", lookup, run)
		if ok {
			t.Error("expected ok=false (keep existing Topic)")
		}
	})

	t.Run("normalizes long recipe output into a capped slug", func(t *testing.T) {
		long := strings.Repeat("a", topicMaxChars+10)
		run := func(_ context.Context, _ []string, _ []byte) (string, error) { return long, nil }
		_, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicAgents("ollama"), "claude", noPrevTopic, run)
		if !ok {
			t.Fatal("expected ok")
		}
		if strings.ContainsRune(topic, '…') || len([]rune(topic)) > topicMaxChars {
			t.Errorf("topic = %q not normalized/capped", topic)
		}
	})

	// Reason-blind fallthrough: a nonzero exit, a timeout, and empty output are
	// treated identically — the recipe is skipped.
	failureCases := []struct {
		name string
		run  topicRecipeRunner
	}{
		{"non-zero exit", func(context.Context, []string, []byte) (string, error) { return "", fmt.Errorf("exit 1") }},
		{"timeout", func(context.Context, []string, []byte) (string, error) { return "", context.DeadlineExceeded }},
		{"empty output", func(context.Context, []string, []byte) (string, error) { return "  \n", nil }},
	}
	for _, fc := range failureCases {
		t.Run("all recipes fail with no prior topic falls back to truncation on "+fc.name, func(t *testing.T) {
			pane, topic, ok := deriveTopicWith(strings.NewReader(payload), nil, withTopicAgents("claude", "ollama:llama3.2"), "claude", noPrevTopic, fc.run)
			if !ok || pane != "%env" || topic != "refactor-the-auth-layer" {
				t.Errorf("got pane=%q topic=%q ok=%v on %s", pane, topic, ok, fc.name)
			}
		})
	}
}

// TestRunTopicRecipe exercises the real exec runner end-to-end: argv execution
// with stdin, a non-zero exit, the empty-argv guard, and a real timeout.
func TestRunTopicRecipe(t *testing.T) {
	t.Run("execs argv and returns stdout", func(t *testing.T) {
		out, err := runTopicRecipe(context.Background(), []string{"cat"}, []byte("hello"))
		if err != nil {
			t.Fatal(err)
		}
		if out != "hello" {
			t.Errorf("out = %q", out)
		}
	})

	t.Run("non-zero exit errors", func(t *testing.T) {
		if _, err := runTopicRecipe(context.Background(), []string{"sh", "-c", "exit 3"}, nil); err == nil {
			t.Error("expected error for non-zero exit")
		}
	})

	t.Run("empty argv errors", func(t *testing.T) {
		if _, err := runTopicRecipe(context.Background(), nil, nil); err == nil {
			t.Error("expected error for empty argv")
		}
	})

	t.Run("timeout errors", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := runTopicRecipe(ctx, []string{"sleep", "5"}, nil); err == nil {
			t.Error("expected error for timeout")
		}
	})
}
