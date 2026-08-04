package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
)

// The nested row model is exercised from fixtures, never from real discovery: the
// prototype's first attempt patched the live picker, and real recency, real
// history and real sessions drowned the shape under test. Here the incoming order
// *is* the comparator (the rows arrive from sortByUnifiedRecency, oldest first),
// so a fixture list reads as a timeline top to bottom.

// rowFixture is one row as RunProject hands it to the row builder: the ui.Item
// plus the metadata the Item does not carry.
type rowFixture struct {
	name     string
	path     string
	session  string
	icon     string
	marker   string
	worktree bool
	// repo is the grouping key. Empty means the row has no metadata at all —
	// a standalone tmux session, which belongs to no project.
	repo  string
	label string
}

func fixtureRows(fx ...rowFixture) ([]ui.Item, map[string]projectRowMeta) {
	items := make([]ui.Item, 0, len(fx))
	meta := make(map[string]projectRowMeta, len(fx))
	for _, f := range fx {
		items = append(items, ui.Item{
			Name:        f.name,
			Path:        f.path,
			Context:     f.repo,
			SessionName: f.session,
			Icon:        f.icon,
			Marker:      f.marker,
		})
		if f.repo != "" {
			meta[f.path] = projectRowMeta{IsWorktree: f.worktree, Repo: f.repo, RepoLabel: f.label}
		}
	}
	return items, meta
}

// liveProject and friends keep the fixtures readable: a row is live when it
// carries an icon, which is exactly what buildSessionAwareItemsWith stamps.
func liveProject(name, path, repo string) rowFixture {
	return rowFixture{name: name, path: path, session: name, icon: iconDirSession, repo: repo, label: name}
}

func coldProject(name, path, repo string) rowFixture {
	return rowFixture{name: name, path: path, session: name, repo: repo, label: name}
}

func liveWorktree(name, path, repo string) rowFixture {
	return rowFixture{name: name, path: path, session: name, icon: iconDirSession, worktree: true, repo: repo}
}

func coldWorktree(name, path, repo string) rowFixture {
	return rowFixture{name: name, path: path, session: name, worktree: true, repo: repo}
}

func rowNames(rows []ui.Item) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		name := r.Name
		if r.Disclosure != "" {
			name += " " + r.Disclosure
		}
		out = append(out, strings.Repeat("  ", r.Depth)+name)
	}
	return out
}

func assertRowNames(t *testing.T, rows []ui.Item, want ...string) {
	t.Helper()
	if got := rowNames(rows); !reflect.DeepEqual(got, want) {
		t.Errorf("rows:\n got %q\nwant %q", got, want)
	}
}

func rowByPath(t *testing.T, rows []ui.Item, path string) ui.Item {
	t.Helper()
	for _, r := range rows {
		if r.Path == path {
			return r
		}
	}
	t.Fatalf("no row for path %q; got %q", path, rowNames(rows))
	return ui.Item{}
}

// Flat mode is today's list: every worktree is a row of its own under its full
// "<project>/<worktree>" name, session or not, with both glyph columns intact.
func TestBuildProjectRowsFlatIsUnchanged(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		coldProject("hawk", "/src/hawk", "hawk"),
		coldWorktree("hawk/cold", "/wt/hawk/cold", "hawk"),
		liveWorktree("hawk/hot", "/wt/hawk/hot", "hawk"),
		rowFixture{name: "pop-map-2026-08-03-demo", path: "tmux:pop-map-2026-08-03-demo", icon: iconStandaloneSession, marker: iconMapSession},
	)
	before := append([]ui.Item(nil), items...)

	for _, display := range []config.WorktreeDisplay{config.WorktreeDisplayFlat, config.WorktreeDisplay(""), config.WorktreeDisplay("nested-ish")} {
		rows := buildProjectRows(items, meta, display, nil)
		if !reflect.DeepEqual(rows, before) {
			t.Errorf("display %q: rows changed:\n got %+v\nwant %+v", display, rows, before)
		}
	}
}

// Nested mode is a different row set, not a rearrangement: the live worktree
// becomes a child, the session-less one drops out (it is reachable by typing a
// query, not by attaching from here).
func TestBuildProjectRowsNestedMembershipIsLiveSessionsOnly(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		coldProject("hawk", "/src/hawk", "hawk"),
		coldWorktree("hawk/cold", "/wt/hawk/cold", "hawk"),
		liveWorktree("hawk/hot", "/wt/hawk/hot", "hawk"),
	)

	collapsed := buildProjectRows(items, meta, config.WorktreeDisplayNested, nil)
	assertRowNames(t, collapsed, "hawk ▸")

	expanded := buildProjectRows(items, meta, config.WorktreeDisplayNested, map[string]bool{"/src/hawk": true})
	assertRowNames(t, expanded, "hawk ▾", "  hot")
}

// One glyph column: every live session reads the same, a Map session is the only
// kind distinction, and the Work-kind badges are gone from this list entirely.
func TestBuildProjectRowsFusesGlyphColumn(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		coldProject("cold", "/src/cold", "cold"),
		liveProject("dir", "/src/dir", "dir"),
		rowFixture{name: "set", path: "/src/set", session: "set", icon: iconDirSession, marker: iconTaskSetSession, repo: "set", label: "set"},
		rowFixture{name: "routine", path: "/src/routine", session: "routine", icon: iconDirSession, marker: iconRoutineSession, repo: "routine", label: "routine"},
		rowFixture{name: "map", path: "tmux:pop-map-demo", icon: iconStandaloneSession, marker: iconMapSession},
		rowFixture{name: "scratch", path: "tmux:scratch", icon: iconStandaloneSession},
		rowFixture{name: "unread", path: "/src/unread", session: "unread", icon: iconAttention, marker: iconTaskSetSession, repo: "unread", label: "unread"},
	)

	rows := buildProjectRows(items, meta, config.WorktreeDisplayNested, nil)

	wantIcon := map[string]string{
		"/src/cold":         "",
		"/src/dir":          iconDirSession,
		"/src/set":          iconDirSession,
		"/src/routine":      iconDirSession,
		"tmux:pop-map-demo": iconNestedMapSession,
		"tmux:scratch":      iconDirSession,
		"/src/unread":       iconAttention,
	}
	for path, want := range wantIcon {
		if got := rowByPath(t, rows, path).Icon; got != want {
			t.Errorf("%s: Icon = %q, want %q", path, got, want)
		}
	}
	for _, r := range rows {
		if r.Marker != "" {
			t.Errorf("%s: Marker = %q, want none — nested mode has one column", r.Path, r.Marker)
		}
		if r.Icon == iconTaskSetSession || r.Icon == iconRoutineSession {
			t.Errorf("%s: Icon = %q — Work-kind badges are not rendered in this list", r.Path, r.Icon)
		}
		// No colour carries meaning, here or on an accent border that no longer
		// exists: a glyph is either there or it is not, so nothing a row renders
		// is styled.
		if strings.Contains(r.Icon+r.Name+r.Disclosure, "\x1b") {
			t.Errorf("%s: styled output %q — no colour carries meaning in this list", r.Path, r.Icon+r.Name)
		}
	}
}

// The disclosure triangle is the whole signal a collapsed row carries: no count,
// no glyph summary of what is folded away, and nothing that needs a colour.
func TestBuildProjectRowsDisclosureTriangle(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		liveProject("quiet", "/src/quiet", "quiet"),
		coldWorktree("quiet/cold", "/wt/quiet/cold", "quiet"),
		coldProject("hawk", "/src/hawk", "hawk"),
		liveWorktree("hawk/hot", "/wt/hawk/hot", "hawk"),
	)

	collapsed := buildProjectRows(items, meta, config.WorktreeDisplayNested, nil)
	if got := rowByPath(t, collapsed, "/src/quiet").Disclosure; got != "" {
		t.Errorf("project with no live worktree: Disclosure = %q, want none", got)
	}
	hawk := rowByPath(t, collapsed, "/src/hawk")
	if hawk.Disclosure != iconRowCollapsed {
		t.Errorf("collapsed: Disclosure = %q, want %q", hawk.Disclosure, iconRowCollapsed)
	}
	if hawk.Name != "hawk" {
		t.Errorf("collapsed row Name = %q, want %q — the triangle is all a collapsed row says", hawk.Name, "hawk")
	}

	expanded := buildProjectRows(items, meta, config.WorktreeDisplayNested, map[string]bool{"/src/hawk": true})
	if got := rowByPath(t, expanded, "/src/hawk").Disclosure; got != iconRowExpanded {
		t.Errorf("expanded: Disclosure = %q, want %q", got, iconRowExpanded)
	}
}

// One ordering rule governs both levels: a project sinks to its most recent
// child, and children keep the same oldest-first direction as the top level.
func TestBuildProjectRowsOrdering(t *testing.T) {
	t.Parallel()
	// Oldest first: hawk's trunk is the oldest row in the list, but two of its
	// worktrees were used after api, so hawk sinks past api.
	items, meta := fixtureRows(
		coldProject("hawk", "/src/hawk", "hawk"),
		liveWorktree("hawk/older", "/wt/hawk/older", "hawk"),
		liveProject("api", "/src/api", "api"),
		liveWorktree("hawk/newer", "/wt/hawk/newer", "hawk"),
	)

	rows := buildProjectRows(items, meta, config.WorktreeDisplayNested, map[string]bool{"/src/hawk": true})
	assertRowNames(t, rows, "api", "hawk ▾", "  older", "  newer")

	// With every worktree older than api, the project stays where its own recency
	// puts it: sinking is to the most recent child, not to the newest of them all.
	items, meta = fixtureRows(
		coldProject("hawk", "/src/hawk", "hawk"),
		liveWorktree("hawk/older", "/wt/hawk/older", "hawk"),
		liveProject("api", "/src/api", "api"),
	)
	rows = buildProjectRows(items, meta, config.WorktreeDisplayNested, map[string]bool{"/src/hawk": true})
	assertRowNames(t, rows, "hawk ▾", "  older", "api")
}

// A bare repository's worktrees are its top-level rows today, so nesting has no
// parent to hang them under. Nested mode invents one from the repo's display
// name: a half-nested list is the worse artifact.
func TestBuildProjectRowsBareRepoSynthesizesParent(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		rowFixture{name: "code/kestrel/main", path: "/wt/kestrel/main", session: "kestrel/main", icon: iconDirSession, worktree: true, repo: "kestrel", label: "code/kestrel"},
		rowFixture{name: "code/kestrel/hotfix", path: "/wt/kestrel/hotfix", session: "kestrel/hotfix", icon: iconDirSession, worktree: true, repo: "kestrel", label: "code/kestrel"},
	)

	rows := buildProjectRows(items, meta, config.WorktreeDisplayNested, map[string]bool{projectGroupPathPrefix + "kestrel": true})
	assertRowNames(t, rows, "code/kestrel ▾", "  main", "  hotfix")

	parent := rows[0]
	if !isSynthesizedProjectRow(parent) {
		t.Errorf("parent Path = %q, want the %q prefix so no action opens it", parent.Path, projectGroupPathPrefix)
	}
	if parent.SessionName != "" || parent.Icon != "" {
		t.Errorf("synthesized parent has SessionName %q / Icon %q, want neither — it names no checkout",
			parent.SessionName, parent.Icon)
	}
	if isSynthesizedProjectRow(rows[1]) {
		t.Error("worktree row read as synthesized")
	}

	// All-cold worktrees still get their parent, or the repository would vanish
	// from the list altogether.
	items, meta = fixtureRows(
		coldWorktree("code/kestrel/main", "/wt/kestrel/main", "kestrel"),
	)
	meta["/wt/kestrel/main"] = projectRowMeta{IsWorktree: true, Repo: "kestrel", RepoLabel: "code/kestrel"}
	rows = buildProjectRows(items, meta, config.WorktreeDisplayNested, nil)
	assertRowNames(t, rows, "code/kestrel")
}

// Nesting is display-only: the label loses the prefix, the session name keeps it,
// because that name is derived from the worktree directory and nothing renames it.
func TestBuildProjectRowsNestingIsDisplayOnly(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		liveProject("hawk", "/src/hawk", "hawk"),
		liveWorktree("hawk/fix-auth", "/wt/hawk/fix-auth", "hawk"),
	)

	rows := buildProjectRows(items, meta, config.WorktreeDisplayNested, map[string]bool{"/src/hawk": true})
	child := rowByPath(t, rows, "/wt/hawk/fix-auth")
	if child.Name != "fix-auth" {
		t.Errorf("child label = %q, want %q", child.Name, "fix-auth")
	}
	if child.SessionName != "hawk/fix-auth" {
		t.Errorf("child SessionName = %q, want %q — no session is renamed", child.SessionName, "hawk/fix-auth")
	}
	if child.Depth != 1 {
		t.Errorf("child Depth = %d, want 1 so the whole row indents", child.Depth)
	}
	if rows[0].Depth != 0 {
		t.Errorf("parent Depth = %d, want 0", rows[0].Depth)
	}
	// Nothing else about a row moves: every rendered row still carries the path
	// and the session name it came in with.
	sessionByPath := map[string]string{}
	for _, it := range items {
		sessionByPath[it.Path] = it.SessionName
	}
	for _, r := range rows {
		if isSynthesizedProjectRow(r) {
			continue
		}
		want, ok := sessionByPath[r.Path]
		if !ok {
			t.Errorf("row %q has a path no incoming row had", r.Path)
			continue
		}
		if r.SessionName != want {
			t.Errorf("%s: SessionName = %q, want %q", r.Path, r.SessionName, want)
		}
	}

	// Flat mode reaches the same worktree under its full name at depth 0.
	flat := buildProjectRows(items, meta, config.WorktreeDisplayFlat, nil)
	if got := rowByPath(t, flat, "/wt/hawk/fix-auth"); got.Name != "hawk/fix-auth" || got.Depth != 0 {
		t.Errorf("flat worktree row = %q at depth %d, want %q at depth 0", got.Name, got.Depth, "hawk/fix-auth")
	}
}

// Two repositories with the same basename cannot be told apart without guessing
// which one a worktree belongs to, and hanging it under the wrong project is
// worse than leaving it where flat mode puts it.
func TestNestProjectRowsAmbiguousRepoStaysTopLevel(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		liveProject("work/hawk", "/work/hawk", "hawk"),
		liveProject("play/hawk", "/play/hawk", "hawk"),
		liveWorktree("hawk/fix", "/wt/hawk/fix", "hawk"),
	)

	rows := buildProjectRows(items, meta, config.WorktreeDisplayNested, nil)
	assertRowNames(t, rows, "work/hawk", "play/hawk", "hawk/fix")
}

// A project used less recently than one of its worktrees arrives after it. The
// parent synthesized for the worktree is adopted rather than duplicated.
func TestNestProjectRowsAdoptsLateProjectRow(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		liveWorktree("hawk/fix", "/wt/hawk/fix", "hawk"),
		liveProject("hawk", "/src/hawk", "hawk"),
	)

	rows := buildProjectRows(items, meta, config.WorktreeDisplayNested, map[string]bool{"/src/hawk": true})
	assertRowNames(t, rows, "hawk ▾", "  fix")
	if rows[0].Path != "/src/hawk" {
		t.Errorf("parent Path = %q, want the real checkout %q", rows[0].Path, "/src/hawk")
	}
}

// The wiring, end to end: [project] worktree_display picks the arrangement, and
// the value is read once where the picker is constructed — a picker loop that
// reopens several times never re-reads config, so a change lands on the next
// invocation of pop, not mid-session.
func TestRunProjectWorktreeDisplayWiring(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, display string) ([]string, int) {
		t.Helper()
		root := t.TempDir()
		projectDir := filepath.Join(root, "hawk")
		if err := os.MkdirAll(projectDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		d := testProjectDeps(t)
		configReads := 0
		d.LoadConfig = func() (*config.Config, error) {
			configReads++
			return &config.Config{
				Projects: []config.ProjectEntry{{Path: projectDir}},
				Project:  &config.ProjectConfig{WorktreeDisplay: display},
			}, nil
		}
		d.ManagedWorktrees = func() []project.ExpandedProject {
			return []project.ExpandedProject{{
				Name:        "hawk/fix-auth",
				Path:        filepath.Join(root, "worktrees", "hawk-0123456789ab", "fix-auth"),
				ProjectName: "hawk",
				IsWorktree:  true,
				SessionName: "hawk/fix-auth",
			}}
		}
		d.SessionActivity = func() map[string]int64 {
			return map[string]int64{"hawk/fix-auth": time.Now().Unix()}
		}

		var rendered []string
		pickerCalls := 0
		d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
			pickerCalls++
			rendered = rowNames(items)
			if pickerCalls == 1 {
				// Any action that closes and reopens the picker with fresh rows.
				return ui.Result{Action: ui.ActionRefresh, Selected: &items[0]}, nil
			}
			return ui.Result{Action: ui.ActionCancel}, nil
		}

		if err := RunProject(d); err != nil {
			t.Fatalf("RunProject: %v", err)
		}
		if pickerCalls != 2 {
			t.Fatalf("picker opened %d times, want 2", pickerCalls)
		}
		return rendered, configReads
	}

	t.Run("nested groups the live worktree under its project", func(t *testing.T) {
		t.Parallel()
		rows, configReads := run(t, "nested")
		if !reflect.DeepEqual(rows, []string{"hawk " + iconRowCollapsed}) {
			t.Errorf("rows = %q, want the project row with its triangle only", rows)
		}
		if configReads != 1 {
			t.Errorf("config read %d times across two picker openings, want 1", configReads)
		}
	})

	t.Run("default lists every worktree flat under its full name", func(t *testing.T) {
		t.Parallel()
		rows, _ := run(t, "")
		if !reflect.DeepEqual(rows, []string{"hawk", "hawk/fix-auth"}) {
			t.Errorf("rows = %q, want the flat prefixed list", rows)
		}
	})

	t.Run("an unreadable value falls back to flat", func(t *testing.T) {
		t.Parallel()
		rows, _ := run(t, "tree")
		if !reflect.DeepEqual(rows, []string{"hawk", "hawk/fix-auth"}) {
			t.Errorf("rows = %q, want the flat prefixed list", rows)
		}
	})
}
