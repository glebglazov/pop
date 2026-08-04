package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
)

// The keyboard half of nested mode. The row set a gesture produces is asserted
// through projectRowTree — the same seam the picker is handed — and the wiring
// (expansion outliving a reopen, Enter on a folded project row) through RunProject
// with a scripted picker that presses real keys.

// Typing does not filter within groups: it hands the picker the whole universe at
// depth zero under the full "<project>/<worktree>" names, so a cold worktree that
// nesting folds away is reachable, and a query can match on the prefix.
func TestProjectRowTreeQueryFlattensTheUniverse(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		coldProject("hawk", "/src/hawk", "hawk"),
		coldWorktree("hawk/cold", "/wt/hawk/cold", "hawk"),
		liveWorktree("hawk/hot", "/wt/hawk/hot", "hawk"),
		rowFixture{name: "pop-map-2026-08-03-demo", path: "tmux:pop-map-2026-08-03-demo", icon: iconStandaloneSession, marker: iconMapSession},
	)
	tree := projectRowTree(items, meta, map[string]bool{"/src/hawk": true})

	rows := tree.Rows("hawk")
	assertRowNames(t, rows, "hawk", "hawk/cold", "hawk/hot", "pop-map-2026-08-03-demo")
	for _, r := range rows {
		if r.Depth != 0 || r.Disclosure != "" {
			t.Errorf("row %q is still nested: depth=%d disclosure=%q", r.Name, r.Depth, r.Disclosure)
		}
	}
	// Same glyph vocabulary as the browse view: typing unfolds the list, it does
	// not switch to a different one.
	if got := rowByPath(t, rows, "tmux:pop-map-2026-08-03-demo"); got.Icon != iconNestedMapSession || got.Marker != "" {
		t.Errorf("Map session row = icon %q marker %q, want the fused %q", got.Icon, got.Marker, iconNestedMapSession)
	}
	// Identity is untouched by the flattening, as it is by the nesting.
	if got := rowByPath(t, rows, "/wt/hawk/hot"); got.SessionName != "hawk/hot" {
		t.Errorf("SessionName = %q, want the derived session name", got.SessionName)
	}

	// An empty query is the tree as expanded, not this list.
	assertRowNames(t, tree.Rows(""), "hawk "+iconRowExpanded, "  hot", "pop-map-2026-08-03-demo")
}

// SetExpanded is the only writer of the expansion memory, and the next browse
// listing reflects it.
func TestProjectRowTreeExpansionRoundTrip(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		coldProject("hawk", "/src/hawk", "hawk"),
		liveWorktree("hawk/hot", "/wt/hawk/hot", "hawk"),
	)
	expanded := map[string]bool{}
	tree := projectRowTree(items, meta, expanded)

	assertRowNames(t, tree.Rows(""), "hawk "+iconRowCollapsed)
	tree.SetExpanded("/src/hawk", true)
	assertRowNames(t, tree.Rows(""), "hawk "+iconRowExpanded, "  hot")
	if !reflect.DeepEqual(expanded, map[string]bool{"/src/hawk": true}) {
		t.Errorf("expansion memory = %v, want the one open row", expanded)
	}
	tree.SetExpanded("/src/hawk", false)
	assertRowNames(t, tree.Rows(""), "hawk "+iconRowCollapsed)
	if len(expanded) != 0 {
		t.Errorf("expansion memory = %v, want nothing left open", expanded)
	}
}

// nestedProjectFixture is one project with one live worktree — the smallest
// arrangement with something to open.
func nestedProjectFixture(t *testing.T, display string) (*ProjectDeps, string, string) {
	t.Helper()
	root := t.TempDir()
	projectDir := filepath.Join(root, "hawk")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	worktreeDir := filepath.Join(root, "worktrees", "hawk-0123456789ab", "fix-auth")

	d := testProjectDeps(t)
	d.LoadConfig = func() (*config.Config, error) {
		return &config.Config{
			Projects: []config.ProjectEntry{{Path: projectDir}},
			Project:  &config.ProjectConfig{WorktreeDisplay: display},
		}, nil
	}
	d.ManagedWorktrees = func() []project.ExpandedProject {
		return []project.ExpandedProject{{
			Name:        "hawk/fix-auth",
			Path:        worktreeDir,
			ProjectName: "hawk",
			IsWorktree:  true,
			SessionName: "hawk/fix-auth",
		}}
	}
	d.SessionActivity = func() map[string]int64 {
		return map[string]int64{"hawk/fix-auth": time.Now().Unix()}
	}
	return d, projectDir, worktreeDir
}

// pressKeysOnPicker builds the picker the loop would have run and sends it keys,
// so the options RunProject passed — the tree among them — do the real work.
func pressKeysOnPicker(items []ui.Item, opts []ui.PickerOption, keys ...tea.KeyPressMsg) {
	p := ui.NewPicker(items, opts...)
	p.Init()
	for _, k := range keys {
		p.Update(k)
	}
}

// Expansion lives in the process, so the reopens the picker loop does for its own
// actions do not collapse the tree the operator opened. And it lives nowhere else:
// a second invocation starts collapsed.
func TestRunProjectExpansionSurvivesReopensAndNotInvocations(t *testing.T) {
	t.Parallel()

	openTheGroup := func(t *testing.T, d *ProjectDeps) []string {
		t.Helper()
		var listings [][]string
		calls := 0
		d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
			calls++
			listings = append(listings, rowNames(items))
			if calls == 1 {
				// The cursor opens at the end of the list, which here is the only
				// row: the project. Right opens it.
				pressKeysOnPicker(items, opts, tea.KeyPressMsg{Code: tea.KeyRight})
				// Any action that closes the picker and reopens it with fresh rows.
				return ui.Result{Action: ui.ActionRefresh, Selected: &items[0]}, nil
			}
			return ui.Result{Action: ui.ActionCancel}, nil
		}
		if err := RunProject(d); err != nil {
			t.Fatalf("RunProject: %v", err)
		}
		if calls != 2 {
			t.Fatalf("picker opened %d times, want 2", calls)
		}
		if want := []string{"hawk " + iconRowCollapsed}; !reflect.DeepEqual(listings[0], want) {
			t.Fatalf("first listing = %q, want %q", listings[0], want)
		}
		return listings[1]
	}

	d, _, _ := nestedProjectFixture(t, "nested")
	second := openTheGroup(t, d)
	if want := []string{"hawk " + iconRowExpanded, "  fix-auth"}; !reflect.DeepEqual(second, want) {
		t.Errorf("listing after the reopen = %q, want the group still open %q", second, want)
	}

	// A fresh dashboard over the same deps: nothing was written anywhere for it to
	// read back.
	calls := 0
	var fresh []string
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		calls++
		fresh = rowNames(items)
		return ui.Result{Action: ui.ActionCancel}, nil
	}
	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if want := []string{"hawk " + iconRowCollapsed}; !reflect.DeepEqual(fresh, want) {
		t.Errorf("fresh invocation = %q, want a collapsed tree %q", fresh, want)
	}
}

// Enter on a folded project row is the common action and must not require opening
// the group first: it creates-or-attaches the project's own session from the
// project's own path, exactly as the flat list does.
func TestRunProjectEnterOnCollapsedRowOpensTrunkSession(t *testing.T) {
	t.Parallel()
	d, projectDir, _ := nestedProjectFixture(t, "nested")

	var opened *ui.Item
	d.OpenSession = func(item *ui.Item) error {
		opened = item
		return nil
	}
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		if len(items) != 1 || items[0].Disclosure != iconRowCollapsed {
			t.Fatalf("rows = %q, want one collapsed project row", rowNames(items))
		}
		return ui.Result{Action: ui.ActionConfirm, Selected: &items[0]}, nil
	}
	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if opened == nil {
		t.Fatal("no session opened")
	}
	// The picker's paths come back symlink-resolved, so compare on the resolved form.
	wantPath, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if opened.Path != wantPath || opened.SessionName != "hawk" {
		t.Errorf("opened %q session %q, want the trunk checkout %q session %q", opened.Path, opened.SessionName, wantPath, "hawk")
	}
}

// Flat mode is not a tree, so it is handed no tree: the arrows stay the query
// field's cursor keys everywhere they were before.
func TestRunProjectFlatModePassesNoTree(t *testing.T) {
	t.Parallel()
	d, _, _ := nestedProjectFixture(t, "")

	var moved bool
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		before := rowNames(items)
		p := ui.NewPicker(items, opts...)
		p.Init()
		p.Update(tea.KeyPressMsg{Code: tea.KeyRight})
		p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if sel := p.Result().Selected; sel != nil && sel.Name != before[len(before)-1] {
			moved = true
		}
		return ui.Result{Action: ui.ActionCancel}, nil
	}
	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if moved {
		t.Error("right moved the cursor in flat mode; the arrows belong to the query field there")
	}
}
