package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A row may hold prose composed from several layers rather than one config value
// (ADR-0212 decision 8). These tests pin what that costs this component: it shows
// the caller's text, it offers none of the three write actions on it (ADR-0226),
// and it still says nothing on stdout.

// proseWriter is a caller whose one row is prose. Every action records that it
// was reached, because reaching one at all is the failure.
type proseWriter struct {
	stored    []string
	copyCalls int
	removes   int
}

const proseLayers = `commits — what is in force here.

----- ANSWER: REPOSITORY (the team's, in version control) -----
/r/docs/agents/commits.md

Conventional commits.

Provenance: commits resolved to repository (/r/docs/agents/commits.md).

read-only here. Your own documents for this kind:
  project (yours, this project, not written yet)
    /home/g/.agents/docs/projects/github.com-g-r/commits.md
  overlay (yours, appended to whichever answered, not written yet)
    /home/g/.agents/docs/commits.overlay.md
Write one with ` + "`pop conventions set commits --project`" + ` or ` + "`--overlay`" + `.`

func (w *proseWriter) Store(key, buffer string) (string, error) {
	w.stored = append(w.stored, buffer)
	return "", nil
}

func (w *proseWriter) CopySource(string) error {
	w.copyCalls++
	return nil
}

func (w *proseWriter) Remove(string) error {
	w.removes++
	return nil
}

func (w *proseWriter) Rows() ([]ConfigDashboardRow, error) {
	return []ConfigDashboardRow{{
		Key:     "conventions.commits",
		Desc:    "How this repository writes commits.",
		Preview: ConfigDashboardPreview{Layers: proseLayers},
	}}, nil
}

func proseConfigDashboard(t *testing.T, editor func(string, tea.ExecCallback) tea.Cmd) (*ConfigDashboard, *proseWriter) {
	t.Helper()
	writer := &proseWriter{}
	rows, err := writer.Rows()
	if err != nil {
		t.Fatalf("Rows() error: %v", err)
	}
	return newSizedConfigDashboardWith(rows, ConfigDashboardOpts{Writer: writer, Editor: editor}, 100, 30), writer
}

// The preview is the caller's own text — the answer in force, the provenance
// line and the paths a write would land in — and none of the config-format
// furniture, there being no single layer for a `from:` line to name.
func TestConfigDashboardProseRowPreviewsItsLayers(t *testing.T) {
	m, _ := proseConfigDashboard(t, nil)

	got := configDashboardView(m)
	for _, want := range []string{
		"conventions.commits",
		"ANSWER: REPOSITORY",
		"Conventional commits.",
		"Provenance:",
		"commits.overlay.md",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("view missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "from:") {
		t.Errorf("a prose row previews a provenance line it has no single layer for:\n%s", got)
	}
}

// A prose row is read-only: the three keys do nothing there, no editor opens,
// and the footer offers none of them — a key a footer names must work, and one
// it does not name must not fire (ADR-0226).
func TestConfigDashboardProseRowTakesNoWriteAction(t *testing.T) {
	opened := 0
	editor := func(path string, done tea.ExecCallback) tea.Cmd {
		opened++
		return func() tea.Msg { return done(nil) }
	}
	m, writer := proseConfigDashboard(t, editor)

	enter(m)
	ctrl(m, 'y')
	ctrl(m, 'x')

	if opened != 0 {
		t.Errorf("$EDITOR opened %d times on a read-only row", opened)
	}
	if len(writer.stored) != 0 || writer.copyCalls != 0 || writer.removes != 0 {
		t.Errorf("write side reached: stored=%v copies=%d removes=%d",
			writer.stored, writer.copyCalls, writer.removes)
	}
	got := configDashboardView(m)
	for _, gone := range []string{"enter edit", "C-y copy source", "C-x remove"} {
		if strings.Contains(got, gone) {
			t.Errorf("the footer offers %q on a read-only row:\n%s", gone, got)
		}
	}
}

// The keys are gated on the highlighted row, not on the component: a dashboard
// holding both sorts of row still writes a config key.
func TestConfigDashboardConfigRowStillWritesBesideProseRows(t *testing.T) {
	writer := newFakeOverrideWriter()
	configRows, err := writer.Rows()
	if err != nil {
		t.Fatalf("Rows() error: %v", err)
	}
	proseRow := ConfigDashboardRow{
		Key:     "conventions.commits",
		Preview: ConfigDashboardPreview{Layers: proseLayers},
	}
	editor := &scriptedEditor{replies: []string{`work.implement.agents = ["codex"]`}}
	m := newSizedConfigDashboardWith(append([]ConfigDashboardRow{proseRow}, configRows...),
		ConfigDashboardOpts{Writer: writer, Editor: editor.open}, 100, 30)

	// Move off the prose row onto the first config key.
	press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	enter(m)

	if len(editor.seeds) != 1 {
		t.Fatalf("editor opened %d times on a config row, want once", len(editor.seeds))
	}
	if got := writer.override["work.implement.agents"]; got != `work.implement.agents = ["codex"]` {
		t.Errorf("stored %q, want what came back from the editor", got)
	}
	if !strings.Contains(configDashboardView(m), "enter edit") {
		t.Errorf("the footer does not offer the keys on a config row:\n%s", configDashboardView(m))
	}
}

// The refusal path is a row in this view, never a line on stdout, which in two
// of the three hosts is a `cd` argument.
func TestConfigDashboardProseRowStaysOffStdout(t *testing.T) {
	var view string
	out := captureStdout(t, func() {
		m, _ := proseConfigDashboard(t, nil)
		enter(m)
		ctrl(m, 'y')
		view = m.ViewContent()
	})

	if out != "" {
		t.Fatalf("the component wrote to stdout: %q", out)
	}
	if strings.Contains(view, "Could not") {
		t.Fatalf("a read-only row produced a failure row:\n%s", view)
	}
}
