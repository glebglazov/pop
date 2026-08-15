package ui

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A row may hold prose composed from several layers rather than one config value
// (ADR-0212 decision 8). These tests pin what that costs this component: it shows
// the caller's text, it edits the layer the caller nominates rather than the
// preview, and it still says nothing on stdout.

// proseWriter is a caller whose one row is prose: the preview is a whole stack
// and the edit writes the top layer of it.
type proseWriter struct {
	overlay   string
	stored    []string
	copyErr   error
	storeErr  error
	copyCalls int
}

const proseLayers = `commits — 2 of 4 layers speak, lowest rank first.

1. USER DEFAULTS — yours, every repository
/home/g/.agents/docs/commits.md

Imperative subjects.

2. REPOSITORY — the team's, in version control
/r/docs/agents/commits.md

Conventional commits.`

func (w *proseWriter) Store(key, buffer string) (string, error) {
	w.stored = append(w.stored, buffer)
	if w.storeErr != nil {
		return "", w.storeErr
	}
	w.overlay = strings.TrimSpace(buffer)
	return "", nil
}

func (w *proseWriter) CopySource(string) error {
	w.copyCalls++
	if w.copyErr != nil {
		return w.copyErr
	}
	return nil
}

func (w *proseWriter) Remove(string) error {
	w.overlay = ""
	return nil
}

func (w *proseWriter) Rows() ([]ConfigDashboardRow, error) {
	row := ConfigDashboardRow{
		Key:        "conventions.commits",
		Desc:       "How this repository writes commits.",
		Overridden: w.overlay != "",
		Preview: ConfigDashboardPreview{
			Layers:   proseLayers,
			EditSeed: ConfigEditorNote("your commits overlay.") + w.overlay,
		},
	}
	return []ConfigDashboardRow{row}, nil
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

// The preview is the caller's own text — every layer that speaks, in the order
// the caller put them — and none of the config-format furniture, there being no
// single layer for a `from:` line to name.
func TestConfigDashboardProseRowPreviewsItsLayers(t *testing.T) {
	m, _ := proseConfigDashboard(t, nil)

	got := configDashboardView(m)
	for _, want := range []string{
		"conventions.commits",
		"USER DEFAULTS",
		"REPOSITORY",
		"Imperative subjects.",
		"Conventional commits.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("view missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "from:") {
		t.Errorf("a prose row previews a provenance line it has no single layer for:\n%s", got)
	}
}

// Enter edits the one layer the caller nominates, in a Markdown buffer, and pop's
// own notes come back out of it: the buffer is the value here, so a note left in
// would be written into the human's document.
func TestConfigDashboardProseRowEditsItsOwnLayer(t *testing.T) {
	var paths []string
	var seeds []string
	editor := func(path string, done tea.ExecCallback) tea.Cmd {
		paths = append(paths, path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read seed: %v", err)
		}
		seeds = append(seeds, string(data))
		if err := os.WriteFile(path, []byte("# pop: this note is pop's\nAlways sign off.\n"), 0o644); err != nil {
			t.Errorf("write reply: %v", err)
		}
		return func() tea.Msg { return done(nil) }
	}
	m, writer := proseConfigDashboard(t, editor)

	enter(m)

	if len(paths) != 1 || !strings.HasSuffix(paths[0], ".md") {
		t.Fatalf("editor buffers = %v, want one Markdown file", paths)
	}
	// The seed is the layer being edited, not the composed preview: seeding with
	// the stack would have the human hand back layers they never wrote.
	if strings.Contains(seeds[0], "REPOSITORY") {
		t.Errorf("the editor was seeded with the whole stack:\n%s", seeds[0])
	}
	if !strings.Contains(seeds[0], "your commits overlay.") {
		t.Errorf("the editor was not seeded with the caller's own text:\n%s", seeds[0])
	}
	if writer.overlay != "Always sign off." {
		t.Errorf("stored %q, want the prose alone with pop's note taken back out", writer.overlay)
	}
	if row, _ := m.Selected(); !row.Overridden {
		t.Errorf("row = %+v, want the human's own layer marked in force", row)
	}
}

// The row the caller marks as having no single value below it refuses the copy,
// and the refusal is a row in this view — not a line on stdout, which in two of
// the three hosts is a `cd` argument.
func TestConfigDashboardProseRowRefusalStaysInTheView(t *testing.T) {
	var view string
	out := captureStdout(t, func() {
		m, writer := proseConfigDashboard(t, nil)
		writer.copyErr = errors.New("the commits layers compose, so there is no single value to copy down")
		ctrl(m, 'y')
		view = m.ViewContent()
	})

	if out != "" {
		t.Fatalf("the component wrote to stdout: %q", out)
	}
	if !strings.Contains(view, "no single value to copy down") {
		t.Fatalf("the refusal is nowhere in the view:\n%s", view)
	}
}
