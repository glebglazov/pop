package ui

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// ADR-0202 decision 11's second half: the component never writes to stdout, on
// any path including error. In two of its three hosts stdout is a data channel —
// the documented worktree binding is `cd "$(pop worktree dashboard)"` — so a
// stray line there is a directory change, not a diagnostic. Every error surfaces
// as a row in its own view instead.

// rowsFailingWriter fails the re-read that follows a write, which is the one
// error path that arrives after the component has already changed the layer.
type rowsFailingWriter struct{ *fakeOverrideWriter }

func (w rowsFailingWriter) Rows() ([]ConfigDashboardRow, error) {
	return nil, errors.New("config.override.toml is not readable")
}

// captureStdout swaps os.Stdout for a pipe, runs body, and returns everything
// written to it.
func captureStdout(t *testing.T, body func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	real := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	defer func() {
		os.Stdout = real
		_ = w.Close()
		_ = r.Close()
	}()
	body()
	os.Stdout = real
	_ = w.Close()
	return <-done
}

func TestConfigDashboardWritesNothingToStdoutOnAnyErrorPath(t *testing.T) {
	var views []string
	out := captureStdout(t, func() {
		// A failed action: the layer refuses to remove the override.
		editor := &scriptedEditor{replies: []string{`work.verify.agents = ["codex"]`}}
		m, writer := editingConfigDashboard(t, editor)
		writer.removeErr = errors.New("the override file is read-only")
		ctrl(m, 'x')
		views = append(views, m.ViewContent())

		// A failed editor: the process itself came back with an error.
		broken := &scriptedEditor{err: errors.New("editor exited 1")}
		m, _ = editingConfigDashboard(t, broken)
		enter(m)
		views = append(views, m.ViewContent())

		// A write that landed but cannot be read back.
		writer = newFakeOverrideWriter()
		rows, err := writer.Rows()
		if err != nil {
			t.Errorf("Rows() error: %v", err)
			return
		}
		m = newSizedConfigDashboardWith(rows, ConfigDashboardOpts{Writer: rowsFailingWriter{writer}}, 100, 30)
		ctrl(m, 'y')
		views = append(views, m.ViewContent())

		// And the host's own way in: a failure it hands the component to show.
		m, _ = editingConfigDashboard(t, nil)
		m.Fail("Could not read the config keys: no such file")
		views = append(views, m.ViewContent())
	})

	if out != "" {
		t.Fatalf("the component wrote to stdout across its error paths:\n%q", out)
	}
	for i, want := range []string{
		"the override file is read-only",
		"editor exited 1",
		"config.override.toml is not readable",
		"Could not read the config keys: no such file",
	} {
		if !strings.Contains(views[i], want) {
			t.Fatalf("error %d is nowhere in the view — it went somewhere else entirely:\n%s", i, views[i])
		}
	}
}
