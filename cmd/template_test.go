package cmd

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

func TestTemplateCommandTree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path    []string
		wantCmd any
		wantRun any
	}{
		// New canonical paths
		{path: []string{"workbench", "list"}, wantCmd: workbenchListCmd, wantRun: runTemplateList},
		{path: []string{"workbench", "apply"}, wantCmd: workbenchApplyCmd, wantRun: runTemplateApply},
		// Alias
		{path: []string{"wb", "list"}, wantCmd: workbenchListCmd, wantRun: runTemplateList},
		{path: []string{"wb", "apply"}, wantCmd: workbenchApplyCmd, wantRun: runTemplateApply},
		// Deprecated hidden alias still works
		{path: []string{"layout", "list"}, wantCmd: layoutListCmd, wantRun: runTemplateList},
		{path: []string{"layout", "apply"}, wantCmd: layoutApplyCmd, wantRun: runTemplateApply},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.path, " "), func(t *testing.T) {
			got, _, err := rootCmd.Find(tt.path)
			if err != nil {
				t.Fatalf("Find(%v): %v", tt.path, err)
			}
			if got != tt.wantCmd {
				t.Fatalf("Find(%v) = %q, want template subcommand", tt.path, got.CommandPath())
			}
			if reflect.ValueOf(got.RunE).Pointer() != reflect.ValueOf(tt.wantRun).Pointer() {
				t.Fatalf("%q does not use the expected handler", got.CommandPath())
			}
		})
	}
}

func TestWorkbenchCmdIsVisibleLayoutCmdIsHidden(t *testing.T) {
	t.Parallel()
	if workbenchCmd.Hidden {
		t.Fatal("workbench command should not be hidden")
	}
	if !layoutCmd.Hidden {
		t.Fatal("layout command must be hidden (deprecated alias)")
	}
}

func TestRunTemplateListWith(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Workbenches: []config.Workbench{
			{Name: "dev"},
			{Name: "review"},
		},
	}
	var out bytes.Buffer

	if err := runTemplateListWith(cfg.Workbenches, &out); err != nil {
		t.Fatalf("runTemplateListWith() error: %v", err)
	}
	if got, want := out.String(), "dev\nreview\n"; got != want {
		t.Fatalf("list output = %q, want %q", got, want)
	}
}

// --- fake-state assertion helpers (ADR-0142): consumer tests arrange in-memory
// state and assert on that state; argument arrays live only in module tests.

// sentCommandSet collects the first key (the command) of every SendKeys call
// across all panes.
func sentCommandSet(f *tmuxtest.Fake) map[string]bool {
	sent := map[string]bool{}
	for _, calls := range f.SentKeys {
		for _, keys := range calls {
			if len(keys) > 0 {
				sent[keys[0]] = true
			}
		}
	}
	return sent
}

// sentToPane reports whether any keys were sent to a specific pane.
func sentToPane(f *tmuxtest.Fake, paneID string) bool {
	return len(f.SentKeys[paneID]) > 0
}

// hasSplitOff reports whether a pane was split off target along the given axis.
func hasSplitOff(f *tmuxtest.Fake, target string, horizontal bool) bool {
	for _, s := range f.SplitPanes {
		if s.Target == target && s.Horizontal == horizontal {
			return true
		}
	}
	return false
}

// hasSplitInDir reports whether any split placed a pane rooted at dir.
func hasSplitInDir(f *tmuxtest.Fake, dir string) bool {
	for _, s := range f.SplitPanes {
		if s.Dir == dir {
			return true
		}
	}
	return false
}

// stampedPaneIdentities returns the set of @pop_pane identities stamped.
func stampedPaneIdentities(f *tmuxtest.Fake) map[string]bool {
	set := map[string]bool{}
	for _, id := range f.PaneIdentity {
		set[id] = true
	}
	return set
}

func TestRunTemplateApplyWith(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "dev",
			Windows: []config.WorkbenchWindow{{
				Name:   "work",
				Layout: &config.WorkbenchPaneSpec{Name: "server", Command: "go test ./..."},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session"}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo/checkout", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// The window is created fresh, rooted at the session dir, and stamped with
	// pop-owned identity (never window_name) with auto-rename disabled.
	if got := f.WindowCwd["current-session:work"]; got != "/repo/checkout" {
		t.Errorf("window cwd = %q, want /repo/checkout", got)
	}
	if got := f.WBWindowIdentity["current-session:work"]; got != "work" {
		t.Errorf("@pop_wb_window = %q, want work", got)
	}
	if !reflect.DeepEqual(f.AutoRenameOff, []string{"current-session:work"}) {
		t.Errorf("automatic-rename disabled for %v, want [current-session:work]", f.AutoRenameOff)
	}
	// The window's initial pane is titled, stamped, and runs its command.
	if got := f.PaneTitles["%100"]; got != "server" {
		t.Errorf("pane title = %q, want server", got)
	}
	if got := f.PaneIdentity["%100"]; got != "server" {
		t.Errorf("@pop_pane = %q, want server", got)
	}
	if !sentToPane(f, "%100") || !sentCommandSet(f)["go test ./..."] {
		t.Errorf("expected %%100 to run %q", "go test ./...")
	}
	// With no explicit focus, the first window's first pane is activated.
	if !reflect.DeepEqual(f.SelectedWindowTargets, []string{"current-session:work"}) {
		t.Errorf("selected windows = %v, want [current-session:work]", f.SelectedWindowTargets)
	}
	if !reflect.DeepEqual(f.Selected, []string{"%100"}) {
		t.Errorf("selected panes = %v, want [%%100]", f.Selected)
	}
}

func TestRunTemplateApplyWithUnknownName(t *testing.T) {
	t.Parallel()
	err := runTemplateApplyWith(templateRuntimeDeps{Tmux: &tmuxtest.Fake{}}, []config.Workbench{}, "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `session template "missing" not found`) {
		t.Fatalf("error = %q, want clear unknown-template message", err.Error())
	}
}

func TestRunTemplateApplyWithTmuxFailure(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "dev",
			Windows: []config.WorkbenchWindow{{
				Name:   "work",
				Layout: &config.WorkbenchPaneSpec{Name: "server", Command: "go test ./..."},
			}},
		}},
	}
	f := &tmuxtest.Fake{
		CurrentSessionName: "current-session",
		NewWindowFunc: func(session, name, dir string) (string, error) {
			return "", fmt.Errorf("tmux refused")
		},
	}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo/checkout", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	err := runTemplateApplyWith(d, cfg.Workbenches, "dev")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `failed to create template window "work"`) {
		t.Fatalf("error = %q, want window creation context", err.Error())
	}
}

func TestRunTemplateApplyWithFlatWeightedSplits(t *testing.T) {
	t.Parallel()
	// A window with 3 columns weighted 1, 2, 3 over a 120-cell-wide window.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "weighted",
			Windows: []config.WorkbenchWindow{{
				Name: "work",
				Layout: &config.WorkbenchPaneSpec{
					Children: "columns",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "left", Command: "echo left", Weight: 1},
						{Name: "middle", Command: "echo middle", Weight: 2},
						{Name: "right", Command: "echo right", Weight: 3},
					},
				},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session", PaneW: 120, PaneH: 40}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "weighted"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Two splits (3 columns), all horizontal (-h) for a columns container.
	if len(f.SplitPanes) != 2 {
		t.Fatalf("expected 2 splits, got %d", len(f.SplitPanes))
	}
	for _, s := range f.SplitPanes {
		if !s.Horizontal {
			t.Errorf("columns container split should be horizontal (-h), got %+v", s)
		}
		if s.Cells <= 0 {
			t.Errorf("split must carry exact cells via -l, got %+v", s)
		}
	}
	// Apportion(120, [1,2,3]) → [20,39,59]; splits pass remaining tail sizes.
	if f.SplitPanes[0].Cells != 99 || f.SplitPanes[1].Cells != 59 {
		t.Errorf("split cells = [%d, %d], want [99, 59]", f.SplitPanes[0].Cells, f.SplitPanes[1].Cells)
	}
	// Columns resize width (-x), never height (-y), to the same apportioned cells.
	if len(f.ResizedHeight) != 0 {
		t.Errorf("columns container must not resize height, got %v", f.ResizedHeight)
	}
	want := map[string]int{"%100": 20, "%101": 39, "%102": 59}
	if !reflect.DeepEqual(f.ResizedWidth, want) {
		t.Errorf("resized widths = %v, want %v", f.ResizedWidth, want)
	}
}

func TestRunTemplateApplyWithColumnDirection(t *testing.T) {
	t.Parallel()
	// A rows container splits vertically (-v) and resizes height (-y).
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "stacked",
			Windows: []config.WorkbenchWindow{{
				Name: "work",
				Layout: &config.WorkbenchPaneSpec{
					Children: "rows",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "top", Command: "echo top"},
						{Name: "bottom", Command: "echo bottom"},
					},
				},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session", PaneW: 120, PaneH: 40}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "stacked"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	if len(f.SplitPanes) == 0 {
		t.Fatal("expected at least one split")
	}
	for _, s := range f.SplitPanes {
		if s.Horizontal {
			t.Errorf("rows container split should be vertical (-v), got %+v", s)
		}
	}
	if len(f.ResizedHeight) == 0 {
		t.Error("rows container must resize height (-y)")
	}
	if len(f.ResizedWidth) != 0 {
		t.Errorf("rows container must not resize width, got %v", f.ResizedWidth)
	}
}

func TestRunTemplateApplyWithNestedContainers(t *testing.T) {
	t.Parallel()
	// Outer columns with 2 children; the first child is a rows container.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "nested",
			Windows: []config.WorkbenchWindow{{
				Name: "work",
				Layout: &config.WorkbenchPaneSpec{
					Children: "columns",
					Panes: []config.WorkbenchPaneSpec{
						{
							Children: "rows",
							Weight:   1,
							Panes: []config.WorkbenchPaneSpec{
								{Name: "top-left", Command: "echo tl", Weight: 1},
								{Name: "bottom-left", Command: "echo bl", Weight: 1},
							},
						},
						{Name: "right", Command: "echo right", Weight: 1},
					},
				},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session", PaneW: 120, PaneH: 40}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "nested"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Outer container splits once (2 children), inner container splits once.
	if len(f.SplitPanes) < 2 {
		t.Errorf("expected at least 2 splits for nested containers, got %d", len(f.SplitPanes))
	}
}

func TestRunTemplateApplyWithSameDirectionNesting(t *testing.T) {
	t.Parallel()
	// Columns inside columns: the inner container must size against its own
	// pane (after the outer correction), not the window.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
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
								{Name: "a", Command: "echo a", Weight: 1},
								{Name: "b", Command: "echo b", Weight: 3},
							},
						},
						{Name: "c", Command: "echo c", Weight: 1},
					},
				},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session", PaneW: 80, PaneH: 24}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "cols-in-cols"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Outer Apportion(80, [2,1]) → [53,26]; inner Apportion(53, [1,3]) → [13,39].
	// Pane ids: %100 = outer/left/a (reused), %101 = c, %102 = b.
	if len(f.SplitPanes) != 2 {
		t.Fatalf("expected 2 splits, got %d", len(f.SplitPanes))
	}
	for _, s := range f.SplitPanes {
		if !s.Horizontal || s.Cells <= 0 {
			t.Errorf("same-direction splits must be horizontal with cells, got %+v", s)
		}
	}
	// Outer split peels left (53) from right tail (26); inner peels a (13) from b (39).
	if f.SplitPanes[0].Cells != 26 {
		t.Errorf("outer split cells = %d, want 26", f.SplitPanes[0].Cells)
	}
	if f.SplitPanes[1].Cells != 39 {
		t.Errorf("inner split cells = %d, want 39 (sized against left pane, not window)", f.SplitPanes[1].Cells)
	}
	if f.ResizedWidth["%100"] != 13 || f.ResizedWidth["%102"] != 39 || f.ResizedWidth["%101"] != 26 {
		t.Errorf("final widths = %v, want %%100:13 %%102:39 %%101:26", f.ResizedWidth)
	}
}

func TestRunTemplateApplyWithDefaultWeight(t *testing.T) {
	t.Parallel()
	// Omitted weights default to 1 (equal split).
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "equal",
			Windows: []config.WorkbenchWindow{{
				Name: "work",
				Layout: &config.WorkbenchPaneSpec{
					Children: "columns",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "left", Command: "echo left"},
						{Name: "right", Command: "echo right"},
					},
				},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session", PaneW: 100, PaneH: 50}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "equal"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	want := map[string]int{"%100": 50, "%101": 49}
	if !reflect.DeepEqual(f.ResizedWidth, want) {
		t.Errorf("resized widths = %v, want equal-split apportionment %v", f.ResizedWidth, want)
	}
	if len(f.SplitPanes) != 1 || f.SplitPanes[0].Cells != 49 {
		t.Errorf("equal split must pass -l 49 for the new pane, got %+v", f.SplitPanes)
	}
}

func TestRunTemplateApplyWithDeepNesting(t *testing.T) {
	t.Parallel()
	// 3 levels deep nesting: every leaf's command must be sent.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "deep",
			Windows: []config.WorkbenchWindow{{
				Name: "work",
				Layout: &config.WorkbenchPaneSpec{
					Children: "columns",
					Panes: []config.WorkbenchPaneSpec{
						{
							Children: "rows",
							Panes: []config.WorkbenchPaneSpec{
								{
									Children: "columns",
									Panes: []config.WorkbenchPaneSpec{
										{Name: "deep-left", Command: "echo dl"},
										{Name: "deep-right", Command: "echo dr"},
									},
								},
								{Name: "bottom", Command: "echo bottom"},
							},
						},
						{Name: "right", Command: "echo right"},
					},
				},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session", PaneW: 120, PaneH: 40}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "deep"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	sent := sentCommandSet(f)
	for _, cmd := range []string{"echo dl", "echo dr", "echo bottom", "echo right"} {
		if !sent[cmd] {
			t.Errorf("expected leaf command %q to run", cmd)
		}
	}
}

func TestRunTemplateApplyWithMultipleWindows(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "dev",
			Windows: []config.WorkbenchWindow{
				{Name: "work", Layout: &config.WorkbenchPaneSpec{Name: "server", Command: "go test ./..."}},
				{Name: "logs", Layout: &config.WorkbenchPaneSpec{Name: "tail", Command: "tail -f app.log"}},
			},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session"}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo/checkout", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Both windows are stamped and their panes run their commands (work=%100,
	// logs=%101 in fresh-mint order).
	if f.WBWindowIdentity["current-session:work"] != "work" || f.WBWindowIdentity["current-session:logs"] != "logs" {
		t.Errorf("window identities = %v, want work + logs stamped", f.WBWindowIdentity)
	}
	sent := sentCommandSet(f)
	if !sent["go test ./..."] || !sent["tail -f app.log"] {
		t.Errorf("both windows must run their commands, sent = %v", sent)
	}
	// Focus defaults to the first window's first pane.
	if !reflect.DeepEqual(f.SelectedWindowTargets, []string{"current-session:work"}) {
		t.Errorf("selected windows = %v, want [current-session:work]", f.SelectedWindowTargets)
	}
	if !reflect.DeepEqual(f.Selected, []string{"%100"}) {
		t.Errorf("selected panes = %v, want [%%100]", f.Selected)
	}
}

func TestRunTemplateApplyWithNoLiveWindowsCreatesFresh(t *testing.T) {
	t.Parallel()
	// A window carrying no @pop_wb_window stamp is not reported by
	// LiveWorkbenchWindows (identity never lives in window_name, ADR-0075), so
	// the target window is created fresh. Here no workbench windows are live.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "dev",
			Windows: []config.WorkbenchWindow{
				{Name: "work", Layout: &config.WorkbenchPaneSpec{Name: "server", Command: "go test ./..."}},
			},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session"}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo/checkout", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Built fresh (no merge): window stamped, no panes inspected for identity.
	if f.WBWindowIdentity["current-session:work"] != "work" {
		t.Errorf("window should be created fresh and stamped, got %v", f.WBWindowIdentity)
	}
	if f.PaneIdentity["%100"] != "server" {
		t.Errorf("fresh pane should be stamped server, got %v", f.PaneIdentity)
	}
}

func TestEffectiveCwdAndResolveCwd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		sessionDir string
		parentCwd  string
		rawCwd     string
		homeDir    string
		want       string
	}{
		{
			name:       "inherit parent cwd when empty",
			sessionDir: "/repo",
			parentCwd:  "/repo/backend",
			rawCwd:     "",
			homeDir:    "/home/user",
			want:       "/repo/backend",
		},
		{
			name:       "relative path resolves under session dir",
			sessionDir: "/repo",
			parentCwd:  "/repo/backend",
			rawCwd:     "api",
			homeDir:    "/home/user",
			want:       "/repo/api",
		},
		{
			name:       "absolute path preserved",
			sessionDir: "/repo",
			parentCwd:  "/repo/backend",
			rawCwd:     "/tmp",
			homeDir:    "/home/user",
			want:       "/tmp",
		},
		{
			name:       "tilde expands to home",
			sessionDir: "/repo",
			parentCwd:  "/repo/backend",
			rawCwd:     "~/docs",
			homeDir:    "/home/user",
			want:       "/home/user/docs",
		},
		{
			name:       "tilde only without slash is literal",
			sessionDir: "/repo",
			parentCwd:  "/repo/backend",
			rawCwd:     "~docs",
			homeDir:    "/home/user",
			want:       "/repo/~docs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveCwd(tt.sessionDir, tt.parentCwd, tt.rawCwd, tt.homeDir)
			if got != tt.want {
				t.Fatalf("effectiveCwd() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunTemplateApplyWithCwdInheritanceAndOverride(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "cwd-test",
			Windows: []config.WorkbenchWindow{{
				Name: "work",
				Layout: &config.WorkbenchPaneSpec{
					Children: "rows",
					Cwd:      "backend",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "inherited", Command: "echo inherited"},
						{Name: "override", Command: "echo override", Cwd: "api"},
					},
				},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session"}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "cwd-test"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// The window is created in the container's cwd.
	if got := f.WindowCwd["current-session:work"]; got != "/repo/backend" {
		t.Errorf("window cwd = %q, want /repo/backend", got)
	}
	// The override pane is split into its own cwd.
	if !hasSplitInDir(f, "/repo/api") {
		t.Errorf("expected a split rooted at /repo/api, splits = %+v", f.SplitPanes)
	}
	// No respawn: the first child inherits the container cwd.
	if len(f.Respawned) != 0 {
		t.Errorf("unexpected respawn: %v", f.Respawned)
	}
}

func TestRunTemplateApplyWithCwdTildeAndAbsolute(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "cwd-test",
			Windows: []config.WorkbenchWindow{{
				Name: "work",
				Layout: &config.WorkbenchPaneSpec{
					Children: "rows",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "home", Command: "echo home", Cwd: "~/docs"},
						{Name: "abs", Command: "echo abs", Cwd: "/tmp"},
					},
				},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session"}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "cwd-test"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// The first child overrides the container cwd, so its container pane is
	// respawned in ~/docs; the second is split into /tmp.
	if got := f.Respawned["%100"]; got != "/home/user/docs" {
		t.Errorf("respawn cwd = %q, want /home/user/docs", got)
	}
	if !hasSplitInDir(f, "/tmp") {
		t.Errorf("expected a split rooted at /tmp, splits = %+v", f.SplitPanes)
	}
}

func TestRunTemplateApplyWithFocusOverride(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "focus-test",
			Windows: []config.WorkbenchWindow{{
				Name: "work",
				Layout: &config.WorkbenchPaneSpec{
					Children: "columns",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "left", Command: "echo left"},
						{Name: "right", Command: "echo right", Focus: true},
					},
				},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session", PaneW: 100, PaneH: 50}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "focus-test"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// left=%100 (window pane), right=%101 (split) is focused.
	if !reflect.DeepEqual(f.SelectedWindowTargets, []string{"current-session:work"}) {
		t.Errorf("selected windows = %v, want [current-session:work]", f.SelectedWindowTargets)
	}
	if !reflect.DeepEqual(f.Selected, []string{"%101"}) {
		t.Errorf("selected panes = %v, want the focused pane [%%101]", f.Selected)
	}
}

func TestRunTemplateApplyWithMultipleFocusWarning(t *testing.T) {
	t.Parallel()
	var warnings bytes.Buffer
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "focus-test",
			Windows: []config.WorkbenchWindow{{
				Name: "work",
				Layout: &config.WorkbenchPaneSpec{
					Children: "columns",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "first", Command: "echo first", Focus: true},
						{Name: "second", Command: "echo second", Focus: true},
					},
				},
			}},
		}},
	}
	f := &tmuxtest.Fake{CurrentSessionName: "current-session", PaneW: 100, PaneH: 50}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      &warnings,
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "focus-test"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	if !strings.Contains(warnings.String(), "multiple panes requested focus") {
		t.Fatalf("expected multiple-focus warning, got %q", warnings.String())
	}
	// First focus wins: the initial pane (%100) is focused, not the split (%101).
	if !reflect.DeepEqual(f.Selected, []string{"%100"}) {
		t.Errorf("selected panes = %v, want first focus [%%100]", f.Selected)
	}
}

func TestRealizePaneTreeStampsNamedLeafSkipsUnnamed(t *testing.T) {
	t.Parallel()
	// Named leaf: identity is stamped.
	f := &tmuxtest.Fake{}
	named := &config.WorkbenchPaneSpec{Name: "server", Command: "go test ./..."}
	if _, err := realizePaneTree(f, named, "%7", "/repo", "/repo", "/home/user"); err != nil {
		t.Fatalf("realizePaneTree(named) error: %v", err)
	}
	if f.PaneTitles["%7"] != "server" {
		t.Errorf("title = %q, want server", f.PaneTitles["%7"])
	}
	if f.PaneIdentity["%7"] != "server" {
		t.Errorf("@pop_pane = %q, want server", f.PaneIdentity["%7"])
	}

	// Unnamed leaf: no @pop_pane stamp.
	f = &tmuxtest.Fake{}
	unnamed := &config.WorkbenchPaneSpec{Command: "htop"}
	if _, err := realizePaneTree(f, unnamed, "%8", "/repo", "/repo", "/home/user"); err != nil {
		t.Fatalf("realizePaneTree(unnamed) error: %v", err)
	}
	if _, ok := f.PaneIdentity["%8"]; ok {
		t.Fatalf("unnamed leaf must not be stamped with @pop_pane, got %q", f.PaneIdentity["%8"])
	}
}

// mergeFake returns a Fake arranged with a live pop-owned window `dev` (id @1)
// carrying the given @pop_pane→pane_id panes, plus fixed window dimensions.
func mergeFake(livePanes map[string]string, width, height int) *tmuxtest.Fake {
	fallback := ""
	for _, id := range livePanes {
		if fallback == "" || id < fallback {
			fallback = id
		}
	}
	return &tmuxtest.Fake{
		CurrentSessionName: "current-session",
		PaneW:            width,
		PaneH:            height,
		LiveWBWindows:      map[string]map[string]string{"current-session": {"dev": "@1"}},
		LiveWBPanes:        map[string]map[string]string{"@1": livePanes},
		LiveWBFallback:     map[string]string{"@1": fallback},
	}
}

func TestRunTemplateApplyMergeSupersetAppend(t *testing.T) {
	t.Parallel()
	// Reference transition (ADR-0075): a live window shaped by `minimal`
	// (rows: vim, claude) reapplied with `gs-dev` (those two rows plus a third
	// row of three columns) keeps the live vim/claude panes and appends only
	// the third row.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "gs-dev",
			Windows: []config.WorkbenchWindow{{
				Name: "dev",
				Layout: &config.WorkbenchPaneSpec{
					Children: "rows",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "vim", Command: "vim"},
						{Name: "claude", Command: "claude"},
						{
							Children: "columns",
							Panes: []config.WorkbenchPaneSpec{
								{Name: "build", Command: "echo build"},
								{Name: "services", Command: "echo services"},
								{Name: "vite", Command: "echo vite"},
							},
						},
					},
				},
			}},
		}},
	}
	f := mergeFake(map[string]string{"vim": "%1", "claude": "%2"}, 200, 60)
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "gs-dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// Live vim/claude survive: commands never re-sent, never respawned.
	if sentToPane(f, "%1") {
		t.Error("vim pane %1 should be left untouched")
	}
	if sentToPane(f, "%2") {
		t.Error("claude pane %2 should be left untouched")
	}
	if len(f.Respawned) != 0 {
		t.Errorf("merge must never respawn panes, got %v", f.Respawned)
	}

	// The third row is appended by splitting -v (rows) off the live claude pane.
	if !hasSplitOff(f, "%2", false) {
		t.Errorf("expected the third row appended off %%2 vertically, splits = %+v", f.SplitPanes)
	}

	// Only the third row's panes are built and stamped.
	sent := sentCommandSet(f)
	for _, cmd := range []string{"echo build", "echo services", "echo vite"} {
		if !sent[cmd] {
			t.Errorf("expected the appended row to run %q", cmd)
		}
	}
	if sent["vim"] || sent["claude"] {
		t.Error("survivor commands must not be re-run on reapply")
	}
	stamped := stampedPaneIdentities(f)
	for _, name := range []string{"build", "services", "vite"} {
		if !stamped[name] {
			t.Errorf("appended pane %q should be stamped with @pop_pane", name)
		}
	}

	// Survivors reproportioned: Apportion(60, [1,1,1]) → [20, 19, 19].
	if f.ResizedHeight["%1"] != 20 || f.ResizedHeight["%2"] != 19 {
		t.Errorf("survivors should be reproportioned to 20/19 cells, got %v", f.ResizedHeight)
	}
}

func TestRunTemplateApplyMergeMidRowColumnInsertion(t *testing.T) {
	t.Parallel()
	// A missing column inside an otherwise-live row is spliced in beside its
	// live left sibling, in the correct position.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "row",
			Windows: []config.WorkbenchWindow{{
				Name: "dev",
				Layout: &config.WorkbenchPaneSpec{
					Children: "columns",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "left", Command: "echo left"},
						{Name: "middle", Command: "echo middle"},
						{Name: "right", Command: "echo right"},
					},
				},
			}},
		}},
	}
	// middle is missing; left=%1, right=%2 are live.
	f := mergeFake(map[string]string{"left": "%1", "right": "%2"}, 90, 30)
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "row"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// middle is inserted by splitting -h off the live left pane (%1), so it
	// lands between left and right rather than after right.
	if !hasSplitOff(f, "%1", true) {
		t.Errorf("expected middle spliced off %%1 horizontally, splits = %+v", f.SplitPanes)
	}
	sent := sentCommandSet(f)
	if !sent["echo middle"] {
		t.Error("the missing middle column should be created")
	}
	if sent["echo left"] || sent["echo right"] {
		t.Error("live left/right columns must not be re-run")
	}
	if sentToPane(f, "%1") || sentToPane(f, "%2") {
		t.Error("live columns must be left untouched")
	}
}

func TestRunTemplateApplyMergeReproportionsSurvivors(t *testing.T) {
	t.Parallel()
	// Reapplying with new weights reproportions survivors without re-running or
	// killing them.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "grow",
			Windows: []config.WorkbenchWindow{{
				Name: "dev",
				Layout: &config.WorkbenchPaneSpec{
					Children: "rows",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "vim", Command: "vim", Weight: 3},
						{Name: "claude", Command: "claude", Weight: 1},
					},
				},
			}},
		}},
	}
	f := mergeFake(map[string]string{"vim": "%1", "claude": "%2"}, 100, 80)
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "grow"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// No panes added (both survive) and none re-run.
	if len(f.SplitPanes) != 0 {
		t.Fatalf("no split expected when all panes survive, got %+v", f.SplitPanes)
	}
	if len(f.SentKeys) != 0 {
		t.Fatalf("survivors must not be re-run, got %v", f.SentKeys)
	}
	// vim weight 3 / claude weight 1 over an 80-cell extent → Apportion → 59 / 20.
	if f.ResizedHeight["%1"] != 59 || f.ResizedHeight["%2"] != 20 {
		t.Errorf("reproportion = %v, want %%1:59 %%2:20", f.ResizedHeight)
	}
}

func TestRunTemplateApplyMergeRecreatesUnnamedLeaf(t *testing.T) {
	t.Parallel()
	// Unnamed leaves are anonymous (ADR-0075 B1): with no identity they cannot
	// be matched, so a reapply always (re)creates them — even when a named
	// sibling survives.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name: "mixed",
			Windows: []config.WorkbenchWindow{{
				Name: "dev",
				Layout: &config.WorkbenchPaneSpec{
					Children: "rows",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "vim", Command: "vim"},
						{Command: "htop"}, // unnamed leaf
					},
				},
			}},
		}},
	}
	f := mergeFake(map[string]string{"vim": "%1"}, 100, 40)
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "mixed"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	// The unnamed leaf is appended (split -v off the live vim pane) and run;
	// vim itself is untouched.
	if !hasSplitOff(f, "%1", false) {
		t.Errorf("expected the unnamed leaf appended off %%1 vertically, splits = %+v", f.SplitPanes)
	}
	if !sentCommandSet(f)["htop"] {
		t.Error("unnamed leaf should be recreated on reapply")
	}
	if sentToPane(f, "%1") {
		t.Error("live vim pane must be left untouched")
	}
	// The recreated unnamed leaf (%100) is never stamped with @pop_pane.
	if _, ok := f.PaneIdentity["%100"]; ok {
		t.Fatalf("unnamed leaf must not be stamped, got %q", f.PaneIdentity["%100"])
	}
}

func TestRunTemplateApplyBeforeApplyRunsBeforeWindowRealization(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name:        "dev",
			BeforeApply: []string{"git pull", "make decrypt"},
			Windows: []config.WorkbenchWindow{{
				Name:   "work",
				Layout: &config.WorkbenchPaneSpec{Name: "server", Command: "go test ./..."},
			}},
		}},
	}

	// A combined log records before_apply commands and the first window
	// realization, so ordering can be asserted.
	var combined []string
	var beforeApplyDirs []string
	f := &tmuxtest.Fake{CurrentSessionName: "current-session"}
	f.NewWindowFunc = func(session, name, dir string) (string, error) {
		combined = append(combined, "tmux:new-window")
		return "%0", nil
	}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo/checkout", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
		RunBeforeApply: func(command, dir string) error {
			combined = append(combined, "before_apply:"+command)
			beforeApplyDirs = append(beforeApplyDirs, dir)
			return nil
		},
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	want := []string{"before_apply:git pull", "before_apply:make decrypt", "tmux:new-window"}
	if !reflect.DeepEqual(combined, want) {
		t.Fatalf("order = %v, want %v (commands must run, in order, before any window is realized)", combined, want)
	}
	for _, dir := range beforeApplyDirs {
		if dir != "/repo/checkout" {
			t.Fatalf("before_apply cwd = %q, want the session directory %q", dir, "/repo/checkout")
		}
	}
}

func TestRunTemplateApplyBeforeApplyRunsOnReapplyOverLiveSession(t *testing.T) {
	t.Parallel()
	// Reapply over a live, pop-owned window (merge path) must still run
	// before_apply on every apply.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name:        "dev",
			BeforeApply: []string{"git pull"},
			Windows: []config.WorkbenchWindow{{
				Name: "dev",
				Layout: &config.WorkbenchPaneSpec{
					Children: "rows",
					Panes: []config.WorkbenchPaneSpec{
						{Name: "vim", Command: "vim"},
						{Name: "claude", Command: "claude"},
					},
				},
			}},
		}},
	}
	// Both target panes are already live: a pure reapply with no new windows.
	f := mergeFake(map[string]string{"vim": "%1", "claude": "%2"}, 100, 40)
	var ran []string
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
		RunBeforeApply: func(command, dir string) error {
			ran = append(ran, command)
			if dir != "/repo" {
				t.Fatalf("before_apply cwd = %q, want session directory /repo", dir)
			}
			return nil
		},
	}

	if err := runTemplateApplyWith(d, cfg.Workbenches, "dev"); err != nil {
		t.Fatalf("runTemplateApplyWith() error: %v", err)
	}

	if !reflect.DeepEqual(ran, []string{"git pull"}) {
		t.Fatalf("before_apply ran = %v, want [git pull] on reapply", ran)
	}
}

func TestRunTemplateApplyBeforeApplyError(t *testing.T) {
	t.Parallel()
	// A failing before_apply command aborts the apply before any window is built.
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name:        "dev",
			BeforeApply: []string{"false"},
			Windows: []config.WorkbenchWindow{{
				Name:   "work",
				Layout: &config.WorkbenchPaneSpec{Name: "server", Command: "go test ./..."},
			}},
		}},
	}
	windowCreated := false
	f := &tmuxtest.Fake{CurrentSessionName: "current-session"}
	f.NewWindowFunc = func(session, name, dir string) (string, error) {
		windowCreated = true
		return "%0", nil
	}
	d := templateRuntimeDeps{
		Tmux:        f,
		Getwd:       func() (string, error) { return "/repo", nil },
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
		RunBeforeApply: func(command, dir string) error {
			return fmt.Errorf("boom")
		},
	}

	err := runTemplateApplyWith(d, cfg.Workbenches, "dev")
	if err == nil {
		t.Fatal("expected error from failing before_apply")
	}
	if !strings.Contains(err.Error(), "before_apply[0]") {
		t.Fatalf("error = %q, want before_apply context", err.Error())
	}
	if windowCreated {
		t.Fatal("no window should be realized after a before_apply failure")
	}
}

func TestCreateSessionFromWorkbenchRemovesStrayWindow(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Workbenches: []config.Workbench{{
			Name:        "dev",
			BeforeApply: []string{"git pull"},
			Windows: []config.WorkbenchWindow{{
				Name:   "work",
				Layout: &config.WorkbenchPaneSpec{Name: "server", Command: "go test ./..."},
			}},
		}},
	}

	var beforeApplyRan bool
	var scaffoldDir string
	f := &tmuxtest.Fake{}
	f.NewScaffoldSessionFunc = func(name, dir string) (string, error) {
		scaffoldDir = dir
		return "@0", nil // the stray initial window id
	}
	d := templateRuntimeDeps{
		Tmux:        f,
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
		RunBeforeApply: func(command, dir string) error {
			beforeApplyRan = true
			if dir != "/repo/checkout" {
				t.Fatalf("before_apply cwd = %q, want session directory %q", dir, "/repo/checkout")
			}
			return nil
		},
	}

	tmpl, _ := findWorkbench(cfg.Workbenches, "dev")
	if err := createSessionFromWorkbench(d, tmpl, "mysess", "/repo/checkout"); err != nil {
		t.Fatalf("createSessionFromWorkbench() error: %v", err)
	}

	// The session is created detached first at the session dir.
	if scaffoldDir != "/repo/checkout" {
		t.Errorf("scaffold session dir = %q, want /repo/checkout", scaffoldDir)
	}
	if !beforeApplyRan {
		t.Error("before_apply must run on the create path (run-every-apply)")
	}
	// The Workbench window is realized fresh (no live match) and stamped.
	if f.WBWindowIdentity["mysess:work"] != "work" {
		t.Errorf("window should be created fresh and stamped, got %v", f.WBWindowIdentity)
	}
	// The stray shell window is removed, so the session is exactly the Workbench.
	if !reflect.DeepEqual(f.KilledWindows, []string{"@0"}) {
		t.Fatalf("killed windows = %v, want the stray [@0]", f.KilledWindows)
	}
}

func TestCreateSessionFromWorkbenchInvalidTemplate(t *testing.T) {
	t.Parallel()
	// A Workbench with no windows is invalid; nothing should be created.
	f := &tmuxtest.Fake{}
	d := templateRuntimeDeps{
		Tmux:        f,
		UserHomeDir: func() (string, error) { return "/home/user", nil },
		ErrOut:      io.Discard,
	}
	err := createSessionFromWorkbench(d, config.Workbench{Name: "empty"}, "mysess", "/repo")
	if err == nil {
		t.Fatal("expected error for a Workbench with no windows")
	}
	if len(f.ScaffoldSessions) != 0 || len(f.WBWindowIdentity) != 0 || len(f.KilledWindows) != 0 {
		t.Fatalf("no tmux side effects expected for an invalid Workbench")
	}
}
