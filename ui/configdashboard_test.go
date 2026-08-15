package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// sampleConfigDashboardRows is the four Work-agent-group keys in the four
// provenance states the preview must tell apart, plus one key that declares a
// reach — the shape the component is built for, without config having to be
// reachable from here.
func sampleConfigDashboardRows() []ConfigDashboardRow {
	return []ConfigDashboardRow{
		{
			Key:        "work.implement.agents",
			Desc:       "Ordered fallback agent list for unpinned tasks.",
			Overridden: true,
			Preview: ConfigDashboardPreview{
				ValueTOML:        "work.implement.agents = [\n  \"codex --model gpt\",\n]",
				Provenance:       "override",
				SourceTOML:       "work.implement.agents = [\n  \"claude\",\n]",
				SourceProvenance: "config.toml",
			},
		},
		{
			Key:  "work.verify.agents",
			Desc: "Ordered fallback agent list for the Verifier.",
			Preview: ConfigDashboardPreview{
				ValueTOML:  "work.verify.agents = []",
				Provenance: "fallthrough → work.implement.agents",
				Note:       "no override — falls through to the implement list",
			},
		},
		{
			Key:        "work.routine.agents",
			Desc:       "Ordered fallback agent list for this kind of work.",
			Overridden: true,
			Preview: ConfigDashboardPreview{
				ValueTOML:        "work.routine.agents = []",
				Provenance:       "override",
				Note:             "override set to an empty list — fallthrough disabled",
				SourceProvenance: "fallthrough → work.implement.agents",
			},
		},
		{
			Key:  "work.attended.agents",
			Desc: "Ordered fallback agent list for attended sessions.",
			Preview: ConfigDashboardPreview{
				ValueTOML:  "work.attended.agents = []",
				Provenance: "built-in default",
			},
		},
		{
			Key:  "worktree.root",
			Desc: "Where managed worktrees are created.",
			Preview: ConfigDashboardPreview{
				ValueTOML:  `worktree.root = "~/worktrees"`,
				Provenance: "config.toml",
				Reach: []ConfigDashboardReachLine{
					{Actor: "claude", Detail: "--add-dir ~/worktrees"},
				},
			},
		},
	}
}

func newSizedConfigDashboard(rows []ConfigDashboardRow, width, height int) *ConfigDashboard {
	return newSizedConfigDashboardWith(rows, ConfigDashboardOpts{}, width, height)
}

func newSizedConfigDashboardWith(rows []ConfigDashboardRow, opts ConfigDashboardOpts, width, height int) *ConfigDashboard {
	m := NewConfigDashboard(rows, opts)
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

func typeConfigQuery(m *ConfigDashboard, query string) {
	for _, r := range query {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// configDashboardView renders the component the way a host and the standalone
// program both see it.
func configDashboardView(m *ConfigDashboard) string {
	return StripANSI(m.ViewContent())
}

func TestConfigDashboardViewGoldenList(t *testing.T) {
	m := newSizedConfigDashboard(sampleConfigDashboardRows(), 140, 30)
	got := configDashboardView(m)

	for _, want := range []string{
		"Config · what is in force here",
		"work.implement.agents",
		"work.verify.agents",
		"work.routine.agents",
		"work.attended.agents",
		// The description sits dim under its own path where height allows.
		"Ordered fallback agent list for unpinned tasks.",
		"Ordered fallback agent list for the Verifier.",
		"type to filter",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("view missing %q:\n%s", want, got)
		}
	}
}

// TestConfigDashboardShortPaneDropsDescriptions pins the "where height allows"
// half: the path identifies the key, so it is what survives a pane that cannot
// carry a second line per row.
func TestConfigDashboardShortPaneDropsDescriptions(t *testing.T) {
	m := newSizedConfigDashboard(sampleConfigDashboardRows(), 100, 12)
	got := configDashboardView(m)

	if !strings.Contains(got, "work.attended.agents") {
		t.Fatalf("short pane dropped a key path:\n%s", got)
	}
	if strings.Contains(got, "Ordered fallback agent list for unpinned tasks.") {
		t.Fatalf("short pane still renders descriptions:\n%s", got)
	}
}

func TestConfigDashboardMarksOverriddenRows(t *testing.T) {
	m := newSizedConfigDashboard(sampleConfigDashboardRows(), 100, 30)
	got := configDashboardView(m)

	for _, row := range sampleConfigDashboardRows() {
		marked := strings.Contains(got, configOverrideMarker+" "+row.Key)
		if marked != row.Overridden {
			t.Errorf("row %q marked=%v, want %v:\n%s", row.Key, marked, row.Overridden, got)
		}
	}
}

func TestConfigDashboardFiltersOverPathAndDescription(t *testing.T) {
	t.Run("over the dotted path", func(t *testing.T) {
		m := newSizedConfigDashboard(sampleConfigDashboardRows(), 100, 30)
		typeConfigQuery(m, "routine")
		got := configDashboardView(m)

		if !strings.Contains(got, "work.routine.agents") {
			t.Fatalf("filtered view lost its match:\n%s", got)
		}
		for _, gone := range []string{"work.verify.agents", "work.attended.agents", "worktree.root"} {
			if strings.Contains(got, gone) {
				t.Errorf("filtered view still lists %q:\n%s", gone, got)
			}
		}
	})

	t.Run("over the description text", func(t *testing.T) {
		m := newSizedConfigDashboard(sampleConfigDashboardRows(), 100, 30)
		// "Verifier" appears only in a description, never in a key path.
		typeConfigQuery(m, "verifier")
		got := configDashboardView(m)

		if !strings.Contains(got, "work.verify.agents") {
			t.Fatalf("description match not listed:\n%s", got)
		}
		// The implement key is named by the verify preview's fallthrough line, so
		// the assertion is about its own row: only a listed row carries a marker
		// column ahead of the path.
		if strings.Contains(got, "  work.implement.agents ") {
			t.Errorf("filtered view still lists a non-matching key:\n%s", got)
		}
	})

	t.Run("no match says so", func(t *testing.T) {
		m := newSizedConfigDashboard(sampleConfigDashboardRows(), 100, 30)
		typeConfigQuery(m, "zzzz")
		got := configDashboardView(m)

		if !strings.Contains(got, "no key matches the filter") {
			t.Fatalf("empty filter result unexplained:\n%s", got)
		}
	})
}

// TestConfigDashboardPreviewProvenanceStates walks the cursor down every state
// the preview must render distinguishably (ADR-0202 decision 12).
func TestConfigDashboardPreviewProvenanceStates(t *testing.T) {
	cases := []struct {
		down int
		want []string
		gone []string
	}{
		{
			down: 0,
			want: []string{
				"work.implement.agents = [",
				`"codex --model gpt"`,
				"from: override",
				"without the override: config.toml",
				`"claude"`,
			},
		},
		{
			down: 1,
			want: []string{
				"work.verify.agents = []",
				"from: fallthrough → work.implement.agents",
				"no override — falls through to the implement list",
			},
			gone: []string{"without the override"},
		},
		{
			down: 2,
			want: []string{
				"work.routine.agents = []",
				"from: override",
				"override set to an empty list — fallthrough disabled",
				"without the override: fallthrough → work.implement.agents",
			},
		},
		{
			down: 3,
			want: []string{
				"work.attended.agents = []",
				"from: built-in default",
			},
			gone: []string{"without the override", "reach:"},
		},
		{
			down: 4,
			want: []string{
				`worktree.root = "~/worktrees"`,
				"from: config.toml",
				"reach:",
				"claude  --add-dir ~/worktrees",
			},
		},
	}

	for _, tc := range cases {
		m := newSizedConfigDashboard(sampleConfigDashboardRows(), 100, 30)
		for i := 0; i < tc.down; i++ {
			m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		row, _ := m.Selected()
		got := configDashboardView(m)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("preview of %q missing %q:\n%s", row.Key, want, got)
			}
		}
		for _, gone := range tc.gone {
			if strings.Contains(got, gone) {
				t.Errorf("preview of %q unexpectedly contains %q:\n%s", row.Key, gone, got)
			}
		}
	}
}

// TestConfigDashboardEscCloses keeps the closing binding honest.
func TestConfigDashboardEscCloses(t *testing.T) {
	m := newSizedConfigDashboard(sampleConfigDashboardRows(), 100, 30)
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd == nil {
		t.Fatal("esc returned no command, want quit")
	}
	if !m.Done() {
		t.Fatal("Done() = false after esc")
	}
}

// TestConfigDashboardFailureIsARowNotStdout pins host contract two (ADR-0202
// decision 11): an error appears inside this view, because in the picker hosts
// stdout is a data channel.
func TestConfigDashboardFailureIsARowNotStdout(t *testing.T) {
	m := newSizedConfigDashboard(sampleConfigDashboardRows(), 100, 30)
	m.Fail("could not read config.override.toml")
	if !strings.Contains(configDashboardView(m), "could not read config.override.toml") {
		t.Fatalf("failure not rendered in the view:\n%s", configDashboardView(m))
	}
}
