// Package work_test drives the seam from outside: the conformance table below
// runs the real adapters through the whole interface, which an in-package test
// could never do — every kind imports `work`, so only an external test package
// can import a kind.
package work_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// conformanceCase is one Work kind under test, wired over the shared fixture
// below. A fourth kind gets its conformance coverage by being added here.
type conformanceCase struct {
	name string
	id   work.KindID
	kind func(t *testing.T, f fixture) work.Kind
	// container is the id Load must produce, and the container every capability
	// assertion runs against.
	container string
	// items is how many Work items that container carries.
	items int
	// wantActions and wantItemActions are the verbs offered over the container and
	// over its first item.
	wantActions     []work.Verb
	wantItemActions []work.Verb
	// wantSummary is the kind's header phrases for its own containers.
	wantSummary []string
	// callerModal is a verb the kind hands back for the caller to dispatch, or
	// empty when it has none.
	callerModal work.Verb
}

func conformanceCases() []conformanceCase {
	return []conformanceCase{
		{
			name:      "task set",
			id:        ref.KindTaskSet,
			container: "2026-07-01-demo",
			items:     2,
			kind: func(t *testing.T, f fixture) work.Kind {
				return setkind.New(&setkind.Deps{
					Tasks:      f.tasks,
					Project:    f.project,
					Config:     f.cfg,
					Groups:     f.groups,
					LiveDrains: func() ([]tasks.RunningDrain, error) { return nil, nil },
					Refresh: func(string) (*tasks.RefreshResult, error) {
						return &tasks.RefreshResult{
							Rows: []tasks.Row{{ID: "2026-07-01-demo", Status: tasks.StatusReady}},
							Manifests: map[string]*tasks.Manifest{"2026-07-01-demo": {
								Dir:   "/repo/tasks/2026-07-01-demo",
								Valid: true,
								Tasks: []tasks.Task{
									{ID: "01-first", File: "01-first.md", Title: "First", Type: "AFK", Status: tasks.TaskOpen},
									{ID: "02-second", File: "02-second.md", Title: "Second", Type: "AFK", Status: tasks.TaskDone, BlockedBy: []string{"01-first"}},
								},
							}},
						}, nil
					},
				})
			},
			wantActions: []work.Verb{
				setkind.VerbDrain, setkind.VerbBind, setkind.VerbAutoDrain, setkind.VerbStatus,
				setkind.VerbAssist, work.VerbShell, setkind.VerbArchive, work.VerbCopyName,
			},
			wantItemActions: []work.Verb{setkind.VerbComplete, setkind.VerbSkip, work.VerbCopyName},
			wantSummary:     []string{"1 task set", "1 ready"},
			callerModal:     setkind.VerbDrain,
		},
		{
			name:      "map",
			id:        ref.KindMap,
			container: "2026-07-01-chart",
			items:     1,
			kind: func(t *testing.T, f fixture) work.Kind {
				return wayfinder.NewMapKind(&wayfinder.MapKindDeps{
					Wayfinder: &wayfinder.Deps{FS: f.fs, Tasks: f.tasks},
					Project:   f.project,
					Config:    f.cfg,
					Groups:    f.groups,
				})
			},
			wantActions:     []work.Verb{wayfinder.VerbWork, work.VerbShell, work.VerbCopyName},
			wantItemActions: []work.Verb{wayfinder.VerbWork, work.VerbCopyName},
			wantSummary:     []string{"1 map"},
		},
	}
}

// TestKindConformance drives every real adapter through every method of the seam.
// It is deliberately one table at the interface rather than a suite per adapter:
// the contract is what `work` may assume of any kind, so a new kind should get
// its coverage by being named in the table.
func TestKindConformance(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			k := tc.kind(t, f)

			if k.ID() != tc.id {
				t.Fatalf("ID() = %q, want %q", k.ID(), tc.id)
			}

			containers, err := k.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			var c work.Container
			found := false
			for _, got := range containers {
				if got.ID == tc.container {
					c, found = got, true
				}
			}
			if !found {
				t.Fatalf("Load did not produce container %q: %+v", tc.container, containers)
			}
			if c.Kind != tc.id {
				t.Fatalf("container kind = %q, want %q", c.Kind, tc.id)
			}
			if c.Project == "" || c.Status == "" || c.CursorKey == "" {
				t.Fatalf("container %+v must carry a project, a status label and a cursor key", c)
			}
			// Every kind composes a STATUS cell, and its first segment is the label a
			// surface colours by bucket.
			segments := k.StatusCell(c)
			if len(segments) == 0 || segments[0].Tone != work.ToneLabel || segments[0].Text != c.Status {
				t.Fatalf("StatusCell = %+v, want a leading label segment carrying %q", segments, c.Status)
			}
			if cell := work.StatusCellText(segments); cell == "" {
				t.Fatalf("StatusCell composed nothing for %+v", c)
			}
			if c.Ref() != (ref.WorkRef{Kind: tc.id, ContainerID: tc.container}) {
				t.Fatalf("Ref() = %q, want %s:%s", c.Ref(), tc.id, tc.container)
			}
			if len(c.Items) != tc.items {
				t.Fatalf("items = %+v, want %d", c.Items, tc.items)
			}

			// Less is asked only about its own kind's containers, and must be a strict
			// order: a container never sorts before itself.
			if k.Less(c, c) {
				t.Fatalf("Less(c, c) = true, want a strict order")
			}

			if got := verbsOf(k.Actions(c)); !slices.Equal(got, tc.wantActions) {
				t.Fatalf("Actions = %v, want %v", got, tc.wantActions)
			}
			item := c.Items[0]
			// An item names itself, says what it is, and points at its text with a
			// path its reader can open without knowing the kind's layout.
			if item.ID == "" || item.Status == "" {
				t.Fatalf("item %+v must carry an id and a status", item)
			}
			if !filepath.IsAbs(item.File) {
				t.Fatalf("item file = %q, want an absolute path", item.File)
			}
			if got := verbsOf(k.ItemActions(c, item)); !slices.Equal(got, tc.wantItemActions) {
				t.Fatalf("ItemActions = %v, want %v", got, tc.wantItemActions)
			}

			// Perform: the two shared verbs behave the same on every kind, an unknown
			// verb is refused by name, and a caller-dispatched verb says so rather than
			// half-acting.
			out, err := k.Perform(c, nil, work.VerbCopyName)
			if err != nil || out.Clipboard != tc.container {
				t.Fatalf("copy-name = %+v, %v, want the container id on the clipboard", out, err)
			}
			out, err = k.Perform(c, nil, work.VerbShell)
			switch {
			case c.Checkout == "":
				// A kind that resolves no directory refuses the shell rather than
				// picking one.
				if err == nil {
					t.Fatalf("shell = %+v, want a refusal when the kind resolves no checkout", out)
				}
			case err != nil:
				t.Fatalf("shell: %v", err)
			case out.Kind != work.OutcomeHandoff || out.Handoff.Dir != c.Checkout:
				t.Fatalf("shell = %+v, want a handoff into %q", out, c.Checkout)
			}
			if _, err := k.Perform(c, nil, work.Verb("no-such-verb")); err == nil {
				t.Fatal("an unknown verb must be refused")
			}
			if tc.callerModal != "" {
				out, err := k.Perform(c, nil, tc.callerModal)
				if err != nil || out.Kind != work.OutcomeCallerModal {
					t.Fatalf("%s = %+v, %v, want a caller-dispatched outcome", tc.callerModal, out, err)
				}
			}

			if got := k.Summary(containers); !slices.Equal(got, tc.wantSummary) {
				t.Fatalf("Summary = %v, want %v", got, tc.wantSummary)
			}
		})
	}
}

// TestSnapshotOrdersByKindPrecedenceThenKindLess pins the seam's whole ordering
// rule over both real adapters: containers arrive in kind order — task sets, then
// Maps — and inside a kind in that kind's own order. This is the ordering change
// the seam makes deliberately: a Map can no longer interleave between two
// projects' task sets, which is the accepted cost of having no shared status
// taxonomy to rank kinds against.
func TestSnapshotOrdersByKindPrecedenceThenKindLess(t *testing.T) {
	f := newFixture(t)
	sets := setkind.New(&setkind.Deps{
		Tasks:      f.tasks,
		Project:    f.project,
		Config:     f.cfg,
		Groups:     f.groups,
		LiveDrains: func() ([]tasks.RunningDrain, error) { return nil, nil },
		Refresh: func(string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "2026-07-01-demo", Status: tasks.StatusReady},
				{ID: "2026-07-02-later", Status: tasks.StatusBlocked},
			}}, nil
		},
	})
	maps := wayfinder.NewMapKind(&wayfinder.MapKindDeps{
		Wayfinder: &wayfinder.Deps{FS: f.fs, Tasks: f.tasks},
		Project:   f.project,
		Config:    f.cfg,
		Groups:    f.groups,
	})

	// Wired Map-first on purpose: precedence is fixed by the closed enum, not by
	// the order `cmd` happens to wire.
	snap, err := work.BuildSnapshot([]work.Kind{maps, sets})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range snap.Containers {
		got = append(got, string(c.Kind)+":"+c.ID)
	}
	want := []string{
		// Task sets first, in the Task-set comparator's order (READY band above the
		// rest band).
		"task-set:2026-07-01-demo",
		"task-set:2026-07-02-later",
		// Then Maps.
		"map:2026-07-01-chart",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("snapshot order = %v, want %v", got, want)
	}
	// Header text: every kind's phrases, joined with · in kind order.
	if want := "2 task sets · 1 ready · 1 map"; snap.SummaryLine() != want {
		t.Fatalf("summary line = %q, want %q", snap.SummaryLine(), want)
	}
}

func verbsOf(actions []work.Action) []work.Verb {
	var out []work.Verb
	for _, a := range actions {
		out = append(out, a.Verb)
	}
	return out
}

// fixture is one repository group on a mock filesystem holding one task set's
// definition path and one active Map, shared by every kind in the table so the
// conformance run compares like with like.
type fixture struct {
	fs      *deps.MockFileSystem
	tasks   *tasks.Deps
	project *project.Deps
	cfg     *config.Config
	group   repogroup.Group
}

// groups is the group seam both kinds are wired to, the way `cmd` wires one
// resolution into every kind of a build.
func (f fixture) groups() ([]repogroup.Group, error) {
	return []repogroup.Group{f.group}, nil
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	storageDir := "/data/repos/repo-aaaa"
	mapDir := filepath.Join(storageDir, "maps", "2026-07-01-chart")
	files := map[string]string{
		filepath.Join(mapDir, "map.md"):                       "Status: active\n\n## Destination\nChart it\n",
		filepath.Join(mapDir, "issues", "01-first.md"):        "Type: grilling\nStatus: open\n\n# Q\n",
		filepath.Join(storageDir, "tasks", "definition.json"): "{}\n",
	}
	fs := &deps.MockFileSystem{
		GetwdFunc:       func() (string, error) { return "/repo/main", nil },
		UserHomeDirFunc: func() (string, error) { return dataHome, nil },
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		EvalSymlinksFunc: func(p string) (string, error) { return p, nil },
		StatFunc:         func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		ReadDirFunc: func(path string) ([]os.DirEntry, error) {
			entries := dirEntriesFor(path, files)
			if entries == nil {
				return nil, os.ErrNotExist
			}
			return entries, nil
		},
		ReadFileFunc: func(path string) ([]byte, error) {
			if content, ok := files[path]; ok {
				return []byte(content), nil
			}
			return nil, os.ErrNotExist
		},
	}
	td := &tasks.Deps{FS: fs}
	t.Cleanup(func() { _ = td.CloseStore() })
	tasksDir := filepath.Join(storageDir, "tasks")
	return fixture{
		fs:      fs,
		tasks:   td,
		project: &project.Deps{FS: fs},
		cfg:     &config.Config{},
		group: repogroup.Group{
			DefPath:       tasksDir,
			StatePath:     tasks.StatePathFor(tasksDir),
			StorageDir:    storageDir,
			RepoKey:       "repo-key",
			RepoCommonDir: "/repo/.git",
			ProjectName:   "pop",
			Rep:           &repogroup.Checkout{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
		},
	}
}

// dirEntriesFor synthesises directory listings for the fixture's file map.
func dirEntriesFor(path string, files map[string]string) []os.DirEntry {
	children := map[string]bool{}
	for filePath := range files {
		if !strings.HasPrefix(filePath, path+string(os.PathSeparator)) {
			continue
		}
		rel := strings.TrimPrefix(filePath, path+string(os.PathSeparator))
		parts := strings.Split(rel, string(os.PathSeparator))
		children[parts[0]] = len(parts) > 1 || children[parts[0]]
	}
	if len(children) == 0 {
		return nil
	}
	var out []os.DirEntry
	for name, isDir := range children {
		out = append(out, deps.MockDirEntry{NameVal: name, IsDirVal: isDir})
	}
	return out
}
