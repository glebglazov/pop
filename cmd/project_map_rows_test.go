package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/history"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
)

// A Map session's row, as buildSessionAwareItemsWith produces one: a standalone
// session (its Path is the session name, not a directory) badged with the Map
// Work kind.
func mapSessionFixture(mapID string) rowFixture {
	session := "pop-map-" + mapID
	return rowFixture{
		name:    session,
		path:    tmuxSessionPathPrefix + session,
		session: session,
		icon:    iconStandaloneSession,
		marker:  iconMapSession,
	}
}

func mapWorkSessions(mapID, dir string) map[string]tmuxmod.WorkSession {
	session := "pop-map-" + mapID
	return map[string]tmuxmod.WorkSession{
		session: {Session: session, Kind: "map", ID: mapID, Dir: dir},
	}
}

// Attribution is the whole decision in one table: a Map is rooted at a
// repository's Trunk, so tmux's start directory names its project — matched to the
// project *group*, which is what keeps a bare repo's Map at depth 1 instead of
// hanging it off the Trunk worktree row that the directory literally names.
func TestAttributeMapSessionsNestsUnderItsProjectGroup(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		fx   []rowFixture
		dir  string
		want []string
	}{
		{
			name: "non-bare repo: the Trunk is the project row itself",
			fx: []rowFixture{
				liveProject("hawk", "/src/hawk", "hawk"),
				mapSessionFixture("2026-08-03-demo"),
			},
			dir:  "/src/hawk",
			want: []string{"hawk ▾", "  2026-08-03-demo"},
		},
		{
			name: "bare repo: the Trunk is a worktree row, and the Map still lands beside it",
			fx: []rowFixture{
				rowFixture{name: "kestrel/main", path: "/wt/kestrel/main", session: "kestrel/main", icon: iconDirSession, worktree: true, repo: "kestrel", label: "kestrel"},
				mapSessionFixture("2026-08-03-demo"),
			},
			dir:  "/wt/kestrel/main",
			want: []string{"kestrel ▾", "  main", "  2026-08-03-demo"},
		},
		{
			name: "a trailing slash is still the same directory",
			fx: []rowFixture{
				liveProject("hawk", "/src/hawk", "hawk"),
				mapSessionFixture("2026-08-03-demo"),
			},
			dir:  "/src/hawk/",
			want: []string{"hawk ▾", "  2026-08-03-demo"},
		},
		{
			name: "no configured project: the Map stays a top-level row and nothing is synthesized",
			fx: []rowFixture{
				liveProject("hawk", "/src/hawk", "hawk"),
				mapSessionFixture("2026-08-03-demo"),
			},
			dir:  "/elsewhere/untracked",
			want: []string{"hawk", "2026-08-03-demo"},
		},
		{
			name: "an unstamped directory is the same fallback",
			fx: []rowFixture{
				liveProject("hawk", "/src/hawk", "hawk"),
				mapSessionFixture("2026-08-03-demo"),
			},
			dir:  "",
			want: []string{"hawk", "2026-08-03-demo"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items, meta := fixtureRows(tc.fx...)
			rows, mapMeta := attributeMapSessions(items, mapWorkSessions("2026-08-03-demo", tc.dir), meta)

			expanded := map[string]bool{}
			for _, it := range rows {
				expanded[it.Path] = true
			}
			expanded[projectGroupPathPrefix+"kestrel"] = true
			assertRowNames(t, buildProjectRows(rows, mapMeta, config.WorktreeDisplayNested, expanded), tc.want...)

			// Whatever it resolved to, nothing invents a parent for a Map: a
			// synthesized row's Enter would open the Trunk session of a project pop
			// does not track.
			for _, it := range buildProjectRows(rows, mapMeta, config.WorktreeDisplayNested, expanded) {
				if isSynthesizedProjectRow(it) && it.Path != projectGroupPathPrefix+"kestrel" {
					t.Errorf("synthesized row %q", it.Path)
				}
			}
		})
	}
}

// The label is the map id whole — date included, because it is the string typed
// into every `pop map` verb and two maps can share a slug — with only pop's
// internal session prefix dropped. Flat mode and a typed query carry the project
// prefix, exactly as a worktree row does; the nested level drops it because the
// level already says which project this is.
func TestAttributeMapSessionsLabelsTheFullMapID(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		liveProject("hawk", "/src/hawk", "hawk"),
		mapSessionFixture("2026-08-03-worktree-session-locality"),
	)
	rows, mapMeta := attributeMapSessions(items, mapWorkSessions("2026-08-03-worktree-session-locality", "/src/hawk"), meta)

	flat := buildProjectRows(rows, mapMeta, config.WorktreeDisplayFlat, nil)
	assertRowNames(t, flat, "hawk", "hawk/2026-08-03-worktree-session-locality")

	nested := buildProjectRows(rows, mapMeta, config.WorktreeDisplayNested, map[string]bool{"/src/hawk": true})
	assertRowNames(t, nested, "hawk ▾", "  2026-08-03-worktree-session-locality")

	// Display only: the row still attaches to the session it always did.
	child := rowByPath(t, nested, tmuxSessionPathPrefix+"pop-map-2026-08-03-worktree-session-locality")
	if !isStandaloneSession(child) || standaloneSessionName(child) != "pop-map-2026-08-03-worktree-session-locality" {
		t.Errorf("child Path = %q, want the untouched standalone session path", child.Path)
	}
	if child.SessionName != "pop-map-2026-08-03-worktree-session-locality" {
		t.Errorf("child SessionName = %q, want the session name unchanged", child.SessionName)
	}
}

// One glyph, not two: `□` said only "standalone", which a Map always is, and `◆`
// said "Map", which `◇` now says on its own. That is what frees `◆` for the Work
// dashboard's Project routine badge.
func TestMapRowRendersOneGlyph(t *testing.T) {
	t.Parallel()
	items, meta := fixtureRows(
		liveProject("hawk", "/src/hawk", "hawk"),
		mapSessionFixture("2026-08-03-demo"),
	)
	rows, mapMeta := attributeMapSessions(items, mapWorkSessions("2026-08-03-demo", "/src/hawk"), meta)
	nested := buildProjectRows(rows, mapMeta, config.WorktreeDisplayNested, map[string]bool{"/src/hawk": true})

	child := rowByPath(t, nested, tmuxSessionPathPrefix+"pop-map-2026-08-03-demo")
	if child.Icon != iconNestedMapSession || child.Marker != "" {
		t.Errorf("Map row glyphs = Icon %q / Marker %q, want %q and nothing else",
			child.Icon, child.Marker, iconNestedMapSession)
	}

	// The unattributed Map is the same row at depth 0, so the glyph does not depend
	// on attribution succeeding.
	rows, mapMeta = attributeMapSessions(items, mapWorkSessions("2026-08-03-demo", "/elsewhere"), meta)
	top := rowByPath(t, buildProjectRows(rows, mapMeta, config.WorktreeDisplayNested, nil), tmuxSessionPathPrefix+"pop-map-2026-08-03-demo")
	if top.Icon != iconNestedMapSession || top.Marker != "" || top.Depth != 0 {
		t.Errorf("fallback Map row = Icon %q / Marker %q / Depth %d, want %q at depth 0",
			top.Icon, top.Marker, top.Depth, iconNestedMapSession)
	}
}

// The implementation trap ticket 13 called out: a Map row has no directory in
// History, so its recency comes from tmux session activity via the
// standalone-session fallback in sortByUnifiedRecency. Nesting must not cost it
// that — the row keeps its `tmux:`-prefixed Path, which is what the fallback keys
// on — and a Map sorts among its siblings by that recency with no pin, so grilling
// a map an hour ago pulls its project down.
func TestMapRowRecencyComesFromSessionActivityAfterNesting(t *testing.T) {
	t.Parallel()

	baseItems := []ui.Item{
		{Name: "hawk", Path: "/src/hawk", SessionName: "hawk"},
		{Name: "hawk/fix-auth", Path: "/wt/hawk/fix-auth", SessionName: "hawk/fix-auth"},
		{Name: "kestrel", Path: "/src/kestrel", SessionName: "kestrel"},
	}
	meta := map[string]projectRowMeta{
		"/src/hawk":         {Repo: "hawk", RepoLabel: "hawk"},
		"/wt/hawk/fix-auth": {IsWorktree: true, Repo: "hawk"},
		"/src/kestrel":      {Repo: "kestrel", RepoLabel: "kestrel"},
	}
	workSessions := mapWorkSessions("2026-08-03-demo", "/src/hawk")

	// The project rows get their recency from History, as they do in production;
	// only the Map row has none, which is the fallback under test.
	hist := &history.History{Entries: []history.Entry{
		{Path: "/src/hawk", LastAccess: time.Unix(100, 0)},
		{Path: "/wt/hawk/fix-auth", LastAccess: time.Unix(200, 0)},
		{Path: "/src/kestrel", LastAccess: time.Unix(300, 0)},
	}}

	render := func(mapActivity int64) []string {
		activity := map[string]int64{
			"hawk":                    100,
			"hawk/fix-auth":           200,
			"kestrel":                 300,
			"pop-map-2026-08-03-demo": mapActivity,
		}
		sessionRows := buildSessionAwareItemsWith(baseItems, hist, activity, nil, nil, workSessions)
		rows, mapMeta := attributeMapSessions(sessionRows, workSessions, meta)
		expanded := map[string]bool{"/src/hawk": true, "/src/kestrel": true}
		return rowNames(buildProjectRows(rows, mapMeta, config.WorktreeDisplayNested, expanded))
	}

	// Oldest first, as the picker lists them: the map is the stalest thing here, so
	// it sits above its own sibling and hawk stays above kestrel.
	if got := render(50); !reflect.DeepEqual(got, []string{"hawk ▾", "  2026-08-03-demo", "  fix-auth", "kestrel"}) {
		t.Errorf("stale map: rows = %q", got)
	}
	// Grilled most recently of anything: the map sorts below its sibling and drags
	// hawk under kestrel, which is a project sinking to its most recent child.
	if got := render(400); !reflect.DeepEqual(got, []string{"kestrel", "hawk ▾", "  fix-auth", "  2026-08-03-demo"}) {
		t.Errorf("fresh map: rows = %q", got)
	}
	// The activity map is the only source: at the epoch the map is the stalest
	// possible row, which is what the fallback answering with it looks like.
	if got := render(0); !reflect.DeepEqual(got, []string{"hawk ▾", "  2026-08-03-demo", "  fix-auth", "kestrel"}) {
		t.Errorf("map at the epoch: rows = %q", got)
	}
}

// End to end through RunProject: the row set the picker is handed nests the live
// Map session under the project whose Trunk it was started in, and the Work
// sessions read costs the one list-sessions call it already cost.
func TestRunProjectNestsMapSessionUnderItsProject(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectDir := filepath.Join(root, "hawk")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	d := testProjectDeps(t)
	d.LoadConfig = func() (*config.Config, error) {
		return &config.Config{
			Projects: []config.ProjectEntry{{Path: projectDir}},
			Project:  &config.ProjectConfig{WorktreeDisplay: "nested"},
		}, nil
	}
	d.ManagedWorktrees = func() []project.ExpandedProject { return nil }
	d.SessionActivity = func() map[string]int64 {
		return map[string]int64{"hawk": 100, "pop-map-2026-08-03-demo": 200}
	}
	// Config resolves a project path through EvalSymlinks, and the Trunk a Map
	// session is started in comes from the same config, so the two agree in
	// production; on macOS a tmpdir is itself a symlink, so the fixture has to.
	trunk, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	workSessionReads := 0
	d.WorkSessions = func() map[string]tmuxmod.WorkSession {
		workSessionReads++
		return mapWorkSessions("2026-08-03-demo", trunk)
	}

	var rendered []string
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		rendered = rowNames(items)
		return ui.Result{Action: ui.ActionCancel}, nil
	}
	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	if !reflect.DeepEqual(rendered, []string{"hawk " + iconRowCollapsed}) {
		t.Fatalf("rows = %q, want the project row holding the Map session folded away", rendered)
	}
	if workSessionReads != 1 {
		t.Errorf("WorkSessions read %d times per picker opening, want 1", workSessionReads)
	}
}
