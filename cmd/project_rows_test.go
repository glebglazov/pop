package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// Flat mode's row set is today's list: every worktree is a row of its own under
// its full "<project>/<worktree>" name, session or not, in the incoming order,
// with no indentation and no disclosure triangle. Fusing the glyph column is the
// only thing that changed, and it changes a copy.
func TestBuildProjectRowsFlatFusesGlyphsOnly(t *testing.T) {
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
		assertRowNames(t, rows, "hawk", "hawk/cold", "hawk/hot", "pop-map-2026-08-03-demo")
		for i, r := range rows {
			bare := r
			bare.Icon, bare.Marker = before[i].Icon, before[i].Marker
			if !reflect.DeepEqual(bare, before[i]) {
				t.Errorf("display %q row %d changed beyond its glyphs:\n got %+v\nwant %+v", display, i, bare, before[i])
			}
			if r.Marker != "" {
				t.Errorf("display %q: %s carries Marker %q — flat renders one column too", display, r.Path, r.Marker)
			}
		}
		// The caller's slice is reused by the next iteration of the picker loop, so
		// fusing must not have written back into it.
		if !reflect.DeepEqual(items, before) {
			t.Errorf("display %q: the caller's rows were mutated:\n got %+v\nwant %+v", display, items, before)
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

// The whole glyph precedence, both modes in one place: what the modes agree on
// (unread outranks everything, a session-less row is blank, a kind glyph replaces
// the session glyph rather than joining it, a Map is hollow) and where they part on
// purpose — flat is the inventory and keeps every distinction it can draw, nested
// answers "what can I attach to" and flattens all Work kinds but a Map.
func TestFuseGlyphColumnPrecedenceInBothModes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		row        rowFixture
		wantFlat   string
		wantNested string
	}{
		{"no session", coldProject("cold", "/src/cold", "cold"), "", ""},
		{"live checkout", liveProject("dir", "/src/dir", "dir"), iconDirSession, iconDirSession},
		{
			"standalone session",
			rowFixture{name: "scratch", path: "tmux:scratch", icon: iconStandaloneSession},
			iconStandaloneSession, iconDirSession,
		},
		{
			"map session",
			rowFixture{name: "map", path: "tmux:pop-map-demo", icon: iconStandaloneSession, marker: iconMapSession},
			iconHollowMapSession, iconHollowMapSession,
		},
		{
			"task-set session",
			rowFixture{name: "set", path: "/src/set", session: "set", icon: iconDirSession, marker: iconTaskSetSession, repo: "set", label: "set"},
			iconTaskSetSession, iconDirSession,
		},
		{
			"routine session",
			rowFixture{name: "routine", path: "/src/routine", session: "routine", icon: iconDirSession, marker: iconRoutineSession, repo: "routine", label: "routine"},
			iconRoutineSession, iconDirSession,
		},
		{
			"unread outranks the work kind",
			rowFixture{name: "unread", path: "/src/unread", session: "unread", icon: iconAttention, marker: iconTaskSetSession, repo: "unread", label: "unread"},
			iconAttention, iconAttention,
		},
	}

	fx := make([]rowFixture, 0, len(cases))
	for _, tc := range cases {
		fx = append(fx, tc.row)
	}
	items, meta := fixtureRows(fx...)
	flat := buildProjectRows(items, meta, config.WorktreeDisplayFlat, nil)
	nested := buildProjectRows(items, meta, config.WorktreeDisplayNested, nil)

	for _, tc := range cases {
		if got := rowByPath(t, flat, tc.row.path).Icon; got != tc.wantFlat {
			t.Errorf("%s: flat Icon = %q, want %q", tc.name, got, tc.wantFlat)
		}
		if got := rowByPath(t, nested, tc.row.path).Icon; got != tc.wantNested {
			t.Errorf("%s: nested Icon = %q, want %q", tc.name, got, tc.wantNested)
		}
	}

	for _, r := range append(append([]ui.Item(nil), flat...), nested...) {
		if r.Marker != "" {
			t.Errorf("%s: Marker = %q, want none — both modes render one column", r.Path, r.Marker)
		}
		if r.Icon == iconMapSession {
			t.Errorf("%s: Icon = %q — the filled Map diamond belongs to the Work dashboard", r.Path, r.Icon)
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

// A bare repository's Trunk is the checkout the operator reaches for most, and it
// arrives as one worktree row among many. Nested mode makes it the repository's
// top-level row rather than hanging it under a header nothing can open: the row
// reads as the repository, opens the trunk's own session, and its siblings nest
// under it.
func TestBuildProjectRowsPromotesDeclaredTrunkToParent(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		rowFixture{name: "code/kestrel/hotfix", path: "/wt/kestrel/hotfix", session: "kestrel/hotfix", icon: iconDirSession, worktree: true, repo: "kestrel", label: "code/kestrel"},
		rowFixture{name: "code/kestrel/main", path: "/wt/kestrel/main", session: "kestrel/main", icon: iconDirSession, worktree: true, repo: "kestrel", label: "code/kestrel"},
	)
	markTrunkRows(meta, []string{"/wt/kestrel/main"}, nil)

	rows := buildProjectRows(items, meta, config.WorktreeDisplayNested, map[string]bool{"/wt/kestrel/main": true})
	assertRowNames(t, rows, "code/kestrel ▾", "  hotfix")

	parent := rows[0]
	if isSynthesizedProjectRow(parent) {
		t.Errorf("parent Path = %q, want the trunk checkout — a header cannot be opened", parent.Path)
	}
	if parent.Path != "/wt/kestrel/main" || parent.SessionName != "kestrel/main" {
		t.Errorf("parent = {Path %q, SessionName %q}, want the trunk's own", parent.Path, parent.SessionName)
	}
	if parent.Icon != iconDirSession {
		t.Errorf("parent Icon = %q, want the trunk's live-session glyph", parent.Icon)
	}
	for _, r := range rows[1:] {
		if r.Path == "/wt/kestrel/main" {
			t.Error("trunk row rendered as a child as well as the parent")
		}
	}

	// A cold trunk is still the parent — that is the whole point, since a
	// session-less worktree is otherwise unreachable without typing a query.
	items, meta = fixtureRows(
		rowFixture{name: "code/kestrel/main", path: "/wt/kestrel/main", session: "kestrel/main", worktree: true, repo: "kestrel", label: "code/kestrel"},
	)
	markTrunkRows(meta, []string{"/wt/kestrel/main"}, nil)
	rows = buildProjectRows(items, meta, config.WorktreeDisplayNested, nil)
	assertRowNames(t, rows, "code/kestrel")
	if rows[0].Path != "/wt/kestrel/main" {
		t.Errorf("cold trunk parent Path = %q, want the checkout", rows[0].Path)
	}

	// Undeclared, and the header comes back: nothing about the arrangement changed
	// for a bare repo pop has not been told the trunk of.
	items, meta = fixtureRows(
		rowFixture{name: "code/kestrel/main", path: "/wt/kestrel/main", session: "kestrel/main", icon: iconDirSession, worktree: true, repo: "kestrel", label: "code/kestrel"},
	)
	rows = buildProjectRows(items, meta, config.WorktreeDisplayNested, map[string]bool{projectGroupPathPrefix + "kestrel": true})
	assertRowNames(t, rows, "code/kestrel ▾", "  main")

	// Flat mode is untouched: every worktree keeps its own row under its full
	// prefixed name, trunk included.
	items, meta = fixtureRows(
		rowFixture{name: "code/kestrel/main", path: "/wt/kestrel/main", session: "kestrel/main", icon: iconDirSession, worktree: true, repo: "kestrel", label: "code/kestrel"},
		rowFixture{name: "code/kestrel/hotfix", path: "/wt/kestrel/hotfix", session: "kestrel/hotfix", icon: iconDirSession, worktree: true, repo: "kestrel", label: "code/kestrel"},
	)
	markTrunkRows(meta, []string{"/wt/kestrel/main"}, nil)
	rows = buildProjectRows(items, meta, config.WorktreeDisplayFlat, nil)
	assertRowNames(t, rows, "code/kestrel/main", "code/kestrel/hotfix")
}

// A non-bare repository already has a row of its own. A declaration naming one of
// its worktrees as well would leave two candidates for one top-level row, and
// guessing between them is worse than leaving the worktrees where they were.
func TestNestProjectRowsRefusesTwoTrunkCandidates(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		liveProject("hawk", "/src/hawk", "hawk"),
		liveWorktree("hawk/fix", "/wt/hawk/fix", "hawk"),
	)
	markTrunkRows(meta, []string{"/wt/hawk/fix"}, nil)

	rows := buildProjectRows(items, meta, config.WorktreeDisplayNested, nil)
	assertRowNames(t, rows, "hawk", "hawk/fix")
}

// The trunk lookup is a set membership test over paths, so the common case costs
// one map lookup per declaration and touches the filesystem not at all. Symlinks
// are resolved only when a plain comparison has already missed, and rows are
// resolved only when resolving the declaration missed too.
func TestMarkTrunkRowsResolvesSymlinksOnlyOnAMiss(t *testing.T) {
	t.Parallel()

	newMeta := func() map[string]projectRowMeta {
		return map[string]projectRowMeta{
			"/private/src/kestrel/main":   {IsWorktree: true, Repo: "kestrel"},
			"/private/src/kestrel/hotfix": {IsWorktree: true, Repo: "kestrel"},
			"/src/hawk":                   {Repo: "hawk"},
		}
	}

	t.Run("direct hit resolves nothing", func(t *testing.T) {
		t.Parallel()
		meta := newMeta()
		calls := 0
		markTrunkRows(meta, []string{"/private/src/kestrel/main"}, func(p string) (string, error) {
			calls++
			return p, nil
		})
		if !meta["/private/src/kestrel/main"].IsTrunk {
			t.Error("declared trunk not marked")
		}
		if calls != 0 {
			t.Errorf("EvalSymlinks called %d times on a direct hit, want 0", calls)
		}
	})

	t.Run("the declaration is resolved once", func(t *testing.T) {
		t.Parallel()
		meta := newMeta()
		var resolved []string
		markTrunkRows(meta, []string{"/src/kestrel/main"}, func(p string) (string, error) {
			resolved = append(resolved, p)
			return strings.Replace(p, "/src/", "/private/src/", 1), nil
		})
		if !meta["/private/src/kestrel/main"].IsTrunk {
			t.Error("trunk declared through a symlinked path not marked")
		}
		if !reflect.DeepEqual(resolved, []string{"/src/kestrel/main"}) {
			t.Errorf("resolved %q, want the declaration alone — no row needed resolving", resolved)
		}
	})

	t.Run("rows are resolved only for a repository with no trunk", func(t *testing.T) {
		t.Parallel()
		meta := newMeta()
		meta["/private/src/kestrel/hotfix"] = projectRowMeta{IsWorktree: true, Repo: "kestrel", IsTrunk: true}
		var resolved []string
		markTrunkRows(meta, []string{"/elsewhere/kestrel/main"}, func(p string) (string, error) {
			resolved = append(resolved, p)
			return p, nil
		})
		if !reflect.DeepEqual(resolved, []string{"/elsewhere/kestrel/main"}) {
			t.Errorf("resolved %q, want the declaration alone — kestrel already had a trunk", resolved)
		}

		// With no trunk anywhere, the rows of the trunkless repository are the last
		// resort — and only those rows.
		meta = newMeta()
		resolved = nil
		markTrunkRows(meta, []string{"/link/kestrel/main"}, func(p string) (string, error) {
			resolved = append(resolved, p)
			if p == "/link/kestrel/main" {
				return "/real/kestrel/main", nil
			}
			if strings.HasSuffix(p, "/kestrel/main") {
				return "/real/kestrel/main", nil
			}
			return p, nil
		})
		if !meta["/private/src/kestrel/main"].IsTrunk {
			t.Error("row matching the resolved declaration not marked")
		}
		if slices.Contains(resolved, "/src/hawk") {
			t.Errorf("resolved %q, want no row of a repository that has a parent already", resolved)
		}
	})

	t.Run("no declarations is no work", func(t *testing.T) {
		t.Parallel()
		meta := newMeta()
		markTrunkRows(meta, nil, func(string) (string, error) {
			t.Error("EvalSymlinks called with nothing declared")
			return "", nil
		})
		for path, m := range meta {
			if m.IsTrunk {
				t.Errorf("row %q marked trunk with nothing declared", path)
			}
		}
	})
}

// The wiring, end to end: a [repo."<path>"] trunk declaration — the same path
// that names the fork base for managed worktrees — is what promotes a bare
// repository's trunk to its top-level row. Nothing in the picker resolves a trunk
// per checkout, so without this read the declaration went unhonoured here.
func TestRunProjectPromotesDeclaredTrunkOfABareRepo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectDir := filepath.Join(root, "hawk")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	repoRoot := filepath.Join(root, "worktrees", "kestrel-0123456789ab")
	trunk := filepath.Join(repoRoot, "main")
	hotfix := filepath.Join(repoRoot, "hotfix")

	d := testProjectDeps(t)
	d.LoadConfig = func() (*config.Config, error) {
		return &config.Config{
			Projects: []config.ProjectEntry{{Path: projectDir}},
			Project:  &config.ProjectConfig{WorktreeDisplay: string(config.WorktreeDisplayNested)},
			Repo: map[string]config.RepoOverrideConfig{
				trunk: {Trunk: trunkPtr(trunk)},
			},
		}, nil
	}
	d.ManagedWorktrees = func() []project.ExpandedProject {
		return []project.ExpandedProject{
			{Name: "kestrel/main", Path: trunk, ProjectName: "kestrel", IsWorktree: true, SessionName: "kestrel/main"},
			{Name: "kestrel/hotfix", Path: hotfix, ProjectName: "kestrel", IsWorktree: true, SessionName: "kestrel/hotfix"},
		}
	}
	d.SessionActivity = func() map[string]int64 {
		return map[string]int64{"kestrel/hotfix": time.Now().Unix()}
	}

	var rows []ui.Item
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		rows = items
		return ui.Result{Action: ui.ActionCancel}, nil
	}
	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	parent := rowByPath(t, rows, trunk)
	if parent.Depth != 0 {
		t.Errorf("trunk row Depth = %d, want 0 — the trunk is the repository's row", parent.Depth)
	}
	if parent.Name != "kestrel" {
		t.Errorf("trunk row Name = %q, want the repository's name", parent.Name)
	}
	if parent.SessionName != "kestrel/main" {
		t.Errorf("trunk row SessionName = %q, want the trunk's own session", parent.SessionName)
	}
	for _, r := range rows {
		if isSynthesizedProjectRow(r) {
			t.Errorf("row %q is a grouping header, want the trunk in its place", r.Name)
		}
	}
}
