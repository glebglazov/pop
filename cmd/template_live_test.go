//go:build live

package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/layout"
)

func requireTmux(t *testing.T) tmuxmod.Tmux {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not in PATH")
	}
	return tmuxmod.New(config.ConfiguredTmuxSocket())
}

func liveSessionName(t *testing.T, label string) string {
	t.Helper()
	return fmt.Sprintf("pop-live-%s-%d", label, os.Getpid())
}

func liveTemplateDeps(tm tmuxmod.Tmux) templateRuntimeDeps {
	return templateRuntimeDeps{
		Tmux:        tm,
		UserHomeDir: os.UserHomeDir,
		ErrOut:      io.Discard,
	}
}

func newLiveSession(t *testing.T, tm tmuxmod.Tmux, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := tm.NewSession(name, dir); err != nil {
		t.Fatalf("NewSession(%q): %v", name, err)
	}
	t.Cleanup(func() {
		_ = tm.KillSession(name)
	})
	return dir
}

// measureWindowWidth creates a throwaway window in session, reads its initial
// pane width, and removes the window. Detached tmux sessions size new windows
// consistently, so this is the extent realizeContainer will apportion against.
func measureWindowWidth(t *testing.T, tm tmuxmod.Tmux, session, dir string) int {
	t.Helper()
	paneID, err := tm.NewWindow(session, "pop-layout-measure", dir)
	if err != nil {
		t.Fatalf("NewWindow(measure): %v", err)
	}
	w, _, err := tm.PaneSize(paneID)
	if err != nil {
		t.Fatalf("PaneSize(measure): %v", err)
	}
	windowRef := session + ":pop-layout-measure"
	if err := tm.KillWindow(windowRef); err != nil {
		t.Fatalf("KillWindow(measure): %v", err)
	}
	return w
}

func colsInColsWorkbench() config.Workbench {
	return config.Workbench{
		Name: "cols-in-cols",
		Windows: []config.WorkbenchWindow{{
			Name: "work",
			Layout: &config.WorkbenchPaneSpec{
				Children: "columns",
				Panes: []config.WorkbenchPaneSpec{
					{
						Children: "columns",
						Weight:   2,
						Panes: []config.WorkbenchPaneSpec{
							{Name: "a", Command: "sleep 300", Weight: 1},
							{Name: "b", Command: "sleep 300", Weight: 3},
						},
					},
					{Name: "c", Command: "sleep 300", Weight: 1},
				},
			},
		}},
	}
}

func TestLiveWorkbenchSameDirectionNesting(t *testing.T) {
	tm := requireTmux(t)
	session := liveSessionName(t, "cols")
	dir := newLiveSession(t, tm, session)

	extent := measureWindowWidth(t, tm, session, dir)
	outer := layout.Apportion(extent, []int{2, 1})
	inner := layout.Apportion(outer[0], []int{1, 3})
	wantWidth := map[string]int{"a": inner[0], "b": inner[1], "c": outer[1]}

	tmpl := colsInColsWorkbench()
	if err := applyWorkbench(liveTemplateDeps(tm), tmpl, session, dir); err != nil {
		t.Fatalf("applyWorkbench: %v", err)
	}

	gotWidth, err := stampedPaneWidths(tm, session, "work")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range wantWidth {
		if gotWidth[name] != want {
			t.Errorf("pane %q width = %d, want %d (window extent %d)", name, gotWidth[name], want, extent)
		}
	}
}

func crowdedRowsWorkbench(n int) config.Workbench {
	panes := make([]config.WorkbenchPaneSpec, n)
	for i := range panes {
		panes[i] = config.WorkbenchPaneSpec{
			Name:    fmt.Sprintf("p%d", i),
			Command: "sleep 300",
		}
	}
	return config.Workbench{
		Name: "crowded",
		Windows: []config.WorkbenchWindow{{
			Name: "work",
			Layout: &config.WorkbenchPaneSpec{
				Children: "rows",
				Panes:    panes,
			},
		}},
	}
}

func TestLiveWorkbenchRejectsUnfittableLayout(t *testing.T) {
	tm := requireTmux(t)
	session := liveSessionName(t, "crowded")
	dir := newLiveSession(t, tm, session)

	tmpl := crowdedRowsWorkbench(25)
	err := applyWorkbench(liveTemplateDeps(tm), tmpl, session, dir)
	if err == nil {
		t.Fatal("expected unfittable layout to be rejected")
	}
	errMsg := err.Error()
	for _, substr := range []string{`window "work"`, "cannot fit", "layout"} {
		if !strings.Contains(errMsg, substr) {
			t.Errorf("error = %q, want substring %q", errMsg, substr)
		}
	}

	panes, err := tm.WindowPanes(session, "work")
	if err != nil {
		t.Fatalf("WindowPanes: %v", err)
	}
	if len(panes) != 1 {
		t.Errorf("unfittable layout left %d panes in window, want 1 (no splits)", len(panes))
	}
}

func stampedPaneWidths(tm tmuxmod.Tmux, session, windowName string) (map[string]int, error) {
	liveWindows, err := tm.LiveWorkbenchWindows(session)
	if err != nil {
		return nil, fmt.Errorf("LiveWorkbenchWindows: %w", err)
	}
	windowRef, ok := liveWindows[windowName]
	if !ok {
		return nil, fmt.Errorf("window %q not found in session", windowName)
	}
	names, _, err := tm.LivePaneIdentities(windowRef)
	if err != nil {
		return nil, fmt.Errorf("LivePaneIdentities: %w", err)
	}
	widths := make(map[string]int, len(names))
	for name, paneID := range names {
		w, _, err := tm.PaneSize(paneID)
		if err != nil {
			return nil, fmt.Errorf("PaneSize(%q): %w", name, err)
		}
		widths[name] = w
	}
	return widths, nil
}
