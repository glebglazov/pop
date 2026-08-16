package ui

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeOverrideWriter is an override layer in memory. It answers rows off the
// state its own calls leave behind, so a test asserts on what the human would
// see next rather than on the calls alone.
type fakeOverrideWriter struct {
	source    map[string]string // key → the `key = value` line below any override
	override  map[string]string // key → the stored override, keyed the same way
	problem   string            // refuse the next Store with this, once
	storeErr  error
	removeErr error

	stored  []string // buffers handed to Store, in order
	copied  []string
	removed []string
}

func newFakeOverrideWriter() *fakeOverrideWriter {
	return &fakeOverrideWriter{
		source: map[string]string{
			"work.implement.agents": `work.implement.agents = ["claude"]`,
			"work.verify.agents":    "work.verify.agents = []",
		},
		override: map[string]string{},
	}
}

func (w *fakeOverrideWriter) Store(key, buffer string) (string, error) {
	w.stored = append(w.stored, buffer)
	if w.storeErr != nil {
		return "", w.storeErr
	}
	if w.problem != "" {
		problem := w.problem
		w.problem = ""
		return problem, nil
	}
	w.override[key] = strings.TrimSpace(stripEditorNotes(buffer))
	return "", nil
}

func (w *fakeOverrideWriter) CopySource(key string) error {
	w.copied = append(w.copied, key)
	w.override[key] = w.source[key]
	return nil
}

func (w *fakeOverrideWriter) Remove(key string) error {
	w.removed = append(w.removed, key)
	if w.removeErr != nil {
		return w.removeErr
	}
	delete(w.override, key)
	return nil
}

func (w *fakeOverrideWriter) Rows() ([]ConfigDashboardRow, error) {
	keys := []string{"work.implement.agents", "work.verify.agents"}
	rows := make([]ConfigDashboardRow, 0, len(keys))
	for _, key := range keys {
		row := ConfigDashboardRow{Key: key, Desc: "Ordered fallback agent list."}
		if value, ok := w.override[key]; ok {
			row.Overridden = true
			row.Preview = ConfigDashboardPreview{
				ValueTOML:        value,
				Provenance:       "override",
				SourceTOML:       w.source[key],
				SourceProvenance: "config.toml",
			}
			rows = append(rows, row)
			continue
		}
		row.Preview = ConfigDashboardPreview{ValueTOML: w.source[key], Provenance: "config.toml"}
		rows = append(rows, row)
	}
	return rows, nil
}

// scriptedEditor stands in for $EDITOR: it records what pop seeded the buffer
// with and leaves the next scripted reply in its place. Replies past the end of
// the script are an empty buffer, which is a cancel.
type scriptedEditor struct {
	replies []string
	seeds   []string
	err     error
}

func (e *scriptedEditor) open(path string, done tea.ExecCallback) tea.Cmd {
	data, err := os.ReadFile(path)
	if err != nil {
		return func() tea.Msg { return done(err) }
	}
	e.seeds = append(e.seeds, string(data))
	reply := ""
	if len(e.seeds) <= len(e.replies) {
		reply = e.replies[len(e.seeds)-1]
	}
	if err := os.WriteFile(path, []byte(reply), 0o644); err != nil {
		return func() tea.Msg { return done(err) }
	}
	return func() tea.Msg { return done(e.err) }
}

// editingConfigDashboard wires the component the way a host does, over the two
// fakes.
func editingConfigDashboard(t *testing.T, editor *scriptedEditor) (*ConfigDashboard, *fakeOverrideWriter) {
	t.Helper()
	writer := newFakeOverrideWriter()
	rows, err := writer.Rows()
	if err != nil {
		t.Fatalf("Rows() error: %v", err)
	}
	opts := ConfigDashboardOpts{Writer: writer}
	if editor != nil {
		opts.Editor = editor.open
	}
	return newSizedConfigDashboardWith(rows, opts, 100, 30), writer
}

// press sends one key and runs the commands it produces to exhaustion, the way
// the tea runtime would.
func press(m *ConfigDashboard, msg tea.KeyPressMsg) {
	_, cmd := m.Update(msg)
	for cmd != nil {
		next := cmd()
		if next == nil {
			return
		}
		_, cmd = m.Update(next)
	}
}

func enter(m *ConfigDashboard) { press(m, tea.KeyPressMsg{Code: tea.KeyEnter}) }

func ctrl(m *ConfigDashboard, r rune) {
	press(m, tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl})
}

// TestConfigDashboardEnterSeedsTheValueInForce is ADR-0202 decision 7's buffer:
// the whole `key = value` line, so the editor never has to infer what came back
// and the human starts from something real.
func TestConfigDashboardEnterSeedsTheValueInForce(t *testing.T) {
	t.Run("a key with no override seeds the source value", func(t *testing.T) {
		editor := &scriptedEditor{replies: []string{`work.implement.agents = ["codex"]`}}
		m, writer := editingConfigDashboard(t, editor)

		enter(m)

		if len(editor.seeds) != 1 {
			t.Fatalf("editor opened %d times, want once", len(editor.seeds))
		}
		if !strings.Contains(editor.seeds[0], `work.implement.agents = ["claude"]`) {
			t.Errorf("seed = %q, want the source value copied down", editor.seeds[0])
		}
		if got := writer.override["work.implement.agents"]; got != `work.implement.agents = ["codex"]` {
			t.Errorf("stored %q, want what came back from the editor", got)
		}
	})

	t.Run("a key with an override seeds the override", func(t *testing.T) {
		editor := &scriptedEditor{replies: []string{
			`work.implement.agents = ["codex"]`,
			`work.implement.agents = ["codex", "claude"]`,
		}}
		m, writer := editingConfigDashboard(t, editor)

		enter(m)
		enter(m)

		if len(editor.seeds) != 2 {
			t.Fatalf("editor opened %d times, want twice", len(editor.seeds))
		}
		if !strings.Contains(editor.seeds[1], `["codex"]`) {
			t.Errorf("second seed = %q, want the override now in force", editor.seeds[1])
		}
		if got := writer.override["work.implement.agents"]; got != `work.implement.agents = ["codex", "claude"]` {
			t.Errorf("stored %q, want the second edit", got)
		}
	})
}

// TestConfigDashboardEmptyBufferCancels: returning nothing changes nothing —
// not the override, and not its absence (ADR-0202 decision 7).
func TestConfigDashboardEmptyBufferCancels(t *testing.T) {
	t.Run("on a key with no override", func(t *testing.T) {
		editor := &scriptedEditor{replies: []string{"   \n\t\n"}}
		m, writer := editingConfigDashboard(t, editor)

		enter(m)

		if len(writer.stored) != 0 {
			t.Fatalf("Store called with %q on an empty buffer", writer.stored)
		}
		if _, ok := writer.override["work.implement.agents"]; ok {
			t.Error("an empty buffer created an override")
		}
	})

	t.Run("on a key that already carries one", func(t *testing.T) {
		editor := &scriptedEditor{replies: []string{`work.implement.agents = ["codex"]`, "\n\n"}}
		m, writer := editingConfigDashboard(t, editor)

		enter(m)
		enter(m)

		if len(writer.stored) != 1 {
			t.Fatalf("%d Store calls, want only the first edit", len(writer.stored))
		}
		if got := writer.override["work.implement.agents"]; got != `work.implement.agents = ["codex"]` {
			t.Errorf("override = %q, want it left exactly as it was", got)
		}
		if len(writer.removed) != 0 {
			t.Error("an empty buffer removed the override; it is a cancel, not a deletion")
		}
	})
}

// TestConfigDashboardProblemReopensTheEditor is ADR-0202 decision 8: a value
// that would produce a config finding goes back to the human with the problem
// on top of it, and nothing is written in the meantime.
func TestConfigDashboardProblemReopensTheEditor(t *testing.T) {
	editor := &scriptedEditor{replies: []string{
		`work.implement.agents = [{ display_name = "Claude" }]`,
		`work.implement.agents = ["claude --model opus"]`,
	}}
	m, writer := editingConfigDashboard(t, editor)
	writer.problem = "Loading this value would report: agents entry 1 is malformed"

	enter(m)

	if len(editor.seeds) != 2 {
		t.Fatalf("editor opened %d times, want a second pass at the problem", len(editor.seeds))
	}
	second := editor.seeds[1]
	if !strings.Contains(second, "agents entry 1 is malformed") {
		t.Errorf("re-opened buffer does not show the problem:\n%s", second)
	}
	if !strings.Contains(second, `display_name = "Claude"`) {
		t.Errorf("re-opened buffer lost the text that caused it:\n%s", second)
	}
	if got := writer.override["work.implement.agents"]; got != `work.implement.agents = ["claude --model opus"]` {
		t.Errorf("stored %q, want only the corrected value", got)
	}
	// The notes are pop's, not the human's: a second refusal must not stack.
	if strings.Count(second, configEditorNote) < 2 {
		t.Errorf("notes are not marked as pop's own:\n%s", second)
	}
}

// TestConfigDashboardRefusalWritesNothing pins the other half of decision 8 for
// the case where the human gives up: a refused value that is then cancelled
// leaves the key exactly as it was.
func TestConfigDashboardRefusalWritesNothing(t *testing.T) {
	editor := &scriptedEditor{replies: []string{"work.implement.agents = [", ""}}
	m, writer := editingConfigDashboard(t, editor)
	writer.problem = "This is not valid TOML: expected a comma"

	enter(m)

	if len(editor.seeds) != 2 {
		t.Fatalf("editor opened %d times, want the problem shown again", len(editor.seeds))
	}
	if _, ok := writer.override["work.implement.agents"]; ok {
		t.Error("a refused value was written after all")
	}
	row, _ := m.Selected()
	if row.Overridden {
		t.Error("the row is marked overridden after a refusal")
	}
}

// TestConfigDashboardCopyAndRemove: the two keys that need no editor, and the
// no-op ADR-0202 decision 6 asks for.
func TestConfigDashboardCopyAndRemove(t *testing.T) {
	t.Run("copy from source writes without opening the editor", func(t *testing.T) {
		editor := &scriptedEditor{}
		m, writer := editingConfigDashboard(t, editor)

		ctrl(m, 'y')

		if len(editor.seeds) != 0 {
			t.Fatalf("editor opened %d times for a copy", len(editor.seeds))
		}
		if got := writer.override["work.implement.agents"]; got != `work.implement.agents = ["claude"]` {
			t.Errorf("override = %q, want the source value", got)
		}
		// And again on a key that already carries one.
		ctrl(m, 'y')
		if len(writer.copied) != 2 {
			t.Errorf("%d copies, want one per keystroke", len(writer.copied))
		}
	})

	t.Run("remove restores the source", func(t *testing.T) {
		editor := &scriptedEditor{}
		m, writer := editingConfigDashboard(t, editor)

		ctrl(m, 'y')
		ctrl(m, 'x')

		if _, ok := writer.override["work.implement.agents"]; ok {
			t.Error("the override survived the remove")
		}
		row, _ := m.Selected()
		if row.Overridden || row.Preview.Provenance != "config.toml" {
			t.Errorf("row = %+v, want the source back in force", row)
		}
	})

	t.Run("remove on a key with no override does nothing", func(t *testing.T) {
		m, writer := editingConfigDashboard(t, &scriptedEditor{})

		ctrl(m, 'x')

		row, _ := m.Selected()
		if row.Overridden {
			t.Error("removing nothing marked the row overridden")
		}
		if len(writer.override) != 0 {
			t.Errorf("override layer = %v, want it still empty", writer.override)
		}
	})
}

// TestConfigDashboardShowsTheWriteAtOnce is the demoable half: the marker and
// the provenance line are the human's confirmation that the write landed, so
// they cannot wait for a re-open.
func TestConfigDashboardShowsTheWriteAtOnce(t *testing.T) {
	editor := &scriptedEditor{replies: []string{`work.implement.agents = ["codex"]`}}
	m, _ := editingConfigDashboard(t, editor)

	before := configDashboardView(m)
	if strings.Contains(before, configOverrideMarker+" work.implement.agents") {
		t.Fatalf("row marked before any write:\n%s", before)
	}

	enter(m)

	after := configDashboardView(m)
	for _, want := range []string{
		configOverrideMarker + " work.implement.agents",
		`work.implement.agents = ["codex"]`,
		"from: override",
		"without the override: config.toml",
	} {
		if !strings.Contains(after, want) {
			t.Errorf("view after the write missing %q:\n%s", want, after)
		}
	}

	ctrl(m, 'x')

	restored := configDashboardView(m)
	if strings.Contains(restored, configOverrideMarker+" work.implement.agents") {
		t.Errorf("marker survived the remove:\n%s", restored)
	}
	if !strings.Contains(restored, "from: config.toml") {
		t.Errorf("provenance did not go back to the source:\n%s", restored)
	}
}

// TestConfigDashboardKeepsTheHighlightAcrossAWrite: the human acts on the row
// they are looking at, and looks at the same row afterwards.
func TestConfigDashboardKeepsTheHighlightAcrossAWrite(t *testing.T) {
	m, _ := editingConfigDashboard(t, &scriptedEditor{})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	ctrl(m, 'y')

	row, ok := m.Selected()
	if !ok || row.Key != "work.verify.agents" {
		t.Fatalf("selected %+v after a write, want the row the human acted on", row)
	}
	if !row.Overridden {
		t.Error("the acted-on row is not the one that changed")
	}
}

// TestConfigDashboardWriteFailureIsARow: a write that genuinely fails is not a
// problem to re-edit, and it must not reach stdout either (decision 11).
func TestConfigDashboardWriteFailureIsARow(t *testing.T) {
	t.Run("from the editor", func(t *testing.T) {
		editor := &scriptedEditor{replies: []string{`work.implement.agents = ["codex"]`}}
		m, writer := editingConfigDashboard(t, editor)
		writer.storeErr = errors.New("no space left on device")

		enter(m)

		if len(editor.seeds) != 1 {
			t.Errorf("editor opened %d times; a failed write is not a problem to re-edit", len(editor.seeds))
		}
		if !strings.Contains(configDashboardView(m), "no space left on device") {
			t.Fatalf("write failure not rendered in the view:\n%s", configDashboardView(m))
		}
	})

	t.Run("from a remove", func(t *testing.T) {
		m, writer := editingConfigDashboard(t, &scriptedEditor{})
		writer.removeErr = errors.New("read-only file system")

		ctrl(m, 'x')

		if !strings.Contains(configDashboardView(m), "read-only file system") {
			t.Fatalf("write failure not rendered in the view:\n%s", configDashboardView(m))
		}
	})
}

// TestConfigDashboardSuccessClearsFailure is the missing half of decision 11:
// the failure row is not forever once shown — a later successful action must
// remove it, or a stale "Could not …" survives past the problem it named.
func TestConfigDashboardSuccessClearsFailure(t *testing.T) {
	m, writer := editingConfigDashboard(t, &scriptedEditor{})
	writer.removeErr = errors.New("read-only file system")

	ctrl(m, 'x')
	if !strings.Contains(configDashboardView(m), "read-only file system") {
		t.Fatalf("setup: failure not shown before the later success:\n%s", configDashboardView(m))
	}

	writer.removeErr = nil
	ctrl(m, 'y') // copy source, on the same row, now succeeds

	if strings.Contains(configDashboardView(m), "read-only file system") {
		t.Errorf("stale failure survived a later successful action:\n%s", configDashboardView(m))
	}
}

// TestConfigDashboardWithoutAWriterIsReadOnly: a host that wires no writer gets
// the preview and nothing else, and says so rather than binding keys that would
// do nothing.
func TestConfigDashboardWithoutAWriterIsReadOnly(t *testing.T) {
	m := newSizedConfigDashboard(sampleConfigDashboardRows(), 140, 30)

	enter(m)

	got := configDashboardView(m)
	for _, gone := range []string{"enter edit", "C-y copy source", "C-x remove"} {
		if strings.Contains(got, gone) {
			t.Errorf("read-only dashboard advertises %q:\n%s", gone, got)
		}
	}
}

// TestConfigDashboardHelpNamesTheActions keeps the help overlay in step with
// what the component actually does.
func TestConfigDashboardHelpNamesTheActions(t *testing.T) {
	m, _ := editingConfigDashboard(t, &scriptedEditor{})
	entries := map[string]string{}
	for _, entry := range m.helpEntries() {
		entries[entry.Key] = entry.Desc
	}
	for _, key := range []string{"Enter", "C-y", "C-x"} {
		if entries[key] == "" {
			t.Errorf("help does not explain %q: %v", key, entries)
		}
	}
	if !strings.Contains(entries["Enter"], "$EDITOR") {
		t.Errorf("help for Enter = %q, want it to name the editor", entries["Enter"])
	}
}
