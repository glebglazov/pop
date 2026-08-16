package dashboard

import (
	"errors"
	"fmt"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

func mkdirDrainStoreDir(t *testing.T, td *tasks.Deps) {
	t.Helper()
	dir := filepath.Dir(tasks.DrainStorePathWith(td))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll drain store dir: %v", err)
	}
}

func dashboardTestDeps(t *testing.T, rows []tasks.Row, locks map[string]*tasks.RuntimeLockStatus) *drain.Deps {
	t.Helper()
	// The Work registry (pop.db) is real-disk-only and opens off this Getenv, so a
	// Map fold's registration write (this package's Map fixtures are pre-manifest
	// header maps) lands in a disposable per-test directory rather than failing
	// against the unwritable default mock home.
	dataHome := t.TempDir()
	fs := &deps.MockFileSystem{
		EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
		ReadFileFunc: func(path string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
		StatFunc: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
	}
	git := &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			switch cmd {
			case "worktree list --porcelain":
				if strings.Contains(dir, "bare") {
					return "worktree /repo/bare.git\nbare\n\n", nil
				}
				return "worktree /repo/main\nHEAD abc\nbranch refs/heads/main\n\nworktree /repo/feature\nHEAD def\nbranch refs/heads/feature\n", nil
			case "branch --show-current":
				switch dir {
				case "/repo/main":
					return "main", nil
				case "/repo/feature":
					return "feature", nil
				case "/repo/bound":
					return "bound-branch", nil
				case "/repo/done":
					return "done-branch", nil
				}
				return "", nil
			default:
				return "", errors.New("unexpected git command: " + cmd)
			}
		},
	}
	tasksDeps := &tasks.Deps{FS: fs, Git: git}
	// The store handle is now process-cached; close it at test end so it does not
	// outlive this test's temp data dir (test cleanup, per ADR-0118).
	t.Cleanup(func() { _ = tasksDeps.CloseStore() })
	return &drain.Deps{
		Tasks:   tasksDeps,
		Project: &project.Deps{FS: fs, Git: git},
		Refresh: func(string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: rows}, nil
		},
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			if locks != nil {
				if lock, ok := locks[runtimePath]; ok {
					return lock
				}
			}
			return &tasks.RuntimeLockStatus{RuntimePath: runtimePath}
		},
	}
}

// TestRenderStatusMirrorsDashboardRows proves `pop work status` renders the
// same rows in the same order as the Work dashboard (ADR-0121 / ADR-0197): both
// consume the one comparator (tasks.SortWorkRows / Kind.Less)
// over the one row set, so the static status table's TASK SET order equals the
// sorted dashboard rows' order, under the Summary headline and without any
// retired inventory section. The row derivation itself lives in work's tests now
// (ADR-0143); here the rows are the build's output, fed to the same sort and the
// status render.
func TestRenderStatusMirrorsDashboardRows(t *testing.T) {
	td := queuetest.DataDeps(t)
	got := []DashboardRow{
		{Project: "pop", ID: "2026-01-01-rdy", RawStatus: tasks.StatusReady, DestKind: work.DestNeedsBind},
		{Project: "pop", ID: "2026-01-02-blk", RawStatus: tasks.StatusBlocked, DestKind: work.DestNeedsBind},
		{Project: "pop", ID: "2026-01-03-aa", RawStatus: tasks.StatusAwaitingApproval, DestKind: work.DestNeedsBind},
	}
	// The shared comparator orders the full set exactly as the build does. The
	// status table must render these same rows in this same order.
	tasks.SortWorkRows(got, "")
	wantOrder := make([]string, len(got))
	for i, r := range got {
		wantOrder[i] = r.ID
	}

	var out strings.Builder
	RenderStatus(&out, drain.StatusSnapshot{Tasks: td}, StatusTables{TaskSets: StatusTable{Kinds: (&drain.Deps{}).WorkKinds(nil), Rows: got}})
	text := out.String()

	if !strings.Contains(text, "Summary:") {
		t.Fatalf("status missing Summary headline:\n%s", text)
	}
	for _, header := range []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE"} {
		if !strings.Contains(text, header) {
			t.Fatalf("status table missing %q column header:\n%s", header, text)
		}
	}
	// The status table's TASK SET column must appear in the shared-comparator
	// order — the dashboard's row set/order, rendered statically.
	prev := -1
	for _, id := range wantOrder {
		idx := strings.Index(text, id)
		if idx < 0 {
			t.Fatalf("status table missing set %q:\n%s", id, text)
		}
		if idx <= prev {
			t.Fatalf("status table order breaks dashboard order at %q:\n%s", id, text)
		}
		prev = idx
	}
	for _, omit := range []string{
		"Picked-up sets:",
		"Active worktrees:",
		"Queued ready sets:",
		"Blocked:",
		"Awaiting approval:",
		"Skipped repositories:",
	} {
		if strings.Contains(text, omit) {
			t.Fatalf("status output should not contain retired section %q:\n%s", omit, text)
		}
	}
}

// TestRenderStatusTableColumnsAndIndicator pins the static status table's
// column composition (ADR-0121): the STATUS cell recomposes IN PROGRESS from a
// live-drained READY set, the trailing ● live-drain indicator survives, and the
// WORKTREE destination cells render as plain (ANSI-free) labels.
func TestRenderStatusTableColumnsAndIndicator(t *testing.T) {
	td := queuetest.DataDeps(t)
	rows := []DashboardRow{
		{Project: "alpha", Started: true, ID: "2026-03-01-inp", RawStatus: tasks.StatusReady, LiveDrain: true, DestKind: work.DestManagedDirective},
		{Project: "bravo", ID: "2026-03-02-blk", RawStatus: tasks.StatusBlocked, DestKind: work.DestNeedsBind},
	}
	tasks.SortWorkRows(rows, "")

	var out strings.Builder
	RenderStatus(&out, drain.StatusSnapshot{Tasks: td}, StatusTables{TaskSets: StatusTable{Kinds: (&drain.Deps{}).WorkKinds(nil), Rows: rows}})
	text := out.String()

	for _, want := range []string{
		"IN PROGRESS",                 // live-drained READY → IN PROGRESS
		dashboardActivityClusterPlain, // trailing activity cluster
		work.DestLabelManagedWt,
		work.DestLabelNeedsBind,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status table missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("status table must be plain text (no ANSI):\n%q", text)
	}
}

func TestDashboardAutoDrainBadgeAndToggle(t *testing.T) {
	rows := []DashboardRow{
		{Project: "pop", Worktree: "/repo/main (main)", RawStatus: tasks.StatusReady, ID: "marked", AutoDrain: true},
		{Project: "pop", Worktree: "/repo/main (main)", RawStatus: tasks.StatusReady, ID: "plain"},
	}
	var rendered strings.Builder
	renderDashboardTable(&rendered, workPage(), testKinds(), rows, 0, 0, 20, livePaneCache{})
	if !strings.Contains(rendered.String(), "AD") {
		t.Fatalf("missing auto-drain flag:\n%s", rendered.String())
	}

	var toggledDef, toggledState, toggledSet string
	d := &drain.Deps{
		ToggleAutoDrain: func(defPath, statePath, setID string) (*tasks.AutoDrainResult, error) {
			toggledDef, toggledState, toggledSet = defPath, statePath, setID
			return &tasks.AutoDrainResult{TaskSetID: setID, AutoDrain: true}, nil
		},
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{{Project: "pop", Worktree: "/repo/main (main)", CursorKey: "pop\x00plain", RawStatus: tasks.StatusReady, ID: "plain", DefPath: "/repo/tasks", StatePath: "/repo/state.json"}}})
	// Auto-drain now lives behind the action menu: open with `a`, toggle with `a`.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a did not open the action menu")
	}
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("a did not close the action menu after dispatch")
	}
	if !got.snap.Containers[0].AutoDrain {
		t.Fatalf("toggle did not update badge immediately: %+v", got.snap.Containers[0])
	}
	msg := cmd().(dashboardToggleMsg)
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if toggledDef != "/repo/tasks" || toggledState != "/repo/state.json" || toggledSet != "plain" {
		t.Fatalf("toggle target = (%q, %q, %q)", toggledDef, toggledState, toggledSet)
	}
	if !got.snap.Containers[0].AutoDrain || got.err != nil {
		t.Fatalf("toggle result = row %+v err %v", got.snap.Containers[0], got.err)
	}
}

// TestDashboardAutoDrainToggleReflectsInRowAndCount proves the render-time STATUS
// composition (ADR-0108): a simulated auto-drain toggle updates the per-row `·
// auto-drain` marker and the header's auto-drain count together on the very next
// View pass — no dashboardTickMsg / dashboardRowsMsg poll is fed between the
// toggle and the render. Before the toggle neither the row cell nor the summary
// mentions auto-drain; after it, both do.
func TestDashboardAutoDrainToggleReflectsInRowAndCount(t *testing.T) {
	d := &drain.Deps{
		ToggleAutoDrain: func(defPath, statePath, setID string) (*tasks.AutoDrainResult, error) {
			return &tasks.AutoDrainResult{TaskSetID: setID, AutoDrain: true}, nil
		},
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00plain", RawStatus: tasks.StatusReady, ID: "plain", DefPath: "/repo/tasks", StatePath: "/repo/state.json"},
	}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = updated.(QueueDashboard)

	// Baseline: the READY row carries no auto-drain marker and the summary carries
	// no auto-drain count.
	before := m.View().Content
	if strings.Contains(before, "auto-drain") {
		t.Fatalf("baseline view should not mention auto-drain:\n%s", before)
	}

	// Toggle auto-drain on via the action menu (`a` opens, `a` dispatches).
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)

	// The same View pass — no poll tick — must reflect the toggle in both places.
	after := m.View().Content
	// The base label is bucket-colored; the auto-drain marker follows it plainly.
	if !strings.Contains(after, "\x1b[34mREADY\x1b[m · auto-drain") {
		t.Fatalf("row cell missing auto-drain marker after toggle:\n%s", after)
	}
	if !strings.Contains(after, "1 auto-drain") {
		t.Fatalf("summary count missing auto-drain after toggle:\n%s", after)
	}
}

// TestDashboardAutoDrainWaitingMarkerAndCount pins the ADR-0108 header tally:
// it counts waiting (consented AND not Picked-up) rows only. The per-row
// marker and the shared waiting predicate are work's (TestStatusCellComposition,
// TestAutoDrainWaiting); this is the queue-side summary-count consumer.
func TestDashboardAutoDrainWaitingMarkerAndCount(t *testing.T) {
	idle := DashboardRow{RawStatus: tasks.StatusReady, AutoDrain: true}
	pickedUp := DashboardRow{RawStatus: tasks.StatusReady, AutoDrain: true, LiveDrain: true}
	plain := DashboardRow{RawStatus: tasks.StatusReady}

	summary := dashboardSummary(testKinds(), []DashboardRow{idle, pickedUp, plain})
	if !strings.Contains(summary, "1 auto-drain") {
		t.Errorf("summary count: got %q, want exactly 1 auto-drain (waiting-only)", summary)
	}
}

func TestDashboardBKeyOpensBindModal(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{{Project: "pop", Worktree: "/repo/main (main)", CursorKey: "pop\x00set-bind", RawStatus: tasks.StatusReady, ID: "set-bind", DefPath: "/repo/tasks", StatePath: "/repo/state.json"}}})
	// Bind now lives behind the action menu: open with `a`, then `b`.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a did not open the action menu")
	}
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("b did not close the action menu after dispatch")
	}
	if got.bind == nil || !got.bind.loading || got.bind.row.ID != "set-bind" {
		t.Fatalf("bind modal = %+v, want loading modal for set-bind", got.bind)
	}
	if cmd == nil {
		t.Fatalf("b key did not return a worktree-loading command")
	}
}

func TestDashboardActionMenuOpenAndClose(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set", RawStatus: tasks.StatusReady, ID: "set", RuntimePath: "/repo/wt"},
	}})
	m.width = 120
	m.height = 20

	// `a` opens the overlay, anchored to the cursored row, with the menu hint.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a did not open the action menu")
	}
	if cmd != nil {
		t.Fatal("opening the menu should not dispatch a command")
	}
	if got.menu.row.ID != "set" {
		t.Fatalf("menu opened on %q, want set", got.menu.row.ID)
	}
	view := got.View().Content
	if !strings.Contains(view, "actions") {
		t.Fatalf("menu caption not rendered:\n%s", view)
	}
	if !strings.Contains(view, "enter/letter run · esc close") {
		t.Fatalf("menu hint not rendered:\n%s", view)
	}

	// `esc` closes the overlay without quitting.
	updated, cmd = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("esc did not close the action menu")
	}
	if cmd != nil {
		t.Fatal("closing the menu should not quit or dispatch")
	}
}

func TestDashboardFormerDirectKeysInertAtTopLevel(t *testing.T) {
	for _, key := range []string{"i", "I", "b", "u", "U", "p", "P", "O", "d", "m"} {
		m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{{Project: "pop", Worktree: "/repo/wt (main)", CursorKey: "pop\x00set", RawStatus: tasks.StatusReady, ID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json", RuntimePath: "/repo/wt", Bound: true, Parked: true}}})
		updated, cmd := m.Update(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
		got := updated.(QueueDashboard)
		if cmd != nil {
			t.Fatalf("%q at top level dispatched a command; verbs must run only through the menu", key)
		}
		if got.menu != nil || got.bind != nil || got.abandon != nil || got.drainPick != nil {
			t.Fatalf("%q at top level opened a modal: menu=%v bind=%v abandon=%v drain=%v",
				key, got.menu, got.bind, got.abandon, got.drainPick)
		}
		if got.snap.Containers[0].AutoDrain {
			t.Fatalf("%q at top level toggled auto-drain", key)
		}
	}
}

func TestDashboardActionMenuContextFiltering(t *testing.T) {
	keysFor := func(row DashboardRow) []string {
		var keys []string
		for _, item := range dashboardMenuItems(testKinds(), row) {
			keys = append(keys, item.key)
		}
		return keys
	}

	// A plain ready set: only the unconditional verbs plus auto-drain (non-orphaned).
	plain := keysFor(DashboardRow{ID: "plain", RuntimePath: "/wt"})
	if want := []string{"I", "S", "O", "m", "b", "a", "s", "x", "y"}; !reflect.DeepEqual(plain, want) {
		t.Fatalf("plain row verbs = %v, want %v", plain, want)
	}

	// Bound row gains unbind; unbound row does not.
	if got := keysFor(DashboardRow{ID: "bound", Bound: true}); !contains(got, "u") {
		t.Fatalf("bound row missing unbind: %v", got)
	}
	if got := keysFor(DashboardRow{ID: "unbound"}); contains(got, "u") {
		t.Fatalf("unbound row should not offer unbind: %v", got)
	}

	// Parked row gains unpark; non-parked row does not.
	if got := keysFor(DashboardRow{ID: "parked", Parked: true}); !contains(got, "r") {
		t.Fatalf("parked row missing unpark: %v", got)
	}
	if got := keysFor(DashboardRow{ID: "live"}); contains(got, "r") {
		t.Fatalf("non-parked row should not offer unpark: %v", got)
	}

	// Auto-drain is offered for non-orphaned rows only.
	if got := keysFor(DashboardRow{ID: "orphan", Orphaned: true}); contains(got, "a") {
		t.Fatalf("orphaned row should not offer auto-drain: %v", got)
	}
}

func TestDashboardActionMenuVerbDispatch(t *testing.T) {
	newModel := func() QueueDashboard {
		return newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{{Project: "pop", Worktree: "/repo/wt (main)", CursorKey: "pop\x00set", RawStatus: tasks.StatusReady, ID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json", RuntimePath: "/repo/wt", Bound: true}}})
	}

	// Letter path: `a` then `u` opens the unbind confirm and closes the menu.
	updated, _ := newModel().Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("letter dispatch did not close the menu")
	}
	if got.abandon == nil {
		t.Fatal("letter dispatch did not open the unbind confirm")
	}
	if cmd != nil {
		t.Fatal("unbind confirm should not dispatch before confirmation")
	}

	// Highlight + Enter path: move onto the bind verb, press Enter.
	updated, _ = newModel().Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got = updated.(QueueDashboard)
	bindIdx := -1
	for i, item := range got.menu.list.Items() {
		if item.key == "b" {
			bindIdx = i
		}
	}
	if bindIdx < 0 {
		t.Fatalf("bind verb absent from menu: %+v", got.menu.list.Items())
	}
	for got.menu.list.Cursor() != bindIdx {
		updated, _ = got.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		got = updated.(QueueDashboard)
	}
	updated, cmd = got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("enter dispatch did not close the menu")
	}
	if got.bind == nil || got.bind.row.ID != "set" {
		t.Fatalf("enter dispatch did not open bind modal: %+v", got.bind)
	}
	if cmd == nil {
		t.Fatal("bind dispatch should return a worktree-loading command")
	}
}

func TestDashboardActionMenuArchiveDispatch(t *testing.T) {
	var archivedDef, archivedSet string
	d := &drain.Deps{
		SetArchived: func(defPath, setID string, archived bool) error {
			if !archived {
				t.Fatalf("row-level archive wrote archived=false for %s", setID)
			}
			archivedDef, archivedSet = defPath, setID
			return nil
		},
	}
	// A DONE, bound row: archive is offered regardless of status.
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{{Project: "pop", Worktree: "/repo/wt (main)", CursorKey: "pop\x00set", RawStatus: tasks.StatusDone, ID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json", RuntimePath: "/repo/wt", Bound: true}}})

	// Archive lives behind the action menu: open with `a`, archive with `x`.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a did not open the action menu")
	}
	var keys []string
	for _, item := range got.menu.list.Items() {
		keys = append(keys, item.key)
	}
	if !contains(keys, "x") {
		t.Fatalf("archive verb absent from a DONE bound row's menu: %v", keys)
	}

	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("x did not close the action menu after dispatch")
	}
	// No confirmation prompt: archiving opens no modal (it is fully reversible).
	if got.bind != nil || got.abandon != nil || got.drainPick != nil {
		t.Fatalf("archive opened a confirmation modal: bind=%v abandon=%v drain=%v", got.bind, got.abandon, got.drainPick)
	}
	if cmd == nil {
		t.Fatal("archive dispatch returned no command")
	}
	msg, ok := cmd().(dashboardKindVerbMsg)
	if !ok {
		t.Fatalf("archive cmd produced %T, want dashboardKindVerbMsg", cmd())
	}
	if msg.err != nil {
		t.Fatalf("archive msg err = %v", msg.err)
	}
	if archivedDef != "/repo/tasks" || archivedSet != "set" {
		t.Fatalf("archive target = (%q, %q), want (/repo/tasks, set)", archivedDef, archivedSet)
	}

	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if got.err != nil {
		t.Fatalf("archive result err = %v", got.err)
	}
	if !strings.Contains(got.flash.Text(), "archived") {
		t.Fatalf("status = %q, want archived confirmation", got.flash.Text())
	}
}

// TestDashboardArchiveRetainsBinding exercises the default archive flag-write
// path end to end: archiving a bound set sets the reversible archived flag in
// Task state (so the row drops out on the next build, which excludes Archived
// sets) while leaving the set's Worktree binding intact.
func TestDashboardArchiveRetainsBinding(t *testing.T) {
	td := queuetest.DataDeps(t)

	tasksDir := filepath.Join(t.TempDir(), "tasks")
	statePath := tasks.StatePathFor(tasksDir)
	canon, err := tasks.CanonicalDefinitionPathWith(td, tasksDir)
	if err != nil {
		t.Fatal(err)
	}

	// Register a non-archived set directly in Task state.
	if err := tasks.UpdateGlobalStateWith(td, statePath, func(state *tasks.GlobalState) error {
		if state.Tasks == nil {
			state.Tasks = map[string]*tasks.TaskEntry{}
		}
		state.Tasks[canon] = &tasks.TaskEntry{TaskSets: []tasks.RegisteredTaskSet{{ID: "set-1"}}}
		return nil
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	// A Worktree binding exists for the set before archiving.
	queuetest.SeedBindingStore(t, td, map[string]drain.WorktreeBinding{
		"proj\x00set-1": {RuntimePath: "/repo/wt", Branch: "set-1", Project: "proj"},
	})

	d := &drain.Deps{Tasks: td}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{{Project: "proj", CursorKey: "proj\x00set-1", RawStatus: tasks.StatusDone, ID: "set-1", DefPath: tasksDir, StatePath: statePath, RuntimePath: "/repo/wt", Bound: true}}})

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	got = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("archive dispatch returned no command")
	}
	msg, ok := cmd().(dashboardKindVerbMsg)
	if !ok {
		t.Fatalf("archive msg type = %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("archive msg err = %v", msg.err)
	}

	// The reversible archived flag is now set through the real flag-write path,
	// so the dashboard's next build (which excludes Archived sets) drops the row.
	state, err := tasks.LoadGlobalStateWith(td, statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[canon]
	if entry == nil || len(entry.TaskSets) != 1 {
		t.Fatalf("registered sets = %+v, want one", entry)
	}
	if !entry.TaskSets[0].Archived {
		t.Fatalf("set-1 not archived after dispatch: %+v", entry.TaskSets[0])
	}

	// The Worktree binding is retained — archive never touches it.
	after := queuetest.LoadBindingStore(t, td)
	if len(after) != 1 {
		t.Fatalf("binding state = %+v, want retained after archive", after)
	}
	if _, ok := after["proj\x00set-1"]; !ok {
		t.Fatalf("binding for set-1 lost after archive: %+v", after)
	}
}

func TestDashboardActionMenuAnchorsBelowAndFlipsAbove(t *testing.T) {
	// dashboardMenuPlaceBelow: a cursor near the top fits the menu below it; a
	// cursor low in the list does not, so it flips above.
	if !dashboardMenuPlaceBelow(0, 6, 24) {
		t.Fatal("menu should render below a top-of-list cursor")
	}
	if dashboardMenuPlaceBelow(18, 6, 24) {
		t.Fatal("menu should flip above a bottom-of-list cursor")
	}
	if !dashboardMenuPlaceBelow(5, 6, 0) {
		t.Fatal("menu should default below when height is unknown")
	}

	// dashboardMenuPlaceBelowTwoLine: each logical row consumes two physical
	// lines, so the available space below the cursor is halved.
	if !dashboardMenuPlaceBelowTwoLine(0, 6, 24) {
		t.Fatal("two-line menu should render below a top-of-list cursor")
	}
	if dashboardMenuPlaceBelowTwoLine(8, 6, 24) {
		t.Fatal("two-line menu should flip above a low cursor (each row is two lines)")
	}
	if !dashboardMenuPlaceBelowTwoLine(5, 6, 0) {
		t.Fatal("two-line menu should default below when height is unknown")
	}

	rows := make([]DashboardRow, 20)
	for i := range rows {
		id := fmt.Sprintf("set-%02d", i)
		rows[i] = DashboardRow{Project: "pop", CursorKey: "pop\x00" + id, RawStatus: tasks.StatusReady, ID: id}
	}

	// Cursor at the top: the menu caption sits below the cursor row.
	mTop := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	mTop.width = 120
	mTop.height = 24
	mTop.list.SetCursor(0)
	updated, _ := mTop.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	topView := updated.(QueueDashboard).View().Content
	if got := menuCaptionLine(topView); got <= cursorRowLine(topView, "set-00") {
		t.Fatalf("top cursor: caption line %d should be below cursor row:\n%s", got, topView)
	}

	// Cursor at the bottom: the menu caption flips above the cursor row.
	mBot := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	mBot.width = 120
	mBot.height = 24
	mBot.list.SetCursor(len(rows) - 1)
	updated, _ = mBot.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	botView := updated.(QueueDashboard).View().Content
	if got := menuCaptionLine(botView); got >= cursorRowLine(botView, "set-19") {
		t.Fatalf("bottom cursor: caption line %d should be above cursor row:\n%s", got, botView)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func menuCaptionLine(view string) int {
	return dashboardTestLineIndex(strings.Split(view, "\n"), "actions")
}

func cursorRowLine(view, setID string) int {
	return dashboardTestLineIndex(strings.Split(view, "\n"), setID)
}

func TestDashboardStatusKeysOpenDetailViewAndClosePreservesCursor(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00first", RawStatus: tasks.StatusReady, ID: "first"},
		{Project: "pop", CursorKey: "pop\x00second", RawStatus: tasks.StatusReady, ID: "second"},
	}})
	m.list.SetCursor(1)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)
	if got.detail == nil || got.detail.row.ID != "second" {
		t.Fatalf("detail view = %+v, want the detail for second", got.detail)
	}
	if cmd != nil {
		t.Fatalf("l key should open the detail from the container already in hand")
	}
	// View must be the detail view (no table).
	view := got.View().Content
	if strings.Contains(view, "project") && strings.Contains(view, "task set") {
		t.Fatalf("view should not show the table when detail is open:\n%s", view)
	}

	// Exit: h closes detail, returns to queue table, cursor unchanged.
	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	got = updated.(QueueDashboard)
	if cmd != nil {
		t.Fatalf("h in detail view should close without quitting")
	}
	if got.detail != nil || got.list.Cursor() != 1 {
		t.Fatalf("after close: detail=%+v cursor=%d, want nil and 1", got.detail, got.list.Cursor())
	}

	// Exit via esc also works.
	m2 := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00alpha", RawStatus: tasks.StatusReady, ID: "alpha"},
	}})
	updated, _ = m2.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	updated, cmd = updated.(QueueDashboard).Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil || updated.(QueueDashboard).detail != nil {
		t.Fatalf("esc should close detail view without quitting")
	}

	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{name: "enter", msg: tea.KeyPressMsg{Code: tea.KeyEnter}},
	} {
		t.Run(tc.name+" opens detail", func(t *testing.T) {
			m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
				{Project: "pop", CursorKey: "pop\x00target", RawStatus: tasks.StatusReady, ID: "target"},
			}})
			updated, cmd := m.Update(tc.msg)
			got := updated.(QueueDashboard)
			if got.detail == nil || got.detail.row.ID != "target" {
				t.Fatalf("detail view = %+v, want the detail for target", got.detail)
			}
			if cmd != nil {
				t.Fatalf("%s key should open the detail without a load", tc.name)
			}
		})
	}
}

func TestDashboardCtrlGOpensBoundCheckout(t *testing.T) {
	// Bound row: Ctrl-g surfaces its checkout path and quits so the command layer
	// can run the workbench-aware open after the TUI exits (task 02).
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set", RawStatus: tasks.StatusReady, ID: "set", RuntimePath: "/repo/wt", Bound: true},
	}})

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	got := updated.(QueueDashboard)
	if got.openCheckout != "/repo/wt" {
		t.Fatalf("openCheckout = %q, want /repo/wt", got.openCheckout)
	}
	if cmd == nil {
		t.Fatal("Ctrl-g on bound row: expected a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl-g command = %T, want tea.QuitMsg", cmd())
	}
	if got.flash.Text() != "" {
		t.Fatalf("flash = %q, want empty on bound open", got.flash.Text())
	}
}

func TestDashboardCtrlGUnboundRowShowsStatusAndDoesNotQuit(t *testing.T) {
	// Unbound row (no RuntimePath): Ctrl-g shows an inline status message and keeps
	// the dashboard running — no quit, no surfaced checkout (task 02).
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00unbound", RawStatus: tasks.StatusReady, ID: "unbound"},
	}})

	updated, cmd := m.update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	got := updated.(QueueDashboard)
	if cmd != nil {
		t.Fatalf("Ctrl-g on unbound row should not quit, got cmd %T", cmd())
	}
	if got.openCheckout != "" {
		t.Fatalf("openCheckout = %q, want empty on unbound row", got.openCheckout)
	}
	if got.flash.Text() != dashboardNoCheckoutMessage {
		t.Fatalf("flash = %q, want %q", got.flash.Text(), dashboardNoCheckoutMessage)
	}
	if strings.Contains(got.flash.Text(), "task set") {
		t.Fatalf("Ctrl-g is offered on every kind's rows; flash %q names one kind", got.flash.Text())
	}
}

func TestDashboardViewUsesTaskTableHeaderAndBottomShortcutLegend(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00set", ID: "set", RawStatus: tasks.StatusReady, AutoDrain: true, LiveDrain: true},
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00done", ID: "done", RawStatus: tasks.StatusDone},
	}})
	m.width = 120
	m.height = 8

	view := m.View().Content
	if strings.Contains(view, "Queue dashboard") {
		t.Fatalf("task-set list should use summary instead of dashboard title:\n%s", view)
	}
	// The auto-drain set here is Picked-up, so per ADR-0108 it drops out of the
	// waiting-only auto-drain tally (the live-drain indicator already signals it).
	if !strings.Contains(view, "Work · active · 2 task sets · 1 ready · 1 running") {
		t.Fatalf("task-set list should render useful summary:\n%s", view)
	}
	if strings.Contains(view, "auto-drain") {
		t.Fatalf("Picked-up auto-drain set should not surface an auto-drain marker/count:\n%s", view)
	}
	for _, want := range []string{"PROJECT  TASK SET  STATUS", "-------  --------  ------"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("line count = %d, want %d:\n%s", got, want, view)
	}
	if !strings.Contains(lines[len(lines)-1], "j/k move") {
		t.Fatalf("shortcut legend should be on bottom line:\n%s", view)
	}
	if got, want := dashboardTestLineIndex(lines, "PROJECT"), 3; got != want {
		t.Fatalf("task-set table header line = %d, want %d:\n%s", got, want, view)
	}
}

func TestDashboardTableClampsToBodyHeight(t *testing.T) {
	// Many rows on a short terminal must not overflow: the List scroll window
	// caps the body at the Frame's budget instead of rendering every row.
	rows := make([]DashboardRow, 40)
	for i := range rows {
		id := fmt.Sprintf("set-%02d", i)
		rows[i] = DashboardRow{Project: "pop", CursorKey: "pop\x00" + id, RawStatus: tasks.StatusReady, ID: id}
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 10})
	m = updated.(QueueDashboard)

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("view line count = %d, want %d (clamped to body height):\n%s", got, want, view)
	}
}

func TestDashboardTableFitsTerminalWidth(t *testing.T) {
	// Wide row content on a narrow pane must not spill horizontally. Auto-drain
	// and orphaned suffixes ride inside STATUS now that FLAGS is gone, so a
	// pane tight enough to shrink STATUS may truncate them — this test only
	// asserts the table never spills past termW; suffix visibility at
	// generous widths is covered by TestDashboardStatusSuffixesRender.
	row := DashboardRow{
		Project:       "very-long-project-name-here",
		VerifiedAtSHA: "abcdef123456",
		Worktree:      "feature/super-long-branch-name-for-testing",
		CursorKey:     "pop\x00set1",
		ID:            "set1",
		RawStatus:     tasks.StatusAwaitingApproval,
		AutoDrain:     true,
		Orphaned:      true,
		ConfigError:   "no trunk worktree configured",
	}
	for _, termW := range []int{40, 60, 80} {
		t.Run(fmt.Sprintf("width=%d", termW), func(t *testing.T) {
			m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
			updated, _ := m.Update(tea.WindowSizeMsg{Width: termW, Height: 20})
			m = updated.(QueueDashboard)
			view := m.View().Content
			for _, line := range dashboardTestTableLines(view) {
				if got := lipgloss.Width(line); got > termW {
					t.Fatalf("table line width %d exceeds terminal width %d:\n%q", got, termW, line)
				}
			}
		})
	}
}

func TestDashboardFitColumnWidths(t *testing.T) {
	natural := []int{20, 30, 40, 25, 1} // PROJECT, TASK SET, STATUS, WORKTREE, indicator
	fitted := workPage().fitWidths(testKinds(), natural, 50)
	if dashboardTableLineWidth(fitted) > 50 {
		t.Fatalf("fitted line width %d exceeds budget 50: %v", dashboardTableLineWidth(fitted), fitted)
	}
}

func dashboardTestTableLines(view string) []string {
	var lines []string
	inTable := false
	for _, line := range strings.Split(view, "\n") {
		singleHeader := strings.Contains(line, "PROJECT") && strings.Contains(line, "STATUS")
		twoLineHeader := strings.Contains(line, "TASK SET") && strings.Contains(line, "WORKTREE")
		if singleHeader || twoLineHeader {
			inTable = true
		}
		if !inTable {
			continue
		}
		if strings.Contains(line, "j/k move") || strings.Contains(line, "h/esc quit") {
			break
		}
		lines = append(lines, line)
	}
	return lines
}

func TestDashboardTwoLineMode(t *testing.T) {
	short := DashboardRow{ID: "short-id"}
	long := DashboardRow{ID: strings.Repeat("a", 37)}

	// roomy is a pane height at/above the floor; two-line decisions apply.
	const roomy = 20

	if !dashboardTwoLineMode([]DashboardRow{short}, 40, roomy) {
		t.Fatalf("narrow terminal (40 cols) should activate two-line mode")
	}
	if !dashboardTwoLineMode([]DashboardRow{short}, 119, roomy) {
		t.Fatalf("terminal just below threshold (119 cols) should activate two-line mode")
	}
	if dashboardTwoLineMode([]DashboardRow{short}, 120, roomy) {
		t.Fatalf("terminal at threshold (120 cols) with short ids should stay single-line")
	}
	if !dashboardTwoLineMode([]DashboardRow{short, long}, 120, roomy) {
		t.Fatalf("one long set id should activate two-line mode for all rows")
	}
	if !dashboardTwoLineMode([]DashboardRow{long, short}, 140, roomy) {
		t.Fatalf("long set id should activate two-line mode even on wide terminals")
	}
}

func TestDashboardTwoLineModeHeightGate(t *testing.T) {
	short := DashboardRow{ID: "short-id"}
	long := DashboardRow{ID: strings.Repeat("a", 37)}

	// Below the height floor, neither a narrow terminal nor a long set id may
	// activate two-line mode: a short popup stays single-line for row density.
	const short_pane = dashboardTwoLineHeightFloor - 1
	if dashboardTwoLineMode([]DashboardRow{short}, 40, short_pane) {
		t.Fatalf("narrow terminal below height floor should stay single-line")
	}
	if dashboardTwoLineMode([]DashboardRow{long, short}, 200, short_pane) {
		t.Fatalf("long set id below height floor should stay single-line")
	}

	// Exactly at the floor the pane is roomy again.
	if !dashboardTwoLineMode([]DashboardRow{short}, 40, dashboardTwoLineHeightFloor) {
		t.Fatalf("narrow terminal at the height floor should activate two-line mode")
	}
}

func TestDashboardTwoLineRowLine1ShowsIndicatorProjectSetIDWorktree(t *testing.T) {
	row := DashboardRow{
		Project:  "pop",
		Worktree: "main",
		ID:       "2026-07-05-queue-dashboard-two-line", RawStatus: tasks.StatusReady, LiveDrain: true,
		CursorKey: "pop\x00set",
	}
	widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths([]DashboardRow{row}), 120)
	line1 := dashboardTwoLineRowLine1(row, widths, livePaneCache{})

	// Line 1 carries PROJECT, TASK SET, WORKTREE and the trailing activity
	// cluster; STATUS lives on line 2.
	for _, want := range []string{dashboardActivityClusterPlain, "pop", row.ID, "main"} {
		if !strings.Contains(line1, want) {
			t.Fatalf("two-line row line 1 missing expected value %q: %q", want, line1)
		}
	}
	if strings.Contains(line1, "picked up") {
		t.Fatalf("two-line row line 1 must not carry the retired DRAIN string: %q", line1)
	}
	if strings.Contains(line1, "READY") {
		t.Fatalf("two-line row line 1 must not contain the status: %q", line1)
	}
}

func TestDashboardTwoLineRowLine2ShowsStatusUnderTaskSet(t *testing.T) {
	row := DashboardRow{
		Project: "pop",
		ID:      "2026-07-05-queue-dashboard-two-line", RawStatus: tasks.StatusReady,
		Started: true,
	}
	widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths([]DashboardRow{row}), 120)
	line2 := dashboardTwoLineRowLine2(testKinds(), row, widths)

	// The base label is bucket-colored (started READY → blue "IN PROGRESS").
	wantStatus := dashboardStatusCellStyled(testKinds(), row)
	if strings.TrimLeft(line2, " ") != wantStatus {
		t.Fatalf("two-line row line 2 = %q, want the status %q (indented)", line2, wantStatus)
	}
	// STATUS must be indented to start under the TASK SET column, i.e. past the
	// PROJECT column and its separator.
	wantIndent := dashboardTwoLineStatusIndent(widths)
	if got := len(line2) - len(strings.TrimLeft(line2, " ")); got != wantIndent {
		t.Fatalf("two-line row line 2 indent = %d, want %d (under TASK SET): %q", got, wantIndent, line2)
	}
	if strings.Contains(line2, row.ID) {
		t.Fatalf("two-line row line 2 must not contain the set id: %q", line2)
	}
}

func TestDashboardTwoLineRowsFitTerminalWidth(t *testing.T) {
	cases := []struct {
		termW int
		setID string
	}{
		// Narrow widths activate two-line mode regardless of set id length.
		{40, "set"},
		{60, "set"},
		// At the width threshold, a long set id is required to keep two-line
		// mode active; pick one that still fits within the 80-column budget.
		{80, "2026-07-05-queue-dashboard-two-line-mode"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("width=%d", tc.termW), func(t *testing.T) {
			row := DashboardRow{
				Project:  "pop",
				Worktree: "main",
				ID:       tc.setID, RawStatus: tasks.StatusReady,
				CursorKey: "pop\x00" + tc.setID,
			}
			m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
			updated, _ := m.Update(tea.WindowSizeMsg{Width: tc.termW, Height: 20})
			m = updated.(QueueDashboard)
			if !dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
				t.Fatalf("expected two-line mode at width %d", tc.termW)
			}
			view := m.View().Content
			for _, line := range dashboardTestTableLines(view) {
				if got := lipgloss.Width(line); got > tc.termW {
					t.Fatalf("table line width %d exceeds terminal width %d:\n%q", got, tc.termW, line)
				}
			}
		})
	}
}

func TestDashboardTwoLineSingleLineLayoutUnchanged(t *testing.T) {
	row := DashboardRow{
		Project:  "pop",
		Worktree: "main",
		ID:       "set1", RawStatus: tasks.StatusReady,
		CursorKey: "pop\x00set1",
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = updated.(QueueDashboard)
	if dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
		t.Fatalf("wide terminal with short ids should not activate two-line mode")
	}
	view := m.View().Content
	for _, want := range []string{"PROJECT  TASK SET  STATUS", "pop      set1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("single-line layout missing %q:\n%s", want, view)
		}
	}
}

// TestDashboardActivityCluster pins the trailing per-activity cluster (ADR-0158):
// every task-set row carries a fixed-width ivfS cluster; map rows carry I.
func TestDashboardActivityCluster(t *testing.T) {
	setID := "set-cluster"
	row := DashboardRow{ID: setID}
	mapRow := DashboardRow{Kind: ref.KindMap, ID: "map-1"}

	plain := dashboardActivityCluster(row, livePaneCache{}, false)
	if plain != dashboardActivityClusterPlain {
		t.Fatalf("plain cluster = %q, want %q", plain, dashboardActivityClusterPlain)
	}
	if got := dashboardActivityCluster(mapRow, livePaneCache{}, false); got != dashboardMapWayfinderKeyPlain {
		t.Fatalf("map cluster = %q, want %q", got, dashboardMapWayfinderKeyPlain)
	}

	live := livePaneCache{}
	live.set(tmuxmod.TagSet, setID, livePaneRunning)
	live.setWayfinder("map-1", livePaneRunning)
	styled := dashboardActivityCluster(row, live, true)
	if !strings.Contains(styled, livePaneRunningStyle.Render("I")) {
		t.Fatalf("styled cluster missing green drain key: %q", styled)
	}
	if w := lipgloss.Width(styled); w != len(dashboardActivityClusterPlain) {
		t.Fatalf("styled cluster width = %d, want %d", w, len(dashboardActivityClusterPlain))
	}
	styledMap := dashboardActivityCluster(mapRow, live, true)
	if !strings.Contains(styledMap, livePaneRunningStyle.Render("I")) {
		t.Fatalf("map cluster missing green wayfinder key: %q", styledMap)
	}
}

// TestDashboardSingleLineDropsDrainColumnKeepsCluster pins the retired DRAIN
// column and the trailing activity cluster on the single-line layout (ADR-0158):
// the header carries no DRAIN, the column order is PROJECT/TASK SET/STATUS/
// WORKTREE/cluster, and every task-set row carries ivfS.
func TestDashboardSingleLineDropsDrainColumnKeepsCluster(t *testing.T) {
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00live", ID: "live", RawStatus: tasks.StatusReady, LiveDrain: true},
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00idle", ID: "idle", RawStatus: tasks.StatusDone},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = updated.(QueueDashboard)
	if dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
		t.Fatalf("wide terminal with short ids should stay single-line")
	}
	view := m.View().Content
	if strings.Contains(view, "DRAIN") {
		t.Fatalf("single-line view must not carry the retired DRAIN column:\n%s", view)
	}
	// Column order: PROJECT, TASK SET, STATUS, WORKTREE — left to right, then the
	// blank-header activity cluster trails (no DRAIN anywhere).
	header := strings.Split(view, "\n")[dashboardTestLineIndex(strings.Split(view, "\n"), "PROJECT")]
	iProject := strings.Index(header, "PROJECT")
	iSet := strings.Index(header, "TASK SET")
	iStatus := strings.Index(header, "STATUS")
	iWorktree := strings.Index(header, "WORKTREE")
	if !(iProject >= 0 && iProject < iSet && iSet < iStatus && iStatus < iWorktree) {
		t.Fatalf("single-line header column order wrong: %q", header)
	}
	lines := strings.Split(view, "\n")
	liveIdx := dashboardTestLineIndex(lines, "live")
	if liveIdx < 0 {
		t.Fatalf("live row missing from view:\n%s", view)
	}
	if !strings.Contains(lines[liveIdx], dashboardActivityClusterPlain) {
		t.Fatalf("live row missing activity cluster: %q", lines[liveIdx])
	}
	doneIdx := dashboardTestLineIndex(lines, "idle")
	if doneIdx < 0 {
		t.Fatalf("idle row missing from view:\n%s", view)
	}
	if !strings.Contains(lines[doneIdx], dashboardActivityClusterPlain) {
		t.Fatalf("idle row missing activity cluster: %q", lines[doneIdx])
	}
}

// TestDashboardNarrowPaneKeepsCluster confirms the fixed-width activity cluster
// is never dropped by elastic width fitting even when the pane is very narrow
// (ADR-0158): ivfS still appears in every task-set row's rendered cells.
func TestDashboardNarrowPaneKeepsCluster(t *testing.T) {
	rows := []DashboardRow{
		{Project: "a-really-long-project-name", Worktree: "some-long-branch", CursorKey: "a\x00live",
			ID: "live", RawStatus: tasks.StatusReady, LiveDrain: true},
	}
	natural := workPage().columnWidths(testKinds(), rows)
	fitted := workPage().fitWidths(testKinds(), natural, 20)
	if fitted[dashboardColIndicator] < len(dashboardActivityClusterPlain) {
		t.Fatalf("indicator column width = %d, want >= %d (never dropped)", fitted[dashboardColIndicator], len(dashboardActivityClusterPlain))
	}
	line := dashboardTableLine(dashboardRowValues(testKinds(), rows[0], livePaneCache{}), fitted)
	if !strings.Contains(line, dashboardActivityClusterPlain) {
		t.Fatalf("narrow-pane row missing activity cluster: %q", line)
	}
}

func TestDashboardDetailViewOmitsTitleAndUsesBottomShortcutLegend(t *testing.T) {
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Title: "First", Type: "AFK", Status: "open"}},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-normal", RawStatus: tasks.StatusReady, ID: "set-normal"},
	}})
	m.width = 120
	m.height = 8
	d := newTaskDetailView(m.snap.Containers[0], manifest, nil)
	m.detail = d

	view := m.View().Content
	if strings.Contains(view, "Queue dashboard") {
		t.Fatalf("detail view should not render dashboard title:\n%s", view)
	}
	if !strings.Contains(view, "Task · set-normal") {
		t.Fatalf("detail view should render task prefix:\n%s", view)
	}
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("line count = %d, want %d:\n%s", got, want, view)
	}
	if !strings.Contains(lines[len(lines)-1], "a actions") {
		t.Fatalf("detail shortcut legend should be on bottom line:\n%s", view)
	}
	if got, want := dashboardTestLineIndex(lines, "STATUS"), 2; got != want {
		t.Fatalf("detail table header line = %d, want %d:\n%s", got, want, view)
	}
}

func TestDashboardDetailViewClampsToBodyHeight(t *testing.T) {
	// Many tasks on a short terminal must not overflow: the detail List scroll
	// window caps the body at the Frame's budget instead of rendering every task.
	manifestTasks := make([]tasks.Task, 40)
	for i := range manifestTasks {
		id := fmt.Sprintf("%02d-t", i)
		manifestTasks[i] = tasks.Task{ID: id, File: id + ".md", Title: "T", Type: "AFK", Status: "open"}
	}
	manifest := &tasks.Manifest{Valid: true, Tasks: manifestTasks}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-long", RawStatus: tasks.StatusReady, ID: "set-long"},
	}})
	m.width = 120
	m.height = 10
	d := newTaskDetailView(m.snap.Containers[0], manifest, nil)
	m.detail = d

	view := m.viewDetail()
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("detail view line count = %d, want %d (clamped to body height):\n%s", got, want, view)
	}
}

// TestDashboardStatusAppendsVerifiedAtSHA confirms the main table STATUS column
// appends the Verified-at SHA badge with three-state colouring.
func TestDashboardStatusAppendsVerifiedAtSHA(t *testing.T) {
	drifted := DashboardRow{
		VerifyMark:        tasks.VerifyMarkVerified,
		VerifiedAtSHA:     "abcdef1234567890",
		VerifiedAtDrifted: true,
		RawStatus:         tasks.StatusAwaitingApproval,
	}
	got := dashboardStatusCellStyled(testKinds(), drifted)
	if !strings.Contains(got, "AWAITING-APPROVAL") {
		t.Fatalf("status label missing: %q", got)
	}
	if !strings.Contains(got, "verified @ abcdef123456") {
		t.Fatalf("verified suffix missing: %q", got)
	}
	if !strings.Contains(got, "\x1b[33m") {
		t.Fatalf("drifted verified suffix should be yellow: %q", got)
	}

	atHead := DashboardRow{
		VerifyMark:        tasks.VerifyMarkVerified,
		VerifiedAtSHA:     "abcdef1234567890",
		VerifiedAtDrifted: false,
		RawStatus:         tasks.StatusDone,
	}
	gotHead := dashboardStatusCellStyled(testKinds(), atHead)
	if !strings.Contains(gotHead, "\x1b[32mverified @") {
		t.Fatalf("at-HEAD verified suffix should be green: %q", gotHead)
	}

	unverified := DashboardRow{RawStatus: tasks.StatusNeedsVerify}
	gotUnv := dashboardStatusCellStyled(testKinds(), unverified)
	if !strings.Contains(gotUnv, "\x1b[31munverified") {
		t.Fatalf("NEEDS-VERIFY should show red unverified: %q", gotUnv)
	}

	plain := DashboardRow{RawStatus: tasks.StatusAwaitingApproval}
	if got := dashboardStatusCellStyled(testKinds(), plain); strings.Contains(got, "verified @") || strings.Contains(got, "unverified") {
		t.Fatalf("plain status should not contain badge: %q", got)
	}
}

// TestDashboardStatusBucketColors confirms the base status label is colored by
// semantic bucket, and only the base token — the label carries the bucket ANSI
// while width measurement (dashboardStatusCell) stays plain.
func TestDashboardStatusBucketColors(t *testing.T) {
	cases := []struct {
		name    string
		status  tasks.TaskSetStatus
		started bool
		ansi    string // expected bucket color prefixing the base label
	}{
		{"DONE green", tasks.StatusDone, false, "\x1b[32m"},
		{"READY blue", tasks.StatusReady, false, "\x1b[34m"},
		{"IN PROGRESS blue", tasks.StatusReady, true, "\x1b[34m"},
		{"NEEDS-VERIFY yellow", tasks.StatusNeedsVerify, false, "\x1b[33m"},
		{"AWAITING-APPROVAL yellow", tasks.StatusAwaitingApproval, false, "\x1b[33m"},
		{"BLOCKED yellow", tasks.StatusBlocked, false, "\x1b[33m"},
		{"FAILED red", tasks.StatusFailed, false, "\x1b[31m"},
		{"VERIFY-FAILED red", tasks.StatusVerifyFailed, false, "\x1b[31m"},
		{"MALFORMED red", tasks.StatusMalformed, false, "\x1b[31m"},
		{"MISSING red", tasks.StatusMissing, false, "\x1b[31m"},
		{"DEFERRED faint", tasks.StatusDeferred, false, "\x1b[2m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := DashboardRow{RawStatus: c.status, Started: c.started}
			styled := dashboardStatusCellStyled(testKinds(), row)
			label := testKinds().statusSegments(row)[0].Text
			// The bucket ANSI must wrap the base label token.
			if !strings.Contains(styled, c.ansi+label) {
				t.Fatalf("styled label = %q, want bucket %q on %q", styled, c.ansi, label)
			}
			// Width measurement stays plain: no ANSI in the un-styled cell.
			if plain := dashboardStatusCellText(testKinds(), row); strings.Contains(plain, "\x1b[") {
				t.Fatalf("plain cell should carry no ANSI: %q", plain)
			}
		})
	}
}

// TestDashboardStatusBucketColorOnlyBaseToken confirms styling colors only the
// base label, leaving suffixes to their own styling: the auto-drain/orphaned
// suffixes stay plain and the label ANSI resets before them.
func TestDashboardStatusBucketColorOnlyBaseToken(t *testing.T) {
	row := DashboardRow{RawStatus: tasks.StatusBlocked, AutoDrain: true, Orphaned: true}
	styled := dashboardStatusCellStyled(testKinds(), row)
	// Base label is yellow and reset before the suffixes.
	if !strings.Contains(styled, "\x1b[33mBLOCKED\x1b[m") {
		t.Fatalf("base BLOCKED token should be yellow-and-reset: %q", styled)
	}
	// Suffixes carry no color of the base bucket.
	if !strings.Contains(styled, "· auto-drain") || !strings.Contains(styled, "· orphaned") {
		t.Fatalf("suffixes missing: %q", styled)
	}
	if strings.Contains(styled, "\x1b[33m· auto-drain") || strings.Contains(styled, "\x1b[33m· orphaned") {
		t.Fatalf("suffixes should not inherit base bucket color: %q", styled)
	}
}

// TestDashboardSummaryRunningCountsLiveDrainsOnly confirms the header "N running"
// tally counts live-drain rows only (ADR-0111): a parked or config-error row is
// not live-drained, so it no longer inflates the count as it did when the tally
// keyed on any non-blank DRAIN.
func TestDashboardSummaryRunningCountsLiveDrainsOnly(t *testing.T) {
	rows := []DashboardRow{
		{RawStatus: tasks.StatusReady, LiveDrain: true},
		{RawStatus: tasks.StatusReady, Parked: true},
		{RawStatus: tasks.StatusReady, ConfigError: "no trunk"},
	}
	summary := dashboardSummary(testKinds(), rows)
	if !strings.Contains(summary, "1 running") {
		t.Errorf("summary = %q, want exactly 1 running (live-drain rows only)", summary)
	}
}

// TestDashboardDetailHeaderIncludesVerifiedAtSHA confirms the detail view header
// includes the Verified-at SHA badge inside the status brackets when applicable —
// composed from the kind's STATUS segments, with the label itself left unpainted.
func TestDashboardDetailHeaderIncludesVerifiedAtSHA(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{})
	drifted := DashboardRow{
		ID: "demo", RawStatus: tasks.StatusAwaitingApproval,
		VerifyMark:        tasks.VerifyMarkVerified,
		VerifiedAtSHA:     "abcdef1234567890",
		VerifiedAtDrifted: true,
		Headline:          "1/1 done",
	}
	drifted.ID = "demo"
	header := m.detailHeader(drifted)
	if !strings.Contains(header, "Task · demo") {
		t.Fatalf("header missing set prefix: %q", header)
	}
	if !strings.Contains(header, "[AWAITING-APPROVAL") {
		t.Fatalf("header missing status bracket: %q", header)
	}
	if !strings.Contains(header, "verified @ abcdef123456") {
		t.Fatalf("header missing verified suffix: %q", header)
	}
	if !strings.Contains(header, "\x1b[33m") {
		t.Fatalf("drifted verified suffix should be yellow: %q", header)
	}
	if !strings.Contains(header, "1/1 done") {
		t.Fatalf("header missing progress headline: %q", header)
	}

	atHead := DashboardRow{ID: "demo", RawStatus: tasks.StatusDone, VerifyMark: tasks.VerifyMarkVerified, VerifiedAtSHA: "abcdef1234567890"}
	atHead.ID = "demo"
	if headHeader := m.detailHeader(atHead); !strings.Contains(headHeader, "\x1b[32mverified @") {
		t.Fatalf("at-HEAD header should be green: %q", headHeader)
	}

	plain := DashboardRow{ID: "demo", RawStatus: tasks.StatusAwaitingApproval}
	plain.ID = "demo"
	if got := m.detailHeader(plain); strings.Contains(got, "verified @") {
		t.Fatalf("plain header should not contain suffix: %q", got)
	}
}

// TestDashboardTableRendersVerifiedAtSHA confirms a row produced through the
// dashboard status pipeline renders the suffix in the STATUS column.
func TestDashboardTableRendersVerifiedAtSHA(t *testing.T) {
	row := DashboardRow{
		Project:           "pop",
		VerifyMark:        tasks.VerifyMarkVerified,
		VerifiedAtSHA:     "abcdef1234567890",
		VerifiedAtDrifted: true,
		Worktree:          "main",
		CursorKey:         "pop\x00set",
		ID:                "set", RawStatus: tasks.StatusDone,
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 8})
	m = updated.(QueueDashboard)
	view := m.View().Content
	if !strings.Contains(view, "DONE") {
		t.Fatalf("view missing DONE status:\n%s", view)
	}
	if !strings.Contains(view, "verified @ abcdef123456") {
		t.Fatalf("view missing verified suffix:\n%s", view)
	}
}

// TestDashboardBindModalListStagesNavigateAndSelect drives the bind modal's two
// list stages through the List: j moves the cursor (wrapping), and Enter on the
// base-ref stage records the highlighted ref and advances to the name stage.
func TestDashboardBindModalListStagesNavigateAndSelect(t *testing.T) {
	row := DashboardRow{Project: "pop", ID: "set-bind"}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})

	// Worktree stage: entries arrive; wrap up from the top lands on the last.
	updated, _ := m.Update(dashboardBindListMsg{row: row, entries: []drain.BindEntry{
		{Label: "wt-a", Path: "/a"},
		{Label: "wt-b", Path: "/b"},
		{Create: true, Label: "create new..."},
	}})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m = updated.(QueueDashboard)
	sel, ok := m.bind.list.Selected()
	if !ok || !sel.Create {
		t.Fatalf("k wrap in worktree stage selected %+v (ok=%v), want the create entry", sel, ok)
	}

	// Base-ref stage: refs arrive; j moves to the second ref, Enter records it.
	updated, _ = m.Update(dashboardBindRefsMsg{refs: []string{"main", "develop"}})
	m = updated.(QueueDashboard)
	if m.bind.stage != dashboardBindStageBaseRef {
		t.Fatalf("stage = %d, want base-ref", m.bind.stage)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(QueueDashboard)
	if m.bind.stage != dashboardBindStageName {
		t.Fatalf("stage = %d, want name after base-ref select", m.bind.stage)
	}
	if m.bind.baseRef != "develop" {
		t.Fatalf("baseRef = %q, want develop", m.bind.baseRef)
	}
}

// TestDashboardBindModalNameStageTypesEveryLetter pins the rule the name stage
// is built to (ADR-0213): once the modal is taking a worktree name, the list
// navigation keys are plain characters, so a name holding j or k can be typed.
// The stage also carries the house field's editing (ADR-0081), so ctrl+u clears
// and the word motions work.
func TestDashboardBindModalNameStageTypesEveryLetter(t *testing.T) {
	row := DashboardRow{Project: "pop", ID: "set-bind"}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.bind = &dashboardBindModal{row: row, stage: dashboardBindStageName, baseRef: "main", name: ui.NewTextField()}

	typeName := func(m QueueDashboard, s string) QueueDashboard {
		for _, ch := range s {
			updated, _ := m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
			m = updated.(QueueDashboard)
		}
		return m
	}

	m = typeName(m, "jk-hotfix")
	if got := m.bind.name.Value(); got != "jk-hotfix" {
		t.Fatalf("name = %q, want every key typed", got)
	}

	// ctrl+u clears the buffer; the word motions then edit what is retyped.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = updated.(QueueDashboard)
	if got := m.bind.name.Value(); got != "" {
		t.Fatalf("name = %q after ctrl+u, want cleared", got)
	}
	m = typeName(m, "kj fix")
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	m = updated.(QueueDashboard)
	if got := m.bind.name.Value(); got != "kj " {
		t.Fatalf("name = %q after ctrl+w, want the last word deleted", got)
	}
}

func TestDashboardBindModalClampsToBodyHeight(t *testing.T) {
	// A long worktree list on a short terminal must not overflow: the modal's
	// List scroll window caps its body instead of rendering every entry.
	entries := make([]drain.BindEntry, 40)
	for i := range entries {
		entries[i] = drain.BindEntry{Label: fmt.Sprintf("wt-%02d", i)}
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-bind", RawStatus: tasks.StatusReady, ID: "set-bind"},
	}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m = updated.(QueueDashboard)
	m.bind = &dashboardBindModal{row: m.snap.Containers[0], list: newBindEntryList(entries)}

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("bind modal view line count = %d, want %d (clamped to body height):\n%s", got, want, view)
	}
}

func TestDashboardDrainModalClampsToBodyHeight(t *testing.T) {
	// A long drain-target list on a short terminal must not overflow: the modal's
	// List scroll window caps its body instead of rendering every entry.
	entries := make([]drain.DrainEntry, 40)
	for i := range entries {
		entries[i] = drain.DrainEntry{Label: fmt.Sprintf("target-%02d", i)}
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-drain", RawStatus: tasks.StatusReady, ID: "set-drain"},
	}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m = updated.(QueueDashboard)
	m.drainPick = newDashboardDrainModal(m.d, m.snap.Containers[0], entries, "")

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("drain modal view line count = %d, want %d (clamped to body height):\n%s", got, want, view)
	}
}

func dashboardTestLineIndex(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

func TestDashboardQAndSAreUnbound(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set", RawStatus: tasks.StatusReady, ID: "set"},
	}})
	got := m
	for _, key := range []string{"q", "s"} {
		updated, cmd := got.Update(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
		got = updated.(QueueDashboard)
		if cmd != nil {
			t.Fatalf("%s at top level returned command, want no-op", key)
		}
		if got.list.Cursor() != 0 || got.detail != nil {
			t.Fatalf("%s changed model: cursor=%d detail=%+v", key, got.list.Cursor(), got.detail)
		}
	}

	got.detail = newDetailView(got.snap.Containers[0])
	for _, key := range []string{"q", "s"} {
		updated, cmd := got.Update(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
		got = updated.(QueueDashboard)
		if cmd != nil {
			t.Fatalf("%s in detail returned command, want no-op", key)
		}
		if got.detail == nil {
			t.Fatalf("%s in detail closed detail view", key)
		}
	}
}

func TestDashboardDetailViewPeekTaskText(t *testing.T) {
	taskPath := filepath.Join("/tasks", "set-peek", "01-a.md")
	d := &drain.Deps{Tasks: &tasks.Deps{FS: &deps.MockFileSystem{
		ReadFileFunc: func(path string) ([]byte, error) {
			if path != taskPath {
				t.Fatalf("read path = %q, want %q", path, taskPath)
			}
			return []byte("# Task A\n\nFull task body.\n"), nil
		},
	}}}
	manifest := &tasks.Manifest{
		Dir:   filepath.Join("/tasks", "set-peek"),
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"}},
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-peek", RawStatus: tasks.StatusReady, ID: "set-peek"},
	}})
	d0 := newTaskDetailView(m.snap.Containers[0], manifest, nil)
	m.detail = d0

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)
	if got.detail.peek == nil || !got.detail.peek.loading || got.detail.peek.itemID != "01-a" {
		t.Fatalf("peek = %+v, want loading peek for 01-a", got.detail.peek)
	}
	if cmd == nil {
		t.Fatalf("l in detail did not return task-text loading command")
	}
	msg := cmd()
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if got.detail.peek == nil || got.detail.peek.loading || got.detail.peek.err != nil {
		t.Fatalf("loaded peek = %+v, want loaded text", got.detail.peek)
	}
	view := got.View().Content
	for _, want := range []string{"set-peek / 01-a", taskPath, "# Task A", "Full task body."} {
		if !strings.Contains(view, want) {
			t.Fatalf("peek view missing %q:\n%s", want, view)
		}
	}

	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	got = updated.(QueueDashboard)
	if cmd != nil {
		t.Fatalf("h from task-text peek returned command")
	}
	if got.detail == nil || got.detail.peek != nil {
		t.Fatalf("h should close peek but keep detail: detail=%+v", got.detail)
	}

	m2 := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-peek", RawStatus: tasks.StatusReady, ID: "set-peek"},
	}})
	d2 := newTaskDetailView(m2.snap.Containers[0], manifest, nil)
	m2.detail = d2
	updated, cmd = m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = updated.(QueueDashboard)
	if got.detail.peek == nil || !got.detail.peek.loading || got.detail.peek.itemID != "01-a" {
		t.Fatalf("enter peek = %+v, want loading peek for 01-a", got.detail.peek)
	}
	if cmd == nil {
		t.Fatalf("enter in detail did not return task-text loading command")
	}
}

func TestDashboardTaskTextPeekScrolls(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-scroll", RawStatus: tasks.StatusReady, ID: "set-scroll"},
	}})
	m.height = 8
	m.width = 80
	m.detail = &detailView{
		row: m.snap.Containers[0],
		peek: &itemTextPeek{
			itemID: "01-a",
			path:   filepath.Join("/tasks", "set-scroll", "01-a.md"),
			text:   "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\n",
		},
	}

	view := m.View().Content
	for _, want := range []string{"line 1", "line 2", "line 3"} {
		if !strings.Contains(view, want) {
			t.Fatalf("initial peek missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "line 4") {
		t.Fatalf("initial peek should clip line 4:\n%s", view)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got := updated.(QueueDashboard)
	if got.detail.peek.scroll != 1 {
		t.Fatalf("after j scroll = %d, want 1", got.detail.peek.scroll)
	}
	view = got.View().Content
	if strings.Contains(view, "line 1") || !strings.Contains(view, "line 4") {
		t.Fatalf("after j view should show lines 2-4:\n%s", view)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	got = updated.(QueueDashboard)
	if got.detail.peek.scroll != 3 {
		t.Fatalf("after G scroll = %d, want 3", got.detail.peek.scroll)
	}
	view = got.View().Content
	if !strings.Contains(view, "line 6") || strings.Contains(view, "line 1") {
		t.Fatalf("after G view should show bottom lines:\n%s", view)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if got.detail.peek.scroll != 3 {
		t.Fatalf("first g scroll = %d, want 3", got.detail.peek.scroll)
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if got.detail.peek.scroll != 0 {
		t.Fatalf("after gg scroll = %d, want 0", got.detail.peek.scroll)
	}
}

func TestDashboardTopLevelVimNavigation(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00first", RawStatus: tasks.StatusReady, ID: "first"},
		{Project: "pop", CursorKey: "pop\x00second", RawStatus: tasks.StatusReady, ID: "second"},
		{Project: "pop", CursorKey: "pop\x00third", RawStatus: tasks.StatusReady, ID: "third"},
	}})

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	got := updated.(QueueDashboard)
	if got.list.Cursor() != 2 {
		t.Fatalf("G cursor = %d, want 2", got.list.Cursor())
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if got.list.Cursor() != 2 {
		t.Fatalf("first g cursor = %d, want 2", got.list.Cursor())
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if got.list.Cursor() != 0 {
		t.Fatalf("gg cursor = %d, want 0", got.list.Cursor())
	}

	_, cmd := got.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if cmd == nil {
		t.Fatalf("h should quit from top level")
	}
}

func TestDashboardReloadPreservesCursorByKey(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00a", RawStatus: tasks.StatusReady, ID: "a"},
		{Project: "pop", CursorKey: "pop\x00b", RawStatus: tasks.StatusReady, ID: "b"},
		{Project: "pop", CursorKey: "pop\x00c", RawStatus: tasks.StatusReady, ID: "c"},
	}})
	m.list.SetCursor(2) // on "c"

	// A tick reload delivers the same sets reordered; the cursor must follow "c".
	reordered := []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00c", RawStatus: tasks.StatusReady, ID: "c"},
		{Project: "pop", CursorKey: "pop\x00a", RawStatus: tasks.StatusReady, ID: "a"},
		{Project: "pop", CursorKey: "pop\x00b", RawStatus: tasks.StatusReady, ID: "b"},
	}
	updated, _ := m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: reordered}})
	got := updated.(QueueDashboard)
	if sel, ok := got.list.Selected(); !ok || sel.ID != "c" {
		t.Fatalf("cursor after reload = %+v (ok=%v), want set c", sel, ok)
	}
}

func TestDashboardDetailViewRendersTaskList(t *testing.T) {
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{
			{ID: "01-a", File: "01-a.md", Title: "First", Type: "AFK", Status: "done"},
			{ID: "02-b", File: "02-b.md", Title: "Second", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
		},
	}
	taskRow := &tasks.Row{ID: "set-normal", Status: tasks.StatusReady, Progress: "1/2 done, 1 open"}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-normal", RawStatus: tasks.StatusReady, ID: "set-normal"},
	}})
	m.width = 120
	m.height = 20
	d := newTaskDetailView(m.snap.Containers[0], manifest, taskRow)
	m.detail = d
	out := m.viewDetail()

	for _, want := range []string{"set-normal  [READY]  1/2 done, 1 open", "STATUS", "01-a", "02-b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered detail missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "01-a") > strings.Index(out, "02-b") {
		t.Fatalf("tasks out of manifest order:\n%s", out)
	}
	// Cursor indicator on first task.
	if !strings.Contains(out, "█") {
		t.Fatalf("expected cursor indicator:\n%s", out)
	}
}

// TestDashboardDetailViewKeepsRetryCountInStatusCell pins the one place a task's
// status cell says more than its status word: a failed task folds its retry
// count into the label the detail renders.
func TestDashboardDetailViewKeepsRetryCountInStatusCell(t *testing.T) {
	after := 3
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Title: "First", Type: "AFK", Status: tasks.TaskFailed, FailedAfter: &after}},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-failed", RawStatus: tasks.StatusReady, ID: "set-failed"},
	}})
	m.width, m.height = 120, 20
	m.detail = newTaskDetailView(m.snap.Containers[0], manifest, nil)

	if out := m.viewDetail(); !strings.Contains(out, "failed(3)") {
		t.Fatalf("detail should fold the retry count into the status cell:\n%s", out)
	}
}

func TestDashboardDetailViewCursorByIDPinsAcrossRefresh(t *testing.T) {
	manifest1 := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-b", Type: "AFK", Status: "open"},
		},
	}
	// Same tasks, different order (simulates a refresh that reorders).
	manifest2 := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{
			{ID: "02-b", Type: "AFK", Status: "done"}, // promoted
			{ID: "01-a", Type: "AFK", Status: "done"},
		},
	}

	d := newTaskDetailView(DashboardRow{ID: "set-x"}, manifest1, nil)
	d.list.SetCursorToKey("02-b")

	// Cursor is on 02-b at index 1 before refresh.
	if sel, ok := d.list.Selected(); !ok || sel.ID != "02-b" || d.list.Cursor() != 1 {
		t.Fatalf("before refresh selected = %+v (ok=%v) cursor=%d, want 02-b at index 1", sel, ok, d.list.Cursor())
	}

	// After a refresh that reorders, the cursor follows 02-b to its new index.
	d.sync(detailRowWithTasks(d.row, manifest2, nil))
	if sel, ok := d.list.Selected(); !ok || sel.ID != "02-b" || d.list.Cursor() != 0 {
		t.Fatalf("after refresh selected = %+v (ok=%v) cursor=%d, want 02-b at index 0", sel, ok, d.list.Cursor())
	}
}

func TestDashboardDetailViewVimNavigation(t *testing.T) {
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-b", Type: "AFK", Status: "open"},
			{ID: "03-c", Type: "AFK", Status: "open"},
		},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-nav", RawStatus: tasks.StatusReady, ID: "set-nav"},
	}})
	d := newTaskDetailView(m.snap.Containers[0], manifest, nil)
	m.detail = d

	selID := func(m QueueDashboard) string {
		task, _ := m.detail.list.Selected()
		return task.ID
	}

	// j moves cursor down.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got := updated.(QueueDashboard)
	if id := selID(got); id != "02-b" {
		t.Fatalf("after j: selected = %q, want 02-b", id)
	}

	// k moves cursor up.
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "01-a" {
		t.Fatalf("after k: selected = %q, want 01-a", id)
	}

	// j/k clamp at boundaries.
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "01-a" {
		t.Fatalf("k at top should clamp: selected = %q, want 01-a", id)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "03-c" {
		t.Fatalf("G should move to bottom: selected = %q, want 03-c", id)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "03-c" {
		t.Fatalf("first g should not move cursor: selected = %q, want 03-c", id)
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "01-a" {
		t.Fatalf("gg should move to top: selected = %q, want 01-a", id)
	}
}

// menuHasKey reports whether the action menu offers a verb bound to key.
func menuHasKey(menu *dashboardMenu, key string) bool {
	if menu == nil {
		return false
	}
	for _, item := range menu.list.Items() {
		if item.key == key {
			return true
		}
	}
	return false
}

func TestDashboardLaunchDrainRoutesPlainTrunkWorktreeAndRecordsPane(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "plain-drain", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)

	result, err := drain.LaunchDrain(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.LaunchDrain: %v", err)
	}
	if queuetest.Canon(t, d.Tasks, result.RuntimePath) != queuetest.Canon(t, d.Tasks, repo) {
		t.Fatalf("runtime = %q, want execution base %q", result.RuntimePath, repo)
	}
	cmd, ok := queuetest.ExtractSpawnCommand(rt)
	if !ok {
		t.Fatal("expected drain spawn command")
	}
	if !strings.Contains(cmd, "pop tasks implement "+setID) || !strings.Contains(cmd, "--task-runtime-path "+result.RuntimePath) {
		t.Fatalf("spawn command = %q, want implement for %s pinned to trunk %q", cmd, setID, result.RuntimePath)
	}
	assertDashboardPaneMapping(t, d, repo, setID, "%3", "dashboard")
}

func TestDashboardLaunchDrainRoutesBoundCheckout(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "bound-drain", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "bound")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "bound", Project: "pop", Provisioned: false},
	})

	result, err := drain.LaunchDrain(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.LaunchDrain: %v", err)
	}
	if result.RuntimePath != bound {
		t.Fatalf("runtime = %q, want bound checkout %q", result.RuntimePath, bound)
	}
	newWindow, ok := rt.FindCommand("new-window")
	if !ok || !queuetest.ArgsContain(newWindow, "-c", bound) {
		t.Fatalf("new-window = %v, want cwd %q", newWindow, bound)
	}
	cmd, ok := queuetest.ExtractSpawnCommand(rt)
	if !ok || !strings.Contains(cmd, "--task-runtime-path "+bound) {
		t.Fatalf("spawn command = %q, want drain pinned to bound checkout %q", cmd, bound)
	}
	assertDashboardPaneMapping(t, d, repo, setID, "%3", "dashboard")
}

// TestDashboardLaunchDrainUnboundUsesRepresentativeCheckout asserts the
// dashboard launch-drain action no longer auto-provisions a managed worktree for
// an unbound worktree-ready set: routing collapsed, so the drain lands on the
// representative checkout (the repo) and records no binding (ADR-0052). Explicit
// provisioning returns later via the Drain target picker.
func TestDashboardLaunchDrainUnboundUsesRepresentativeCheckout(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "managed-drain", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)

	result, err := drain.LaunchDrain(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.LaunchDrain: %v", err)
	}
	wantRepo, _ := filepath.EvalSymlinks(repo)
	gotRuntime, _ := filepath.EvalSymlinks(result.RuntimePath)
	if gotRuntime != wantRepo || strings.Contains(result.RuntimePath, filepath.Join("pop", "work", "worktrees")) {
		t.Fatalf("runtime = %q, want representative checkout %q with no provisioned worktree", result.RuntimePath, repo)
	}
	cmd, ok := queuetest.ExtractSpawnCommand(rt)
	if !ok || !strings.Contains(cmd, "pop tasks implement "+setID) || !strings.Contains(cmd, "--task-runtime-path "+result.RuntimePath) {
		t.Fatalf("spawn command = %q, want implement for %s pinned to representative checkout %q", cmd, setID, result.RuntimePath)
	}
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	bindings := queuetest.LoadBindingStore(t, d.Tasks)
	if b, ok := bindings[drain.SetScopedKey(repoKey, setID)]; ok {
		t.Fatalf("unexpected binding for unbound dashboard drain: %+v", b)
	}
	assertDashboardPaneMapping(t, d, repo, setID, "%3", "dashboard")
}

// TestDashboardShowsUnsatisfiableWorktreeDirective asserts the Work dashboard
// shows a set whose `name` worktree directive names a worktree absent on this
// machine as a config error on the set's row (ADR-0059), read-only — no drain, no
// provisioning.
func TestDashboardShowsUnsatisfiableWorktreeDirective(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "named-directive", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, _, _ := dashboardLaunchFixture(t, repo, setID)

	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, repo)
	if err != nil {
		t.Fatal(err)
	}
	canonDef, err := tasks.CanonicalDefinitionPathWith(d.Tasks, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.UpdateGlobalStateWith(d.Tasks, tasks.StatePathFor(canonDef), func(s *tasks.GlobalState) error {
		entry := s.Tasks[canonDef]
		for i := range entry.TaskSets {
			if entry.TaskSets[i].ID == setID {
				entry.TaskSets[i].WorktreeIntent = &tasks.WorktreeDirective{Name: "absent"}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	snap, err := work.BuildSnapshot(d.WorkKinds(cfg))
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	var row *DashboardRow
	for i := range snap.Containers {
		if snap.Containers[i].ID == setID {
			row = &snap.Containers[i]
		}
	}
	if row == nil {
		t.Fatalf("set %s not in dashboard rows: %+v", setID, snap.Containers)
	}
	if row.LiveDrain {
		t.Fatalf("LiveDrain = true, want false (config error row is not a live drain)")
	}
	status := dashboardStatusCellText(testKinds(), *row)
	if !strings.Contains(status, "· config error:") || !strings.Contains(status, "no worktree of that name") {
		t.Fatalf("status = %q, want a config error suffix for the unsatisfiable named directive", status)
	}
}

func TestDashboardBindPickerListsAndAdoptsExistingWorktree(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "bind-existing", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	wt1 := filepath.Join(t.TempDir(), "existing-one")
	wt2 := filepath.Join(t.TempDir(), "existing-two")
	runGit(t, repo, "worktree", "add", "-b", "existing-one", wt1, "HEAD")
	runGit(t, repo, "worktree", "add", "-b", "existing-two", wt2, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)

	entries, err := drain.BindWorktreeEntries(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.BindWorktreeEntries: %v", err)
	}
	if len(entries) < 3 || !entries[len(entries)-1].Create {
		t.Fatalf("entries = %+v, want existing worktrees plus create entry", entries)
	}
	var sawWT1 bool
	for _, entry := range entries {
		if entry.Path != "" && queuetest.Canon(t, d.Tasks, entry.Path) == queuetest.Canon(t, d.Tasks, wt1) && entry.Branch == "existing-one" {
			sawWT1 = true
		}
	}
	if !sawWT1 {
		t.Fatalf("entries = %+v, want %s on branch existing-one", entries, wt1)
	}

	got, err := drain.AdoptWorktree(d, cfg, row, wt1)
	if err != nil {
		t.Fatalf("drain.AdoptWorktree: %v", err)
	}
	if got.RuntimePath != wt1 || got.Branch != "existing-one" {
		t.Fatalf("adopt result = %+v, want %s existing-one", got, wt1)
	}

	repointed, err := drain.AdoptWorktree(d, cfg, row, wt2)
	if err != nil {
		t.Fatalf("idle re-point should not require force prompt: %v", err)
	}
	if !repointed.Replaced || repointed.RuntimePath != wt2 {
		t.Fatalf("repoint result = %+v, want replaced binding to %s", repointed, wt2)
	}
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	bindings := queuetest.LoadBindingStore(t, d.Tasks)
	binding := bindings[drain.SetScopedKey(repoKey, setID)]
	if binding.RuntimePath != wt2 || binding.Provisioned {
		t.Fatalf("binding = %+v, want adopted %s", binding, wt2)
	}
	snap, err := work.BuildSnapshot(d.WorkKinds(cfg))
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snap.Containers) == 0 || snap.Containers[0].Worktree != "existing-two" {
		t.Fatalf("dashboard rows = %+v, want worktree column updated", snap.Containers)
	}
}

func dashboardManagedIntent(t *testing.T, d *drain.Deps, repo, setID string) *tasks.WorktreeDirective {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, repo)
	if err != nil {
		t.Fatal(err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(d.Tasks, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := tasks.RegisteredWorktreeIntent(d.Tasks, defPath, setID)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

// TestDashboardBindPickerManagedEntryRecordsIntent covers the bind picker managed
// entry: picking it on an unbound set provisions a managed worktree eagerly
// (ADR-0147); on a bound adopted set it re-points without a second prompt,
// dropping the old binding forget-only before provisioning the new checkout.
func TestDashboardBindPickerManagedEntryRecordsIntent(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "bind-managed", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := drain.BindWorktreeEntries(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.BindWorktreeEntries: %v", err)
	}
	if n := len(entries); n < 3 || !entries[n-2].Managed || !entries[n-1].Create {
		t.Fatalf("entries = %+v, want managed entry before the create entry", entries)
	}

	got, err := drain.BindManagedWorktree(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.BindManagedWorktree on unbound set: %v", err)
	}
	b, ok := queuetest.LoadBindingStore(t, d.Tasks)[drain.SetScopedKey(repoKey, setID)]
	if !ok || !b.Provisioned {
		t.Fatalf("binding = ok=%v %+v, want provisioned managed binding", ok, b)
	}
	if got.RuntimePath == "" || got.RuntimePath != b.RuntimePath {
		t.Fatalf("result runtime %q binding %q, want matching provisioned checkout", got.RuntimePath, b.RuntimePath)
	}
	if intent := dashboardManagedIntent(t, d, repo, setID); intent != nil {
		t.Fatalf("intent = %+v, want no managed intent after eager provision", intent)
	}

	wt := filepath.Join(t.TempDir(), "adopted")
	runGit(t, repo, "worktree", "add", "-b", "adopted-branch", wt, "HEAD")
	setID2 := "bind-managed-repoint"
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	setDir := filepath.Join(id.TasksDir, setID2)
	queuetest.WriteSpawnTaskMD(t, setDir, "01-b.md")
	queuetest.WriteSpawnManifest(t, setDir, []queuetest.SpawnTask{
		{ID: "01-b", File: "01-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	statePath := tasks.StatePathFor(id.TasksDir)
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), id.TasksDir, statePath); err != nil {
		t.Fatal(err)
	}
	d2, cfg2, row2, _ := dashboardLaunchFixture(t, repo, setID2)
	if _, err := drain.AdoptWorktree(d2, cfg2, row2, wt); err != nil {
		t.Fatalf("drain.AdoptWorktree: %v", err)
	}
	got, err = drain.BindManagedWorktree(d2, cfg2, row2)
	if err != nil {
		t.Fatalf("drain.BindManagedWorktree on bound set should re-point without prompt: %v", err)
	}
	repoKey2, err := drain.ResolveRepoKey(d2, repo)
	if err != nil {
		t.Fatal(err)
	}
	b, ok = queuetest.LoadBindingStore(t, d2.Tasks)[drain.SetScopedKey(repoKey2, setID2)]
	if !ok || !b.Provisioned {
		t.Fatalf("binding after re-point = ok=%v %+v, want new provisioned binding", ok, b)
	}
	if b.RuntimePath == wt {
		t.Fatalf("binding still at adopted checkout %q, want new managed worktree", wt)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("old checkout %s should stay on disk: %v", wt, err)
	}
	if intent := dashboardManagedIntent(t, d2, repo, setID2); intent != nil {
		t.Fatalf("intent after re-point = %+v, want no managed intent", intent)
	}
}

// TestDashboardBindManagedRefusesLiveLock covers acceptance criterion 4: the
// managed bind refuses while the set's own drain holds the runtime lock, and
// the existing binding survives untouched.
func TestDashboardBindManagedRefusesLiveLock(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "bind-managed-locked", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	locked := filepath.Join(t.TempDir(), "locked")
	runGit(t, repo, "worktree", "add", "-b", "locked-branch", locked, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = locked
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: locked, Branch: "locked-branch", Project: "pop", Provisioned: false},
	})
	d.ReadLock = func(runtimePath string) *tasks.RuntimeLockStatus {
		if runtimePath == locked {
			lock := queuetest.LiveLock(runtimePath)
			lock.Metadata.SetID = setID
			return lock
		}
		return queuetest.IdleLock(runtimePath)
	}

	_, err = drain.BindManagedWorktree(d, cfg, row)
	if err == nil || !strings.Contains(err.Error(), "currently executing") {
		t.Fatalf("drain.BindManagedWorktree err = %v, want live-lock refusal", err)
	}
	if got := queuetest.LoadBindingStore(t, d.Tasks)[drain.SetScopedKey(repoKey, setID)].RuntimePath; got != locked {
		t.Fatalf("binding runtime = %q, want unchanged %q", got, locked)
	}
}

// staticGitGuard wraps a real git and fails the test if the dashboard build ever
// forks one of the static-path commands — identity (`rev-parse`), integration
// target (`worktree list`), or branch (`branch --show-current`). Those facts are
// derived fork-free from the repo.json marker and config (ADR-0060); any such
// fork is a regression. Other commands (e.g. mergeability in reconcile) pass
// through to the real git so the build still completes.
type staticGitGuard struct {
	t     *testing.T
	inner deps.Git
}

func (g *staticGitGuard) check(dir string, args []string) {
	if len(args) == 0 {
		return
	}
	static := args[0] == "rev-parse" ||
		args[0] == "branch" ||
		(args[0] == "worktree" && len(args) > 1 && args[1] == "list")
	if static {
		g.t.Errorf("dashboard build forked git on the static path: git %s (dir %q)", strings.Join(args, " "), dir)
	}
}

func (g *staticGitGuard) Command(args ...string) (string, error) {
	g.check("", args)
	return g.inner.Command(args...)
}

func (g *staticGitGuard) CommandInDir(dir string, args ...string) (string, error) {
	g.check(dir, args)
	return g.inner.CommandInDir(dir, args...)
}

// TestDashboardBuildForksNoStaticGit is the ADR-0060 guard: a dashboard build
// resolves identity, integration target, and branch from the marker + config
// with zero git. The guard git fails the test if any static-path command is
// forked, yet the build still yields rows whose branch column is populated —
// proving the branch came from the integration target's HEAD file, not a fork.
func TestDashboardBuildForksNoStaticGit(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "fork-free", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, _, _ := dashboardLaunchFixture(t, repo, setID)
	guard := &staticGitGuard{t: t, inner: d.Tasks.Git}
	d.Tasks.Git = guard
	d.Project.Git = guard

	snap, err := work.BuildSnapshot(d.WorkKinds(cfg))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(snap.Containers) == 0 {
		t.Fatalf("build produced no rows; fixture did not exercise resolution")
	}
	if strings.TrimSpace(snap.Containers[0].Worktree) == "" {
		t.Fatalf("branch/worktree column empty; HEAD-file branch resolution failed: %+v", snap.Containers[0])
	}
}

func TestCreateWorktreeManagedFreshBranchNoSession(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "bind-create", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)

	refs, err := drain.BindBaseRefs(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.BindBaseRefs: %v", err)
	}
	if len(refs) == 0 || refs[0] != "main" {
		t.Fatalf("refs = %v, want main first", refs)
	}

	got, err := drain.CreateWorktree(d, cfg, row, "main", "fresh-dashboard-branch")
	if err != nil {
		t.Fatalf("drain.CreateWorktree: %v", err)
	}
	if got.Branch != "fresh-dashboard-branch" || got.BaseRef != "main" {
		t.Fatalf("create result = %+v", got)
	}
	if len(rt.Commands) != 0 {
		t.Fatalf("create-new must not spawn or switch tmux sessions, got %v", rt.Commands)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "fresh-dashboard-branch"); strings.TrimSpace(branch) == "" {
		t.Fatalf("fresh branch was not created")
	}
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	bindings := queuetest.LoadBindingStore(t, d.Tasks)
	binding := bindings[drain.SetScopedKey(repoKey, setID)]
	if binding.RuntimePath != got.RuntimePath || binding.Branch != "fresh-dashboard-branch" || !binding.Provisioned {
		t.Fatalf("binding = %+v, want managed fresh branch at %s", binding, got.RuntimePath)
	}
}

func TestDashboardBindRefusesLiveLock(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "bind-locked", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	locked := filepath.Join(t.TempDir(), "locked")
	target := filepath.Join(t.TempDir(), "target")
	runGit(t, repo, "worktree", "add", "-b", "locked-branch", locked, "HEAD")
	runGit(t, repo, "worktree", "add", "-b", "target-branch", target, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = locked
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: locked, Branch: "locked-branch", Project: "pop", Provisioned: false},
	})
	d.ReadLock = func(runtimePath string) *tasks.RuntimeLockStatus {
		if runtimePath == locked {
			lock := queuetest.LiveLock(runtimePath)
			lock.Metadata.SetID = setID
			return lock
		}
		return queuetest.IdleLock(runtimePath)
	}

	_, err = drain.AdoptWorktree(d, cfg, row, target)
	if err == nil || !strings.Contains(err.Error(), "currently executing") {
		t.Fatalf("drain.AdoptWorktree err = %v, want live-lock refusal", err)
	}
	afterBindings := queuetest.LoadBindingStore(t, d.Tasks)
	if got := afterBindings[drain.SetScopedKey(repoKey, setID)].RuntimePath; got != locked {
		t.Fatalf("binding runtime = %q, want unchanged %q", got, locked)
	}
}

func TestDashboardUKeyRequiresInlineConfirmBeforeUnbind(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{{Project: "pop", Worktree: "/repo/bound (branch)", CursorKey: "pop\x00set-unbind", RawStatus: tasks.StatusFailed, ID: "set-unbind", DefPath: "/repo/tasks", StatePath: "/repo/state.json", Bound: true}}})

	// Unbind now lives behind the action menu: open with `a`, then `u`.
	openMenu := func(model QueueDashboard) QueueDashboard {
		updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		if !menuHasKey(got.menu, "u") {
			t.Fatalf("unbind not offered on bound row: %+v", got.menu)
		}
		return got
	}

	got := openMenu(m)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	got = updated.(QueueDashboard)
	if cmd != nil {
		t.Fatalf("u key returned command before confirmation")
	}
	if got.menu != nil {
		t.Fatal("u did not close the action menu")
	}
	if got.abandon == nil || got.abandon.row.ID != "set-unbind" {
		t.Fatalf("abandon modal = %+v, want set-unbind", got.abandon)
	}
	if !strings.Contains(got.View().Content, "Unbind worktree for set-unbind") {
		t.Fatalf("view missing unbind modal:\n%s", got.View().Content)
	}

	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	got = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatalf("y confirm did not return unbind command")
	}
	if got.abandon == nil || !got.abandon.loading {
		t.Fatalf("abandon modal after confirm = %+v, want loading", got.abandon)
	}

	got = openMenu(m)
	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	got = updated.(QueueDashboard)
	updated, cmd = got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = updated.(QueueDashboard)
	if cmd != nil || got.abandon != nil {
		t.Fatalf("enter should cancel without command: modal=%+v cmd=%v", got.abandon, cmd)
	}

	got = openMenu(m)
	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	got = updated.(QueueDashboard)
	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	got = updated.(QueueDashboard)
	if cmd != nil || got.abandon != nil {
		t.Fatalf("cancel should close modal without command: modal=%+v cmd=%v", got.abandon, cmd)
	}
}

func TestDashboardUnbindManagedOnlyForgetsBindingAndKeepsCheckout(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "dashboard-unbind-managed", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
	})
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	beforeManifest := mustReadFile(t, filepath.Join(id.TasksDir, setID, "index.json"))
	wt := filepath.Join(t.TempDir(), "managed")
	runGit(t, repo, "worktree", "add", "-b", "managed-unbind", wt, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = wt
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: wt, Branch: "managed-unbind", Project: filepath.Base(repo), Provisioned: true},
	})

	got, err := drain.UnbindWorktree(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.UnbindWorktree: %v", err)
	}
	if got.Noop {
		t.Fatalf("unbind result = %+v, want success", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("managed checkout should remain: %v", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "managed-unbind"); strings.TrimSpace(branch) == "" {
		t.Fatalf("managed branch was removed")
	}
	if len(queuetest.LoadBindingStore(t, d.Tasks)) != 0 {
		t.Fatalf("bindings = %+v, want cleared", queuetest.LoadBindingStore(t, d.Tasks))
	}
	afterManifest := mustReadFile(t, filepath.Join(id.TasksDir, setID, "index.json"))
	if string(beforeManifest) != string(afterManifest) {
		t.Fatalf("manifest changed:\nbefore:%s\nafter:%s", beforeManifest, afterManifest)
	}
}

func TestDashboardUnbindAdoptedOnlyForgetsBindingAndKeepsStatus(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "dashboard-unbind-adopted", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	beforeManifest := mustReadFile(t, filepath.Join(id.TasksDir, setID, "index.json"))
	wt := filepath.Join(t.TempDir(), "adopted")
	runGit(t, repo, "worktree", "add", "-b", "adopted-unbind", wt, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = wt
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: wt, Branch: "adopted-unbind", Project: filepath.Base(repo), Provisioned: false},
	})

	got, err := drain.UnbindWorktree(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.UnbindWorktree: %v", err)
	}
	if got.Noop {
		t.Fatalf("unbind result = %+v, want success", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("adopted checkout should remain: %v", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "adopted-unbind"); strings.TrimSpace(branch) == "" {
		t.Fatalf("adopted branch was removed")
	}
	if len(queuetest.LoadBindingStore(t, d.Tasks)) != 0 {
		t.Fatalf("bindings = %+v, want cleared", queuetest.LoadBindingStore(t, d.Tasks))
	}
	afterManifest := mustReadFile(t, filepath.Join(id.TasksDir, setID, "index.json"))
	if string(beforeManifest) != string(afterManifest) {
		t.Fatalf("manifest changed:\nbefore:%s\nafter:%s", beforeManifest, afterManifest)
	}
}

func TestDashboardUnbindRefusesLiveLockAndNoopsWithoutBinding(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "dashboard-unbind-locked", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
	})
	wt := filepath.Join(t.TempDir(), "locked")
	runGit(t, repo, "worktree", "add", "-b", "locked-unbind", wt, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = wt
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: wt, Branch: "locked-unbind", Project: filepath.Base(repo), Provisioned: false},
	})
	d.ReadLock = func(runtimePath string) *tasks.RuntimeLockStatus {
		if runtimePath == wt {
			lock := queuetest.LiveLock(runtimePath)
			lock.Metadata.SetID = setID
			return lock
		}
		return queuetest.IdleLock(runtimePath)
	}

	_, err = drain.UnbindWorktree(d, cfg, row)
	if err == nil || !strings.Contains(err.Error(), "refusing unbind") {
		t.Fatalf("drain.UnbindWorktree err = %v, want live-lock refusal", err)
	}
	afterBindings := queuetest.LoadBindingStore(t, d.Tasks)
	if got := afterBindings[drain.SetScopedKey(repoKey, setID)].RuntimePath; got != wt {
		t.Fatalf("binding runtime = %q, want unchanged %q", got, wt)
	}

	d.ReadLock = func(runtimePath string) *tasks.RuntimeLockStatus {
		t.Fatalf("no-binding unbind must not read runtime lock")
		return nil
	}
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{})
	got, err := drain.UnbindWorktree(d, cfg, row)
	if err != nil {
		t.Fatalf("no-binding drain.UnbindWorktree: %v", err)
	}
	if !got.Noop {
		t.Fatalf("no-binding result = %+v, want noop", got)
	}
}

func TestDashboardLaunchDrainRefusesBareWithoutTrunk(t *testing.T) {
	_, wts := queuetest.InitBareRepoWithWorktrees(t, 1)
	checkout := wts[0]
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), checkout)
	if err != nil {
		t.Fatal(err)
	}
	setID := "bare-drain"
	setDir := filepath.Join(id.TasksDir, setID)
	queuetest.WriteSpawnTaskMD(t, setDir, "01-a.md")
	queuetest.WriteSpawnManifest(t, setDir, []queuetest.SpawnTask{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}})
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), id.TasksDir, tasks.StatePathFor(id.TasksDir)); err != nil {
		t.Fatal(err)
	}
	d, cfg, row, rt := dashboardLaunchFixture(t, checkout, setID)

	_, err = drain.LaunchDrain(d, cfg, row)
	if err == nil || !strings.Contains(err.Error(), drain.RepoScanReason) {
		t.Fatalf("drain.LaunchDrain err = %v, want %q", err, drain.RepoScanReason)
	}
	if len(rt.Commands) != 0 {
		t.Fatalf("bare no-base refusal must not touch tmux, got %v", rt.Commands)
	}
}

func dashboardLaunchFixture(t *testing.T, repo, setID string) (*drain.Deps, *config.Config, DashboardRow, *queuetest.RecordingTmux) {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	// A real repo with task sets always carries a repo.json storage marker
	// (EnsureStorage writes it on first task touch); the spawn fixture writes
	// task files directly and skips it, so write it here so BuildDashboard's
	// storage-scoped discovery sees this repo.
	if err := tasks.EnsureStorage(tasks.DefaultDeps(), id); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := queuetest.NewRecordingTmux(false, "0")
	td := queuetest.TasksDeps(t, true)
	d := &drain.Deps{Tasks: td, Project: project.DefaultDeps(), Tmux: rt}
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) == 0 {
		t.Fatalf("no picker projects for %s", repo)
	}
	defPath, err := drain.ScanDefinitionPath(d, projects[0])
	if err != nil {
		t.Fatal(err)
	}
	row := DashboardRow{Project: "pop", ID: setID, DefPath: defPath, StatePath: tasks.StatePathFor(id.TasksDir)}
	return d, cfg, row, rt
}

func assertDashboardPaneMapping(t *testing.T, d *drain.Deps, repo, setID, paneID, source string) {
	t.Helper()
	panes, err := tasks.AllDrainPanes(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	pane := panes[drain.SetScopedKey(repoKey, setID)]
	if pane.PaneID != paneID || pane.SetID != setID || pane.Source != source {
		t.Fatalf("pane mapping = %+v, want pane=%s set=%s source=%s", pane, paneID, setID, source)
	}
}

func filterTestModel() QueueDashboard {
	rows := []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusReady, ID: "set-one"},
		{Project: "beta", CursorKey: "beta\x00set-two", RawStatus: tasks.StatusReady, ID: "set-two"},
		{Project: "gamma", CursorKey: "gamma\x00feature", RawStatus: tasks.StatusFailed, ID: "feature"},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	m.list.SetCursor(2)
	return m
}

// filterMenuTestModel builds a model with two non-done rows under the default
// (active) Work view preset — the launch default the filter menu selects from.
func filterMenuTestModel() QueueDashboard {
	rows := []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusReady, ID: "set-one"},
		{Project: "beta", CursorKey: "beta\x00set-two", RawStatus: tasks.StatusFailed, ID: "set-two"},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	m.width = 120
	m.height = 20
	return m
}

// doneRow is a DONE task set a reload may deliver once an admitting preset
// (e.g. all) is selected.
func doneRow() DashboardRow {
	return DashboardRow{Project: "gamma", CursorKey: "gamma\x00done-set", RawStatus: tasks.StatusDone, ID: "done-set"}
}

func TestDashboardFilterMenuOpenAndClose(t *testing.T) {
	m := filterMenuTestModel()

	// `f` opens the filter modal without dispatching a command.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := updated.(QueueDashboard)
	if got.filter == nil {
		t.Fatal("f did not open the filter menu")
	}
	if cmd != nil {
		t.Fatal("opening the filter menu should not dispatch a command")
	}
	if got.searchTyping {
		t.Fatal("f must not start typing a search")
	}
	view := got.View().Content
	for _, want := range []string{"filters", "active", "[x]", "1 ", "enter select · esc close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("filter menu view missing %q:\n%s", want, view)
		}
	}
	for _, name := range []string{"unfolded", "recent-7d", "recent-30d", "all"} {
		if !strings.Contains(view, name) {
			t.Fatalf("filter menu missing preset %q:\n%s", name, view)
		}
	}

	// `esc` closes the overlay without quitting.
	updated, cmd = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got = updated.(QueueDashboard)
	if got.filter != nil {
		t.Fatal("esc did not close the filter menu")
	}
	if cmd != nil {
		t.Fatal("closing the filter menu should not quit or dispatch")
	}

	// `f` reopens and `f` again closes (sibling-modal toggle).
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got = updated.(QueueDashboard)
	if got.filter == nil {
		t.Fatal("f did not reopen the filter menu")
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got = updated.(QueueDashboard)
	if got.filter != nil {
		t.Fatal("second f did not close the filter menu")
	}
}

// TestDashboardFilterMenuVisibleOnTallTable is the overflow regression for
// ADR-0197 decision 9: with more rows than the pane can show, opening `f` must
// still render every preset line and the footer — the Frame reserves the block
// height so the table shrinks instead of pushing the menu off-screen.
func TestDashboardFilterMenuVisibleOnTallTable(t *testing.T) {
	rows := make([]DashboardRow, 40)
	for i := range rows {
		id := fmt.Sprintf("set-%02d", i)
		rows[i] = DashboardRow{Project: "pop", CursorKey: "pop\x00" + id, RawStatus: tasks.StatusReady, ID: id}
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	// One line taller than the reserved block, so the table still has a body to
	// shrink: below that the body floor absorbs part of the reservation and the
	// arithmetic this test pins stops being the thing under test.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 13})
	m = updated.(QueueDashboard)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	if m.filter == nil {
		t.Fatal("f did not open the filter menu")
	}

	view := m.View().Content
	for _, item := range dashboardFilterItems(m.cfg.ResolveWorkViewPresets()) {
		if !strings.Contains(view, item.label) {
			t.Fatalf("filter menu missing preset %q on a tall table:\n%s", item.label, view)
		}
	}
	if !strings.Contains(view, "filters") {
		t.Fatalf("filter menu missing caption on a tall table:\n%s", view)
	}
	if !strings.Contains(view, "1-9/enter select · esc close") {
		t.Fatalf("filter menu missing footer hint on a tall table:\n%s", view)
	}
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("view line count = %d, want %d (frame-clamped):\n%s", got, want, view)
	}
	// BodyHeight must shrink by exactly the reserved block — the same Frame
	// description drives budget and render.
	frame := m.frameSpec()
	blockLines := len(frame.Block)
	if blockLines == 0 {
		t.Fatal("frameSpec with open filter must reserve a Block region")
	}
	without := frame
	without.Block = nil
	if got, want := without.BodyHeight(m.height)-frame.BodyHeight(m.height), blockLines; got != want {
		t.Fatalf("BodyHeight shrink = %d, want %d (reserved Block height)", got, want)
	}
}

// TestDashboardFilterMenuFullScreenOnShortPane covers ADR-0197 decision 9's
// short-pane branch: below the two-line height floor the filter view takes the
// whole screen with no table behind it.
func TestDashboardFilterMenuFullScreenOnShortPane(t *testing.T) {
	rows := make([]DashboardRow, 10)
	for i := range rows {
		id := fmt.Sprintf("set-%02d", i)
		rows[i] = DashboardRow{Project: "pop", CursorKey: "pop\x00" + id, RawStatus: tasks.StatusReady, ID: id}
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: dashboardTwoLineHeightFloor - 1})
	m = updated.(QueueDashboard)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	view := m.View().Content
	for _, want := range []string{"filters", "active", "all", "1-9/enter select · esc close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("short-pane filter view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "PROJECT") || strings.Contains(view, "TASK SET") {
		t.Fatalf("short-pane filter view must not keep the table behind it:\n%s", view)
	}
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("short-pane filter view line count = %d, want %d:\n%s", got, want, view)
	}
}

// TestDashboardFilterMenuFitsAtHeightFloor is the above-floor band that task 01
// left unguarded: a nine-preset roster (caption + 9 lines) at exactly
// dashboardTwoLineHeightFloor must keep the footer hint on-screen and never
// paint past the pane.
func TestDashboardFilterMenuFitsAtHeightFloor(t *testing.T) {
	presets := make([]config.WorkViewPreset, 9)
	for i := range presets {
		presets[i] = config.WorkViewPreset{Name: fmt.Sprintf("preset-%d", i+1), Label: fmt.Sprintf("Preset %d", i+1)}
	}
	cfg := &config.Config{Work: &config.WorkConfig{
		Dashboard: &config.WorkDashboardConfig{
			Tasks: &config.WorkDashboardTasksConfig{Presets: presets},
		},
	}}
	rows := make([]DashboardRow, 20)
	for i := range rows {
		id := fmt.Sprintf("set-%02d", i)
		rows[i] = DashboardRow{Project: "pop", CursorKey: "pop\x00" + id, RawStatus: tasks.StatusReady, ID: id}
	}
	m := newQueueDashboard(&drain.Deps{ViewPreset: cfg.DefaultWorkViewPreset()}, cfg, DashboardSnapshot{Containers: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: dashboardTwoLineHeightFloor})
	m = updated.(QueueDashboard)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	if m.filter == nil {
		t.Fatal("f did not open the filter menu")
	}
	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("view line count = %d, want %d (must not overflow the pane):\n%s", got, want, view)
	}
	if !strings.Contains(lines[len(lines)-1], "1-9/enter select · esc close") {
		t.Fatalf("footer hint missing from last line %q:\n%s", lines[len(lines)-1], view)
	}
	if strings.Contains(view, "clipped to fit the pane") {
		// Nine presets + caption = 10 block lines; at height 16 with ordinary
		// chrome (header + hints) the block still fits — a clip here would mean
		// the floor path is cutting content it does not need to.
		t.Fatalf("unexpected block clip at the height floor with nine presets:\n%s", view)
	}
}

func TestDashboardFilterMenuDigitSelectsPreset(t *testing.T) {
	m := filterMenuTestModel()
	if len(m.snap.Containers) != 2 {
		t.Fatalf("initial rows = %d, want 2", len(m.snap.Containers))
	}
	if got := m.activeViewPreset().Name; got != "active" {
		t.Fatalf("initial preset = %q, want active", got)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	// Digit 5 selects shipped "all" (position 5) and rebuilds.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: '5', Text: "5"})
	m = updated.(QueueDashboard)
	if got := m.d.ViewPreset.Name; got != "all" {
		t.Fatalf("digit 5 preset = %q, want all", got)
	}
	if cmd == nil {
		t.Fatal("selecting a preset must trigger a rebuild")
	}
	if m.filter == nil {
		t.Fatal("selection should leave the filter menu open")
	}
	if !strings.Contains(m.View().Content, "[x] all") {
		t.Fatalf("active mark should move to all:\n%s", m.View().Content)
	}
	if strings.Count(m.View().Content, "[x]") != 1 {
		t.Fatalf("exactly one preset must be marked active:\n%s", m.View().Content)
	}

	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusReady, ID: "set-one"},
		{Project: "beta", CursorKey: "beta\x00set-two", RawStatus: tasks.StatusFailed, ID: "set-two"},
		doneRow(),
	}}})
	m = updated.(QueueDashboard)
	if len(m.snap.Containers) != 3 {
		t.Fatalf("after all reload: rows = %d, want 3", len(m.snap.Containers))
	}
	if !strings.Contains(m.View().Content, "done-set") {
		t.Fatalf("DONE set should be visible under all:\n%s", m.View().Content)
	}

	// Digit 6 reaches the muted preset — the only route back to a muted row, so
	// it has to be reachable by the same one-keystroke gesture as the rest
	// (ADR-0200 decision 8).
	updated, _ = m.Update(tea.KeyPressMsg{Code: '6', Text: "6"})
	m = updated.(QueueDashboard)
	if got := m.d.ViewPreset.Name; got != "muted" {
		t.Fatalf("digit 6 preset = %q, want muted", got)
	}
	if p := m.d.ViewPreset; p.Muted == nil || !*p.Muted {
		t.Fatalf("muted preset threaded without its muted filter: %#v", p.Muted)
	}

	// Digit 1 restores active.
	updated, _ = m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	m = updated.(QueueDashboard)
	if got := m.d.ViewPreset.Name; got != "active" {
		t.Fatalf("digit 1 preset = %q, want active", got)
	}
}

func TestDashboardFilterMenuEnterSelectsHighlighted(t *testing.T) {
	m := filterMenuTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	// Move to unfolded (position 2) and activate with Enter.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(QueueDashboard)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(QueueDashboard)
	if got.d.ViewPreset.Name != "unfolded" {
		t.Fatalf("enter preset = %q, want unfolded", got.d.ViewPreset.Name)
	}
	if cmd == nil {
		t.Fatal("enter select must trigger a rebuild")
	}
}

func TestDashboardFilterMenuTenthPresetViaJKEnter(t *testing.T) {
	// Ten presets: positions 1–9 keep digit keys; the tenth is j/k + Enter only.
	presets := make([]config.WorkViewPreset, 10)
	for i := range presets {
		presets[i] = config.WorkViewPreset{Name: fmt.Sprintf("p%d", i+1)}
	}
	cfg := &config.Config{Work: &config.WorkConfig{
		Dashboard: &config.WorkDashboardConfig{
			Tasks: &config.WorkDashboardTasksConfig{Presets: presets},
		},
	}}
	m := newQueueDashboard(&drain.Deps{ViewPreset: cfg.DefaultWorkViewPreset()}, cfg, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusReady, ID: "set-one"},
	}})
	m.width = 120
	m.height = 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	items := m.filter.list.Items()
	if len(items) != 10 {
		t.Fatalf("items = %d, want 10", len(items))
	}
	if items[9].key != "" {
		t.Fatalf("tenth preset key = %q, want empty", items[9].key)
	}
	for i := 0; i < 9; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		m = updated.(QueueDashboard)
	}
	if m.filter.list.Cursor() != 9 {
		t.Fatalf("cursor = %d, want 9 after nine j presses", m.filter.list.Cursor())
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(QueueDashboard)
	if got.d.ViewPreset.Name != "p10" {
		t.Fatalf("enter on tenth = %q, want p10", got.d.ViewPreset.Name)
	}
	if cmd == nil {
		t.Fatal("enter on tenth must rebuild")
	}
}

func TestDashboardFilterMenuSeedsActiveMark(t *testing.T) {
	// `--include-done` seeds the all preset at launch; the menu opens with all marked.
	all, _ := config.ShippedWorkViewPreset("all")
	m := newQueueDashboard(&drain.Deps{ViewPreset: all}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusReady, ID: "set-one"},
	}})
	m.width = 120
	m.height = 20
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := updated.(QueueDashboard)
	if !strings.Contains(got.View().Content, "[x] all") {
		t.Fatalf("all should seed marked from --include-done:\n%s", got.View().Content)
	}
	if got.filter.list.Cursor() != 4 {
		t.Fatalf("cursor = %d, want 4 (all)", got.filter.list.Cursor())
	}
}

func TestDashboardFilterMenuSessionOnly(t *testing.T) {
	m := filterMenuTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: '5', Text: "5"})
	m = updated.(QueueDashboard)
	if m.d.ViewPreset.Name != "all" {
		t.Fatalf("preset = %q, want all", m.d.ViewPreset.Name)
	}
	// Nothing is persisted: a fresh model starts on the default again.
	if fresh := filterMenuTestModel(); fresh.activeViewPreset().Name != "active" {
		t.Fatalf("fresh dashboard preset = %q, want active", fresh.activeViewPreset().Name)
	}
}

func TestDashboardPageHeaderNamesActivePreset(t *testing.T) {
	m := filterMenuTestModel()
	if !strings.Contains(m.pageHeader(), "Work · active ·") {
		t.Fatalf("header missing default preset name: %q", m.pageHeader())
	}
	all, _ := config.ShippedWorkViewPreset("all")
	m.d.ViewPreset = all
	if !strings.Contains(m.pageHeader(), "Work · all ·") {
		t.Fatalf("header missing selected preset name: %q", m.pageHeader())
	}

	// An empty list must still name the active preset in the Frame header —
	// otherwise "nothing here" is indistinguishable from "you filtered it away"
	// (ADR-0197 decision 8). pageHeader() alone is not enough: frameSpec used
	// to gate the Header on row count.
	empty := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{})
	empty.width = 120
	empty.height = 30
	view := empty.View().Content
	if !strings.Contains(view, "Work · active ·") {
		t.Fatalf("empty list view missing default preset name:\n%s", view)
	}
	if !strings.Contains(view, "No work-actionable task sets.") {
		t.Fatalf("empty list view missing empty message:\n%s", view)
	}
}

func TestDashboardFilterMenuIndependentOfSlash(t *testing.T) {
	// The `/` search is a distinct concept from the filter menu. Opening the menu
	// never starts a search, and a rebuild the menu triggers preserves the applied
	// search term.
	m := filterTestModel()

	// A search narrows the visible rows.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	for _, ch := range "beta" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(QueueDashboard)
	if len(m.snap.Containers) != 1 {
		t.Fatalf("after 'beta' query: rows = %d, want 1", len(m.snap.Containers))
	}
	if m.filter != nil {
		t.Fatal("searching must not open the filter menu")
	}

	// A rebuild triggered while the search is applied (as a preset pick would
	// trigger) re-applies the term rather than dropping it.
	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusReady, ID: "set-one"},
		{Project: "beta", CursorKey: "beta\x00set-two", RawStatus: tasks.StatusFailed, ID: "set-two"},
		{Project: "gamma", CursorKey: "gamma\x00feature", RawStatus: tasks.StatusFailed, ID: "feature"},
	}}})
	m = updated.(QueueDashboard)
	if m.searchTerm != "beta" {
		t.Fatalf("rebuild dropped the search term: %q", m.searchTerm)
	}
	if len(m.snap.Containers) != 1 || m.snap.Containers[0].Project != "beta" {
		t.Fatalf("rebuild did not preserve the 'beta' query: rows = %d", len(m.snap.Containers))
	}

	// The `f` menu opens with a search in force, and opening it starts no typing.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := updated.(QueueDashboard)
	if got.filter == nil {
		t.Fatal("f did not open the filter menu with a search applied")
	}
	if got.searchTyping {
		t.Fatal("opening the filter menu must not start typing a search")
	}
	if got.searchTerm != "beta" {
		t.Fatalf("opening the filter menu dropped the search term: %q", got.searchTerm)
	}
}

func TestDashboardFilterMenuBlocksViewToggle(t *testing.T) {
	m := filterMenuTestModel()
	if !m.ViewToggleAllowed() {
		t.Fatal("view toggle should be allowed on the plain main list")
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := updated.(QueueDashboard)
	if got.ViewToggleAllowed() {
		t.Fatal("view toggle must be blocked while the filter menu is open")
	}
}

// typeSearch drives the model through `/`, the query's characters and, when
// apply is set, the Enter that commits it — the way a human reaches a narrowed
// list.
func typeSearch(m QueueDashboard, query string, apply bool) QueueDashboard {
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	for _, ch := range query {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}
	if apply {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(QueueDashboard)
	}
	return m
}

func TestDashboardSearch_SlashStartsTypingFromAnEmptyBuffer(t *testing.T) {
	m := typeSearch(filterTestModel(), "alpha", true)
	if len(m.snap.Containers) != 1 {
		t.Fatalf("applied 'alpha': rows = %d, want 1", len(m.snap.Containers))
	}

	// Reopening starts empty, so the rows widen back to the whole preset before a
	// single character is typed.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	got := updated.(QueueDashboard)
	if !got.searchTyping {
		t.Fatal("expected the typing phase after /")
	}
	if got.searchInput.Value() != "" {
		t.Fatalf("buffer = %q, want empty on open", got.searchInput.Value())
	}
	if len(got.snap.Containers) != 3 {
		t.Fatalf("rows = %d, want 3 (widened on open)", len(got.snap.Containers))
	}
}

func TestDashboardSearch_EmptyQueryClearsTheSearch(t *testing.T) {
	m := typeSearch(filterTestModel(), "alpha", true)
	// `/` then Enter applies the empty query, which is how a search is cleared.
	m = typeSearch(m, "", true)
	if m.searchTerm != "" {
		t.Fatalf("search term = %q, want empty", m.searchTerm)
	}
	if len(m.snap.Containers) != 3 {
		t.Fatalf("after clearing: rows = %d, want 3", len(m.snap.Containers))
	}
}

func TestDashboardSearch_EscRestoresThePreviousTerm(t *testing.T) {
	m := typeSearch(filterTestModel(), "set", true)
	if len(m.snap.Containers) != 2 {
		t.Fatalf("applied 'set': rows = %d, want 2", len(m.snap.Containers))
	}

	// Abandon an edit that would have narrowed further: the term in force comes
	// back, rows and all.
	m = typeSearch(m, "alpha", false)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := updated.(QueueDashboard)
	if got.searchTyping {
		t.Fatal("esc must leave the typing phase")
	}
	if got.searchTerm != "set" {
		t.Fatalf("search term = %q, want the 'set' that was in force", got.searchTerm)
	}
	if got.searchInput.Value() != "" {
		t.Fatalf("buffer = %q, want empty after esc", got.searchInput.Value())
	}
	if len(got.snap.Containers) != 2 {
		t.Fatalf("after esc: rows = %d, want the 2 'set' matched", len(got.snap.Containers))
	}

	// With no term in force, abandoning applies none.
	fresh := typeSearch(filterTestModel(), "alpha", false)
	updated, _ = fresh.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	fresh = updated.(QueueDashboard)
	if fresh.searchTerm != "" {
		t.Fatalf("search term = %q, want none", fresh.searchTerm)
	}
	if len(fresh.snap.Containers) != 3 {
		t.Fatalf("after esc: rows = %d, want all 3 back", len(fresh.snap.Containers))
	}
}

func TestDashboardSearch_EscQuitsWhenNotTyping(t *testing.T) {
	m := typeSearch(filterTestModel(), "set", true)
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc on the applied list did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("esc command = %T, want tea.QuitMsg", cmd())
	}
}

func TestDashboardSearch_TypingNarrowsRows(t *testing.T) {
	m := filterTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)

	// The rows narrow on each keystroke, before anything is applied: "alph" is
	// already down to the one project, and the final "a" keeps it there.
	for i, ch := range "alpha" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
		want := 3
		switch i {
		case 1, 2, 3, 4: // "al", "alp", "alph", "alpha"
			want = 1
		}
		if len(m.snap.Containers) != want {
			t.Fatalf("after %q: rows = %d, want %d", "alpha"[:i+1], len(m.snap.Containers), want)
		}
	}
	if m.snap.Containers[0].Project != "alpha" {
		t.Fatalf("narrowed row project = %q, want alpha", m.snap.Containers[0].Project)
	}
}

func TestDashboardSearch_MatchesSetID(t *testing.T) {
	// "feature" matches SetID "feature" in project "gamma"
	m := typeSearch(filterTestModel(), "feature", true)
	if len(m.snap.Containers) != 1 {
		t.Fatalf("after 'feature': rows = %d, want 1", len(m.snap.Containers))
	}
	if m.snap.Containers[0].ID != "feature" {
		t.Fatalf("matched row setID = %q, want feature", m.snap.Containers[0].ID)
	}
}

func TestDashboardSearch_CursorClampedToFilteredRows(t *testing.T) {
	m := filterTestModel()
	m.list.SetCursor(2) // on gamma/feature

	// Only alpha/set-one matches; the cursored row is gone, so the cursor must
	// land in bounds rather than off the end of the narrowed list.
	m = typeSearch(m, "alpha", true)
	if c := m.list.Cursor(); c < 0 || c >= len(m.snap.Containers) {
		t.Fatalf("cursor = %d, out of bounds for %d narrowed rows", c, len(m.snap.Containers))
	}
}

func TestDashboardSearch_CursorStaysOnTheSameContainer(t *testing.T) {
	m := filterTestModel()
	m.list.SetCursor(2) // on gamma/feature

	// Applying a search does not move the human off the row they were hunting
	// for, even though its index changed.
	m = typeSearch(m, "gamma", true)
	row, ok := m.list.Selected()
	if !ok || row.CursorKey != "gamma\x00feature" {
		t.Fatalf("after applying: cursor on %+v, want gamma/feature", row)
	}

	// Neither does clearing it.
	m = typeSearch(m, "", true)
	row, ok = m.list.Selected()
	if !ok || row.CursorKey != "gamma\x00feature" {
		t.Fatalf("after clearing: cursor on %+v, want gamma/feature", row)
	}

	// Only a search that excludes the cursored row moves it, and then to the top.
	m.list.SetCursor(2)
	m = typeSearch(m, "alpha", true)
	if got := m.list.Cursor(); got != 0 {
		t.Fatalf("cursor = %d, want the top row when the cursored one is gone", got)
	}
}

// TestDashboardSearch_EveryPrintableKeyIsText pins the rule the typing phase is
// built to (ADR-0213): the navigation keys, the quit key, the page toggle and
// the digits are all just characters, so a set called "jj" or "k8s" can be
// searched for.
func TestDashboardSearch_EveryPrintableKeyIsText(t *testing.T) {
	m := filterTestModel()
	m.list.SetCursor(0)
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)

	for _, ch := range "jkvq19" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}
	if m.searchInput.Value() != "jkvq19" {
		t.Fatalf("buffer = %q, want every key typed", m.searchInput.Value())
	}
	if !m.searchTyping {
		t.Fatal("q must not quit the typing phase")
	}
	if m.list.Cursor() != 0 {
		t.Fatalf("cursor = %d: j/k navigated instead of typing", m.list.Cursor())
	}
	if m.ViewToggleAllowed() {
		t.Fatal("the page toggle must be off while typing, so v is typeable")
	}
	if m.filter != nil || m.menu != nil || m.detail != nil {
		t.Fatal("no overlay may open from a printable key while typing")
	}
}

// TestDashboardSearch_ApplyRestoresTheWholeKeymap is the other half of the rule:
// what typing costs, Enter gives back. Over the narrowed rows every key means
// what it means on the main list, and the search does not drop.
func TestDashboardSearch_ApplyRestoresTheWholeKeymap(t *testing.T) {
	copied := ""
	m := filterTestModel()
	m.copyFunc = func(s string) error { copied = s; return nil }
	m = typeSearch(m, "set", true)
	if m.searchTyping {
		t.Fatal("enter must leave the typing phase")
	}
	if m.searchTerm != "set" {
		t.Fatalf("search term = %q, want 'set'", m.searchTerm)
	}
	if len(m.snap.Containers) != 2 {
		t.Fatalf("after 'set': rows = %d, want 2", len(m.snap.Containers))
	}

	m.list.SetCursor(0)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(QueueDashboard)
	if m.list.Cursor() != 1 {
		t.Fatalf("j over the narrowed rows: cursor = %d, want 1", m.list.Cursor())
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m = updated.(QueueDashboard)
	if m.list.Cursor() != 0 {
		t.Fatalf("k over the narrowed rows: cursor = %d, want 0", m.list.Cursor())
	}

	// An action key runs its verb on the narrowed row.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = updated.(QueueDashboard)
	if copied != "set-one" {
		t.Fatalf("y copied %q, want set-one", copied)
	}

	// Enter opens the detail view rather than being swallowed by a field.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(QueueDashboard)
	if m.detail == nil {
		t.Fatal("enter did not open the detail view")
	}
	if m.searchTerm != "set" || len(m.snap.Containers) != 2 {
		t.Fatalf("the search dropped while acting on a row: term %q over %d rows", m.searchTerm, len(m.snap.Containers))
	}
}

// TestDashboardSearch_BareActionsAreTextWhileTyping keeps the verbs off the
// keyboard for exactly as long as the field owns it: a letter that would unbind
// or drain a set is a letter, and Enter applies rather than acting.
func TestDashboardSearch_BareActionsAreTextWhileTyping(t *testing.T) {
	called := false
	d := &drain.Deps{
		ToggleAutoDrain: func(defPath, statePath, setID string) (*tasks.AutoDrainResult, error) {
			called = true
			return &tasks.AutoDrainResult{}, nil
		},
	}
	rows := []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusReady, ID: "set-one", DefPath: "/def", StatePath: "/state"},
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: rows})
	m.list.SetCursor(0)
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	for _, key := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'I', Text: "I"},
		{Code: 'b', Text: "b"},
		{Code: 'U', Text: "U"},
		{Code: 'u', Text: "u"},
		{Code: 'a', Text: "a"},
		{Code: 'p', Text: "p"},
		{Code: 's', Text: "s"},
		{Code: 'l', Text: "l"},
	} {
		updated, _ = m.Update(key)
		m = updated.(QueueDashboard)
	}
	if called {
		t.Fatal("bare-letter actions must be text while typing")
	}
	if m.bind != nil || m.abandon != nil || m.detail != nil || m.menu != nil {
		t.Fatal("modals must not open while typing")
	}
	if m.searchInput.Value() != "iIbUuapsl" {
		t.Fatalf("buffer = %q, want every action letter typed", m.searchInput.Value())
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(QueueDashboard)
	if m.detail != nil {
		t.Fatal("the applying enter must not also open a row")
	}
	if m.searchTerm != "iIbUuapsl" {
		t.Fatalf("search term = %q, want the typed buffer", m.searchTerm)
	}
}

func TestDashboardSearch_SurvivesReloadAndPresetChange(t *testing.T) {
	m := typeSearch(filterTestModel(), "alpha", true)
	if len(m.snap.Containers) != 1 {
		t.Fatalf("before reload: rows = %d, want 1", len(m.snap.Containers))
	}

	// A background poll delivers new rows; the term is re-applied to them.
	newRows := []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusBlocked, ID: "set-one"},
		{Project: "beta", CursorKey: "beta\x00set-two", RawStatus: tasks.StatusReady, ID: "set-two"},
		{Project: "delta", CursorKey: "delta\x00alpha-task", RawStatus: tasks.StatusReady, ID: "alpha-task"},
	}
	updated, _ := m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: newRows}})
	m = updated.(QueueDashboard)
	if m.searchTerm != "alpha" {
		t.Fatalf("search term = %q, want it to survive the poll", m.searchTerm)
	}
	// "alpha" matches the alpha project and the alpha-task set.
	if len(m.snap.Containers) != 2 {
		t.Fatalf("after the poll: rows = %d, want 2", len(m.snap.Containers))
	}

	// A preset pick reloads through the same message, so the search survives it
	// too — and still only subtracts from what the new preset selected.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: '4', Text: "4"}) // a preset by number
	m = updated.(QueueDashboard)
	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: append(newRows, doneRow())}})
	m = updated.(QueueDashboard)
	if m.searchTerm != "alpha" {
		t.Fatalf("search term = %q, want it to survive the preset change", m.searchTerm)
	}
	if len(m.snap.Containers) != 2 {
		t.Fatalf("after the preset change: rows = %d, want the 2 'alpha' matched", len(m.snap.Containers))
	}
	for _, row := range m.snap.Containers {
		if row.ID == "done-set" {
			t.Fatal("the search must subtract from the preset's rows, never widen past them")
		}
	}
}

// TestDashboardSearch_HeaderNamesTheAppliedTerm covers the chrome: with the
// typing over, the term is what tells the human why rows are missing.
func TestDashboardSearch_HeaderNamesTheAppliedTerm(t *testing.T) {
	m := filterTestModel()
	m.width = 120
	m.height = 24
	m = typeSearch(m, "beta", true)
	if view := m.View().Content; !strings.Contains(view, "search: beta") {
		t.Fatalf("header does not name the applied search:\n%s", view)
	}
	m = typeSearch(m, "", true)
	if view := m.View().Content; strings.Contains(view, "search: ") {
		t.Fatalf("cleared search still named in the header:\n%s", view)
	}
}

// The Routine page has no preset menu, so its header names the search alone —
// the form must not depend on a preset being there to sit beside.
func TestDashboardSearch_HeaderWithoutAPresetMenu(t *testing.T) {
	m := openPage(t, routinePageDeps(routinePageContainers()), PageRoutines)
	m = typeSearch(m, "a", true)
	header := m.pageHeader()
	if !strings.HasPrefix(header, "Routines · search: a · ") {
		t.Fatalf("routine page header = %q, want the search named right after the page", header)
	}
}

// The zero-match view is the one screen with no rows to read, so it has to
// carry both the term that emptied it and the keys that undo it.
func TestDashboardSearch_EmptyStateNamesTheTermAndTheWayOut(t *testing.T) {
	m := filterTestModel()
	m.width = 120
	m.height = 24
	m = typeSearch(m, "zzz", true)
	view := m.View().Content
	for _, want := range []string{`No task sets match search "zzz"`, "/ then enter to clear"} {
		if !strings.Contains(view, want) {
			t.Fatalf("zero-match view missing %q:\n%s", want, view)
		}
	}
}

// One word per concept: `/` is the search and `f` is the filter, and neither
// borrows the other's noun on screen.
func TestDashboardSearch_VocabularyDoesNotBorrowTheFilterWord(t *testing.T) {
	m := filterTestModel()
	m.width = 120
	m.height = 24

	typing := typeSearch(m, "alpha", false)
	if hint := typing.mainHint(); !strings.Contains(hint, "enter apply search") ||
		!strings.Contains(hint, "esc cancel") ||
		strings.Contains(hint, "j/k") || strings.Contains(hint, " v ") {
		t.Fatalf("typing hint = %q, want apply/cancel and no navigation", hint)
	}
	if strings.Contains(typing.View().Content, "filter") {
		t.Fatalf("the search calls itself a filter somewhere:\n%s", typing.View().Content)
	}

	applied := typeSearch(m, "alpha", true)
	if strings.Contains(applied.pageHeader(), "filter") {
		t.Fatalf("applied search named a filter in the header: %q", applied.pageHeader())
	}

	// The `f` menu keeps the word "filter" and never calls itself a search.
	menu := filterMenuTestModel()
	updated, _ := menu.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	menu = updated.(QueueDashboard)
	view := menu.View().Content
	if !strings.Contains(view, "filters") {
		t.Fatalf("the preset menu dropped the word filter:\n%s", view)
	}
	if strings.Contains(view, "search") {
		t.Fatalf("the preset menu calls itself a search:\n%s", view)
	}
}

func TestFilterDashboardRows(t *testing.T) {
	rows := []DashboardRow{
		{Project: "alpha", ID: "set-one"},
		{Project: "beta", ID: "set-two"},
		{Project: "gamma", ID: "feature"},
	}

	t.Run("empty query returns all rows", func(t *testing.T) {
		got := filterDashboardRows(rows, "")
		if len(got) != 3 {
			t.Fatalf("empty filter: got %d rows, want 3", len(got))
		}
	})

	t.Run("matches project name", func(t *testing.T) {
		got := filterDashboardRows(rows, "beta")
		if len(got) != 1 || got[0].Project != "beta" {
			t.Fatalf("got %+v, want beta row", got)
		}
	})

	t.Run("matches set ID", func(t *testing.T) {
		got := filterDashboardRows(rows, "feature")
		if len(got) != 1 || got[0].ID != "feature" {
			t.Fatalf("got %+v, want feature row", got)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		got := filterDashboardRows(rows, "ALPHA")
		if len(got) != 1 || got[0].Project != "alpha" {
			t.Fatalf("got %+v, want alpha row", got)
		}
	})

	t.Run("partial match works", func(t *testing.T) {
		got := filterDashboardRows(rows, "set")
		if len(got) != 2 {
			t.Fatalf("got %d rows for 'set', want 2", len(got))
		}
	})

	t.Run("no match returns nil", func(t *testing.T) {
		got := filterDashboardRows(rows, "zzz")
		if len(got) != 0 {
			t.Fatalf("got %d rows for 'zzz', want 0", len(got))
		}
	})
}

// detailOverrideModel builds a QueueDashboard with a loaded detailView and
// injectable override seams. The seams record calls and return the provided error.
func detailOverrideModel(row DashboardRow, task tasks.Task, completeErr, resetErr, skipErr error) (QueueDashboard, *int, *int, *int) {
	completeCalls, resetCalls, skipCalls := 0, 0, 0
	d := &drain.Deps{
		CompleteDetailTask: func(defPath, taskPath string) error {
			completeCalls++
			return completeErr
		},
		ResetDetailTask: func(defPath, taskPath string) error {
			resetCalls++
			return resetErr
		},
		SkipDetailTask: func(defPath, taskPath string) error {
			skipCalls++
			return skipErr
		},
	}
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{task},
	}
	m := newQueueDashboard(d, nil, DashboardSnapshot{Containers: []DashboardRow{row}})
	dv := newTaskDetailView(row, manifest, nil)
	m.detail = dv
	return m, &completeCalls, &resetCalls, &skipCalls
}

// itemMenuKeys returns the verb-letter keys offered by an open item menu.
func itemMenuKeys(menu *itemMenu) []string {
	if menu == nil {
		return nil
	}
	actions := menu.list.Items()
	keys := make([]string, len(actions))
	for i, action := range actions {
		keys[i] = action.Key
	}
	return keys
}

// openTaskMenu presses `a` in the detail view and returns the resulting model.
func openTaskMenu(t *testing.T, m QueueDashboard) QueueDashboard {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	return updated.(QueueDashboard)
}

func TestDetailTaskMenuCompleteVerb(t *testing.T) {
	row := DashboardRow{ID: "set-x", DefPath: "/def"}

	// Complete on open task: menu offers C, dispatching it runs the command.
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, completeCalls, _, _ := detailOverrideModel(row, openTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	if m.itemMenu == nil {
		t.Fatal("a on open task: expected task menu to open")
	}
	if got := itemMenuKeys(m.itemMenu); !slices.Contains(got, "c") {
		t.Fatalf("open task menu = %v, want to contain C", got)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	got := updated.(QueueDashboard)
	if got.itemMenu != nil {
		t.Fatal("C should close the menu")
	}
	if cmd == nil {
		t.Fatal("C on open task: expected a command to be dispatched")
	}
	msg := cmd()
	if *completeCalls != 1 {
		t.Fatalf("completeCalls = %d, want 1", *completeCalls)
	}
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if !strings.Contains(got.detail.flash.Text(), "complete") {
		t.Fatalf("C confirmation = %q, want 'complete'", got.detail.flash.Text())
	}

	// Done task: Complete does not apply, but Open (reopen) does — the menu
	// opens with O only, mirroring CanReopen.
	doneTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "done"}
	m2, completeCalls2, resetCalls2, _ := detailOverrideModel(row, doneTask, nil, nil, nil)
	m2 = openTaskMenu(t, m2)
	if m2.itemMenu == nil {
		t.Fatal("a on done task: expected task menu to open with Open verb")
	}
	keys := itemMenuKeys(m2.itemMenu)
	if slices.Contains(keys, "c") {
		t.Fatalf("done task menu = %v, want NOT to contain C", keys)
	}
	if !slices.Contains(keys, "o") {
		t.Fatalf("done task menu = %v, want to contain O", keys)
	}
	_, cmd2 := m2.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if cmd2 == nil {
		t.Fatal("O on done task: expected a command")
	}
	cmd2()
	if *resetCalls2 != 1 {
		t.Fatalf("O on done: resetCalls = %d, want 1", *resetCalls2)
	}
	if *completeCalls2 != 0 {
		t.Fatalf("done task: completeCalls = %d, want 0", *completeCalls2)
	}
}

func TestDetailTaskMenuOpenVerb(t *testing.T) {
	row := DashboardRow{ID: "set-y", DefPath: "/def"}

	// Open on failed task: menu offers O.
	failedTask := tasks.Task{ID: "02-b", File: "02-b.md", Status: "failed"}
	m, _, resetCalls, _ := detailOverrideModel(row, failedTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	if got := itemMenuKeys(m.itemMenu); !slices.Contains(got, "o") {
		t.Fatalf("failed task menu = %v, want to contain O", got)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	got := updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("O on failed: expected a command")
	}
	msg := cmd()
	if *resetCalls != 1 {
		t.Fatalf("resetCalls = %d, want 1", *resetCalls)
	}
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if !strings.Contains(got.detail.flash.Text(), "open") {
		t.Fatalf("O confirmation = %q, want 'open'", got.detail.flash.Text())
	}

	// Open on skipped task: also offered.
	skippedTask := tasks.Task{ID: "03-c", File: "03-c.md", Status: "skipped"}
	m2, _, resetCalls2, _ := detailOverrideModel(row, skippedTask, nil, nil, nil)
	m2 = openTaskMenu(t, m2)
	if got := itemMenuKeys(m2.itemMenu); !slices.Contains(got, "o") {
		t.Fatalf("skipped task menu = %v, want to contain O", got)
	}
	_, cmd2 := m2.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if cmd2 == nil {
		t.Fatal("O on skipped: expected a command")
	}
	cmd2()
	if *resetCalls2 != 1 {
		t.Fatalf("O on skipped: resetCalls = %d, want 1", *resetCalls2)
	}

	// Open is NOT offered for an already-open task (CanReopen excludes open).
	openTask := tasks.Task{ID: "04-d", File: "04-d.md", Status: "open"}
	m3, _, resetCalls3, _ := detailOverrideModel(row, openTask, nil, nil, nil)
	m3 = openTaskMenu(t, m3)
	if got := itemMenuKeys(m3.itemMenu); slices.Contains(got, "o") {
		t.Fatalf("open task menu = %v, want NOT to contain O", got)
	}
	// Pressing O is inert while the menu is open and has no O item.
	_, cmd3 := m3.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if cmd3 != nil {
		t.Fatal("O on open task: expected no command")
	}
	if *resetCalls3 != 0 {
		t.Fatalf("O on open: resetCalls = %d, want 0", *resetCalls3)
	}
}

func TestDetailTaskMenuSkipVerb(t *testing.T) {
	row := DashboardRow{ID: "set-z", DefPath: "/def"}

	// Skip on open task: menu offers s.
	openTask := tasks.Task{ID: "04-d", File: "04-d.md", Status: "open"}
	m, _, _, skipCalls := detailOverrideModel(row, openTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	if got := itemMenuKeys(m.itemMenu); !slices.Contains(got, "s") {
		t.Fatalf("open task menu = %v, want to contain s", got)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	got := updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("s on open: expected a command")
	}
	msg := cmd()
	if *skipCalls != 1 {
		t.Fatalf("skipCalls = %d, want 1", *skipCalls)
	}
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if !strings.Contains(got.detail.flash.Text(), "skip") {
		t.Fatalf("s confirmation = %q, want 'skip'", got.detail.flash.Text())
	}

	// Skip is NOT offered for a failed task (requires open).
	failedTask := tasks.Task{ID: "04-d", File: "04-d.md", Status: "failed"}
	m2, _, _, skipCalls2 := detailOverrideModel(row, failedTask, nil, nil, nil)
	m2 = openTaskMenu(t, m2)
	if got := itemMenuKeys(m2.itemMenu); slices.Contains(got, "s") {
		t.Fatalf("failed task menu = %v, want NOT to contain s", got)
	}
	_, cmd2 := m2.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if cmd2 != nil {
		t.Fatal("s on failed: expected no command")
	}
	if *skipCalls2 != 0 {
		t.Fatalf("s on failed: skipCalls = %d, want 0", *skipCalls2)
	}
}

// TestTaskMenuKMovesCursor pins j/k as movement-only in the task menu: `k` must
// move the highlight rather than fire a verb.
func TestTaskMenuKMovesCursor(t *testing.T) {
	row := DashboardRow{ID: "set-move", DefPath: "/def"}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, completeCalls, resetCalls, skipCalls := detailOverrideModel(row, openTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	before := m.itemMenu.list.Cursor()

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got := updated.(QueueDashboard)
	if cmd != nil {
		t.Fatal("k in task menu: expected no command")
	}
	if got.itemMenu == nil {
		t.Fatal("k in task menu: menu should stay open")
	}
	if got.itemMenu.list.Cursor() == before {
		t.Fatalf("k in task menu: cursor did not move from %d", before)
	}
	if *completeCalls != 0 || *resetCalls != 0 || *skipCalls != 0 {
		t.Fatal("k in task menu should not change any task status")
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got = updated.(QueueDashboard)
	if got.itemMenu.list.Cursor() != before {
		t.Fatalf("j should return the cursor to %d, got %d", before, got.itemMenu.list.Cursor())
	}
}

// TestStatusSubmenuKMovesCursor pins j/k as movement-only in the status submenu.
func TestStatusSubmenuKMovesCursor(t *testing.T) {
	row := DashboardRow{ID: "demo"}
	m := newQueueDashboard(nil, nil, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.menu = newDashboardMenu(testKinds(), row, false)

	updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	got := updated.(QueueDashboard)
	if got.menu == nil || got.menu.status == nil {
		t.Fatal("s should open status submenu")
	}
	before := got.menu.status.list.Cursor()

	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got = updated.(QueueDashboard)
	if cmd != nil {
		t.Fatal("k in status submenu: expected no command (no status verb)")
	}
	if got.menu == nil || got.menu.status == nil {
		t.Fatal("k in status submenu: submenu should stay open")
	}
	if got.menu.status.list.Cursor() == before {
		t.Fatalf("k in status submenu: cursor did not move from %d", before)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got = updated.(QueueDashboard)
	if got.menu.status.list.Cursor() != before {
		t.Fatalf("j should return the cursor to %d, got %d", before, got.menu.status.list.Cursor())
	}
}

// TestDashboardMenusReserveMovementKeys keeps j/k/J/K movement-only: no menu
// item in any dashboard table may bind them, so a new verb cannot shadow
// navigation. Nothing else is reserved — ADR-0202 decision 5 returned alt+a to
// kind key space with the picker it opened.
func TestDashboardMenusReserveMovementKeys(t *testing.T) {
	reserved := []string{"j", "k", "J", "K"}
	check := func(table string, keys []string) {
		t.Helper()
		for _, key := range keys {
			if slices.Contains(reserved, key) {
				t.Errorf("%s binds reserved key %q", table, key)
			}
		}
	}

	rows := []DashboardRow{
		{ID: "plain"},
		{ID: "bound", Bound: true, RawStatus: tasks.StatusNeedsVerify},
		{ID: "parked", Parked: true, Orphaned: true},
		{Kind: ref.KindMap, ID: "map"},
	}
	for _, row := range rows {
		var keys []string
		for _, item := range dashboardMenuItems(testKinds(), row) {
			keys = append(keys, item.key)
		}
		check("action menu ("+row.ID+")", keys)
	}

	for _, row := range rows {
		var statusKeys []string
		for _, item := range testKinds().statusActionsFor(row) {
			statusKeys = append(statusKeys, item.Key)
		}
		if len(statusKeys) == 0 {
			continue
		}
		check("status submenu ("+row.ID+")", statusKeys)
	}

	kinds := testKinds()
	for _, status := range []tasks.TaskStatus{tasks.TaskOpen, tasks.TaskDone, "failed", "skipped"} {
		var keys []string
		item := work.Item{ID: "01-a", File: "01-a.md", Status: string(status)}
		for _, action := range kinds.itemActionsFor(DashboardRow{ID: "set"}, item) {
			keys = append(keys, action.Key)
		}
		check("item menu ("+string(status)+")", keys)
	}
	for _, status := range []string{"open", "claimed", "resolved"} {
		var keys []string
		item := work.Item{ID: "01", Status: status}
		for _, action := range kinds.itemActionsFor(DashboardRow{Kind: ref.KindMap, ID: "map"}, item) {
			keys = append(keys, action.Key)
		}
		check("map item menu ("+status+")", keys)
	}

	var filterKeys []string
	for _, item := range dashboardFilterItems((&config.Config{}).ResolveWorkViewPresets()) {
		filterKeys = append(filterKeys, item.key)
	}
	check("filter menu", filterKeys)
}

// TestDetailTaskMenuDispatchViaEnter exercises j/k highlight + Enter dispatch.
func TestDetailTaskMenuDispatchViaEnter(t *testing.T) {
	row := DashboardRow{ID: "set-enter", DefPath: "/def"}
	failedTask := tasks.Task{ID: "02-b", File: "02-b.md", Status: "failed"}
	m, completeCalls, resetCalls, _ := detailOverrideModel(row, failedTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	// Menu order for a failed task: complete (C), open (O). Highlight O via j.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got := updated.(QueueDashboard)
	if got.itemMenu.list.Cursor() != 1 {
		t.Fatalf("after j cursor = %d, want 1", got.itemMenu.list.Cursor())
	}
	updated, cmd := got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("Enter on highlighted O: expected a command")
	}
	cmd()
	if *resetCalls != 1 {
		t.Fatalf("resetCalls = %d, want 1", *resetCalls)
	}
	if *completeCalls != 0 {
		t.Fatalf("completeCalls = %d, want 0", *completeCalls)
	}
}

// TestDetailTaskMenuEscCloses verifies esc dismisses the menu without dispatch.
func TestDetailTaskMenuEscCloses(t *testing.T) {
	row := DashboardRow{ID: "set-esc", DefPath: "/def"}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, completeCalls, _, skipCalls := detailOverrideModel(row, openTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := updated.(QueueDashboard)
	if got.itemMenu != nil {
		t.Fatal("esc should close the task menu")
	}
	if cmd != nil {
		t.Fatal("esc should not dispatch a command")
	}
	if got.detail == nil {
		t.Fatal("esc should keep the detail view open")
	}
	if *completeCalls != 0 || *skipCalls != 0 {
		t.Fatal("esc should not dispatch any verb")
	}
}

func TestDetailTaskMenuErrorSurfaced(t *testing.T) {
	row := DashboardRow{ID: "set-err", DefPath: "/def"}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	someErr := errors.New("blocked by unsatisfied")
	m, _, _, _ := detailOverrideModel(row, openTask, someErr, nil, nil)

	m = openTaskMenu(t, m)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	got := updated.(QueueDashboard)
	msg := cmd()
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if !strings.Contains(got.detail.flash.Text(), "error") {
		t.Fatalf("error not surfaced in flash: %q", got.detail.flash.Text())
	}
}

func TestDetailViewActionsHintRendered(t *testing.T) {
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Status: "open", Type: "AFK", Title: "A"}},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-render", RawStatus: tasks.StatusReady, ID: "set-render"},
	}})
	m.width = 80
	m.height = 12
	m.detail = newTaskDetailView(m.snap.Containers[0], manifest, nil)

	out := m.viewDetail()
	if !strings.Contains(out, "a actions") {
		t.Fatalf("hint line missing actions key:\n%s", out)
	}

	// A live flash takes that same line for its three seconds (ADR-0204), so the
	// message is what the operator reads and the hints stand down.
	m.detail.flash.Set("completed 01-a")
	out = m.viewDetail()
	if !strings.Contains(out, "completed 01-a") {
		t.Fatalf("flash not rendered:\n%s", out)
	}
	if strings.Contains(out, "a actions") {
		t.Fatalf("hints still shown under a live flash:\n%s", out)
	}
}

// TestPeekTaskMenuOpensAndDispatches verifies `a` in the task text peek opens
// the task menu for the previewed task and dispatches a filtered verb.
func TestPeekTaskMenuOpensAndDispatches(t *testing.T) {
	row := DashboardRow{ID: "set-peek", DefPath: "/def"}
	failedTask := tasks.Task{ID: "02-b", File: "02-b.md", Status: "failed"}
	m, completeCalls, resetCalls, _ := detailOverrideModel(row, failedTask, nil, nil, nil)
	// Open a peek over the previewed task.
	m.detail.peek = &itemTextPeek{itemID: "02-b", text: "body\n"}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.itemMenu == nil {
		t.Fatal("a in peek: expected task menu to open")
	}
	if !got.itemMenu.inPeek {
		t.Fatal("peek-opened menu should be marked inPeek")
	}
	if keys := itemMenuKeys(got.itemMenu); !slices.Contains(keys, "o") || slices.Contains(keys, "k") {
		t.Fatalf("peek failed-task menu = %v, want O and not K", keys)
	}
	// The peek stays open beneath the menu.
	if got.detail.peek == nil {
		t.Fatal("peek should remain open while menu is up")
	}

	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	got = updated.(QueueDashboard)
	if got.itemMenu != nil {
		t.Fatal("o should close the menu")
	}
	if cmd == nil {
		t.Fatal("o in peek menu: expected a command")
	}
	cmd()
	if *resetCalls != 1 {
		t.Fatalf("resetCalls = %d, want 1", *resetCalls)
	}
	if *completeCalls != 0 {
		t.Fatalf("completeCalls = %d, want 0", *completeCalls)
	}
}

// TestPeekTaskMenuRendersOverlay verifies the peek view renders the menu verbs.
func TestPeekTaskMenuRendersOverlay(t *testing.T) {
	row := DashboardRow{ID: "set-peek-render", DefPath: "/def"}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, _, _, _ := detailOverrideModel(row, openTask, nil, nil, nil)
	m.width = 120
	m.height = 14
	m.detail.peek = &itemTextPeek{itemID: "01-a", text: "body line\n"}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	out := got.View().Content
	for _, want := range []string{"actions", "c  complete", "s  skip"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered peek menu missing %q:\n%s", want, out)
		}
	}
}

// TestPeekFormerKeysInertWithoutMenu confirms C/O/K do nothing in the peek
// outside the menu (they act only through it).
func TestPeekFormerKeysInertWithoutMenu(t *testing.T) {
	row := DashboardRow{ID: "set-peek-inert", DefPath: "/def"}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, completeCalls, _, skipCalls := detailOverrideModel(row, openTask, nil, nil, nil)
	m.detail.peek = &itemTextPeek{itemID: "01-a", text: "body\n"}

	for _, key := range []rune{'c', 'o', 'k'} {
		updated, cmd := m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		got := updated.(QueueDashboard)
		if cmd != nil {
			t.Fatalf("%c in peek (no menu): expected no command", key)
		}
		if got.itemMenu != nil {
			t.Fatalf("%c in peek (no menu): should not open a menu", key)
		}
	}
	if *completeCalls != 0 || *skipCalls != 0 {
		t.Fatal("former direct keys should not dispatch in the peek")
	}
}

// TestDetailFormerKeysInertWithoutMenu confirms C/O/K do nothing in the detail
// list outside the menu.
func TestDetailFormerKeysInertWithoutMenu(t *testing.T) {
	row := DashboardRow{ID: "set-inert", DefPath: "/def"}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, completeCalls, resetCalls, skipCalls := detailOverrideModel(row, openTask, nil, nil, nil)

	for _, key := range []rune{'c', 'o', 'k'} {
		updated, cmd := m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		got := updated.(QueueDashboard)
		if cmd != nil {
			t.Fatalf("%c in detail (no menu): expected no command", key)
		}
		if got.itemMenu != nil {
			t.Fatalf("%c in detail (no menu): should not open a menu", key)
		}
	}
	if *completeCalls != 0 || *resetCalls != 0 || *skipCalls != 0 {
		t.Fatal("former direct keys should not dispatch in the detail list")
	}
}

// TestDetailTaskMenuRendersOverlay verifies the open menu's verbs render in the
// detail view, anchored under the cursored task.
func TestDetailTaskMenuRendersOverlay(t *testing.T) {
	row := DashboardRow{ID: "set-render", DefPath: "/def"}
	failedTask := tasks.Task{ID: "02-b", File: "02-b.md", Status: "failed", Type: "AFK", Title: "B"}
	m, _, _, _ := detailOverrideModel(row, failedTask, nil, nil, nil)
	m.width = 120
	m.height = 12
	m = openTaskMenu(t, m)
	out := m.View().Content
	for _, want := range []string{"actions", "c  complete", "o  open"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered detail menu missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "s  skip") {
		t.Fatalf("failed task menu should not offer skip:\n%s", out)
	}
}

func TestMainListRuntimeShell(t *testing.T) {
	// runtimeShell opens the action menu and dispatches the shell verb (`O`).
	runtimeShell := func(m QueueDashboard) (QueueDashboard, tea.Cmd) {
		updated, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		if got.menu == nil {
			t.Fatal("a did not open the action menu")
		}
		updated, cmd := got.update(tea.KeyPressMsg{Code: 'O', Text: "O"})
		return updated.(QueueDashboard), cmd
	}

	// The shell verb is the kind's own, so an unbound row's refusal is the Task-set
	// kind's wording, arriving through the verb seam rather than a dashboard flash.
	refuse := func(t *testing.T, m QueueDashboard) QueueDashboard {
		t.Helper()
		got, cmd := runtimeShell(m)
		if cmd == nil {
			t.Fatal("shell verb must return a command")
		}
		msg, ok := cmd().(dashboardKindVerbMsg)
		if !ok {
			t.Fatalf("shell cmd produced %T, want dashboardKindVerbMsg", cmd())
		}
		updated, cmd := got.update(msg)
		if cmd != nil {
			t.Fatal("a refused shell must hand off nowhere")
		}
		return updated.(QueueDashboard)
	}

	t.Run("O with empty runtimePath refuses in the kind's words", func(t *testing.T) {
		row := DashboardRow{ID: "set-x", DefPath: "/def", RuntimePath: ""}
		got := refuse(t, newQueueDashboard(nil, nil, DashboardSnapshot{Containers: []DashboardRow{row}}))
		want := `task set "set-x" has no checkout to open a shell in`
		if got.actionErr == nil || got.actionErr.Error() != want {
			t.Fatalf("action error = %v, want %q", got.actionErr, want)
		}
		if got.flash.Text() != "" {
			t.Fatalf("flash = %q, want the pending reassurance cleared by the refusal", got.flash.Text())
		}
	})

	t.Run("O with whitespace-only runtimePath refuses too", func(t *testing.T) {
		row := DashboardRow{ID: "set-y", DefPath: "/def", RuntimePath: "   "}
		got := refuse(t, newQueueDashboard(nil, nil, DashboardSnapshot{Containers: []DashboardRow{row}}))
		if got.actionErr == nil || !strings.Contains(got.actionErr.Error(), "no checkout to open a shell in") {
			t.Fatalf("action error = %v, want a shell refusal", got.actionErr)
		}
	})

	t.Run("a with no rows does not open the menu", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, cmd := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		if cmd != nil {
			t.Fatal("a with no rows: expected no cmd")
		}
		if got.menu != nil {
			t.Fatal("a with no rows: menu must not open")
		}
		if got.flash.Text() != "" {
			t.Fatalf("a with no rows: expected no flash, got %q", got.flash.Text())
		}
	})

	t.Run("flash hint rendered in view", func(t *testing.T) {
		row := DashboardRow{ID: "set-z", DefPath: "/def", RuntimePath: ""}
		m := newQueueDashboard(nil, nil, DashboardSnapshot{Containers: []DashboardRow{row}})
		m.flash.Set(dashboardNoCheckoutMessage)
		v := m.View()
		if !strings.Contains(v.Content, dashboardNoCheckoutMessage) {
			t.Fatalf("flash not rendered in view:\n%s", v.Content)
		}
	})

	t.Run("menu offers the shell verb", func(t *testing.T) {
		row := DashboardRow{ID: "set-hint", DefPath: "/def"}
		m := newQueueDashboard(nil, nil, DashboardSnapshot{Containers: []DashboardRow{row}})
		updated, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		view := got.View().Content
		if !menuHasKey(got.menu, "O") {
			t.Fatalf("menu missing shell verb: %+v", got.menu)
		}
		if !strings.Contains(view, "O  shell") {
			t.Fatalf("menu view missing 'O  shell':\n%s", view)
		}
	})
}

func TestQueueDashboardHelpOverlay(t *testing.T) {
	ctrlH := tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl}

	t.Run("C-h opens help in main list", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help overlay")
		}
	})

	t.Run("second C-h closes help", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Fatal("first C-h should open help")
		}
		updated, _ = got.Update(ctrlH)
		got = updated.(QueueDashboard)
		if got.showHelp {
			t.Error("second C-h should close help")
		}
	})

	t.Run("Esc closes help", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Fatal("C-h should open help")
		}
		updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		got = updated.(QueueDashboard)
		if got.showHelp {
			t.Error("Esc should close help")
		}
	})

	t.Run("help swallows other keys when open", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Fatal("C-h should open help")
		}
		// Try pressing 'j' which would normally move cursor
		updated, _ = got.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		got = updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("help should remain open when other keys pressed")
		}
	})

	t.Run("help works while typing a search", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
		got := updated.(QueueDashboard)
		if !got.searchTyping {
			t.Fatal("/ should start the typing phase")
		}
		updated, _ = got.Update(ctrlH)
		got = updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help while typing a search")
		}
	})

	t.Run("help works in detail view", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.detail = &detailView{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in detail view")
		}
	})

	t.Run("help works in peek mode", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.detail = &detailView{peek: &itemTextPeek{}}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in peek mode")
		}
	})

	t.Run("help works in action menu", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.menu = &dashboardMenu{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in action menu")
		}
	})

	t.Run("help works in item menu", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.itemMenu = &itemMenu{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in item menu")
		}
	})

	t.Run("help works in bind modal", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.bind = &dashboardBindModal{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in bind modal")
		}
	})

	t.Run("help works in drain picker", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.drainPick = &dashboardDrainModal{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in drain picker")
		}
	})

	t.Run("help works in abandon modal", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.abandon = &dashboardAbandonModal{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in abandon modal")
		}
	})

	t.Run("F1 does nothing", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
		got := updated.(QueueDashboard)
		if got.showHelp {
			t.Error("F1 should not open help")
		}
	})
}

func TestQueueDashboardHelpContent(t *testing.T) {
	t.Run("main list shows main bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		entries := m.helpEntries()
		if len(entries) == 0 {
			t.Fatal("main list should have help entries")
		}
		// Check for key bindings
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		required := []string{"j/k", "gg", "G", "l/enter", "y", "a", "A", "/", "h/esc"}
		for _, key := range required {
			if !found[key] {
				t.Errorf("main list help missing key: %s", key)
			}
		}
	})

	t.Run("search typing lists only the keys that are not text", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.searchTyping = true
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		for _, key := range []string{"typing", "enter", "esc", "ctrl+c"} {
			if !found[key] {
				t.Errorf("search help missing %q", key)
			}
		}
		// j/k and v are letters here, so promising them would be a lie.
		for _, key := range []string{"j/k", "v"} {
			if found[key] {
				t.Errorf("search help still offers %q, which is text while typing", key)
			}
		}
	})

	t.Run("filter menu shows preset bindings", func(t *testing.T) {
		m := filterMenuTestModel()
		updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
		m = updated.(QueueDashboard)
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		for _, key := range []string{"1-9", "j/k", "enter/space", "esc"} {
			if !found[key] {
				t.Errorf("filter menu help missing key: %s", key)
			}
		}
		if found["d"] || found["a"] {
			t.Error("filter menu help still lists retired toggle letters")
		}
	})

	t.Run("detail view shows detail bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.detail = &detailView{}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["l/enter"] {
			t.Error("detail view help missing 'l/enter'")
		}
		if !found["a"] {
			t.Error("detail view help missing 'a'")
		}
	})

	t.Run("peek mode shows peek bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.detail = &detailView{peek: &itemTextPeek{}}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["ctrl+d"] {
			t.Error("peek mode help missing 'ctrl+d'")
		}
		if !found["ctrl+u"] {
			t.Error("peek mode help missing 'ctrl+u'")
		}
	})

	t.Run("action menu shows menu verbs", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		// The help lists the menu that is open, so it is opened over a row the way a
		// keypress opens it — the verbs are the row's kind's, not a second list here.
		m.menu = newDashboardMenu(testKinds(), DashboardRow{ID: "set"}, false)
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		// Should show menu-specific verbs
		if !found["I"] {
			t.Error("action menu help missing 'I' (drain)")
		}
		if !found["b"] {
			t.Error("action menu help missing 'b' (bind)")
		}
		if !found["esc"] {
			t.Error("action menu help missing 'esc'")
		}
	})

	t.Run("item menu shows the open menu's verbs", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.itemMenu = newItemMenu(work.Item{ID: "01-a", Status: "open"}, testKinds().itemActionsFor(
			DashboardRow{ID: "set"}, work.Item{ID: "01-a", Status: "open"}), false)
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["c"] {
			t.Error("item menu help missing 'c' (complete)")
		}
		if !found["s"] {
			t.Error("item menu help missing 's' (skip)")
		}
		if !found["esc"] {
			t.Error("item menu help missing 'esc'")
		}
	})

	t.Run("bind modal shows bind bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.bind = &dashboardBindModal{}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["j/k"] {
			t.Error("bind modal help missing 'j/k'")
		}
		if !found["enter"] {
			t.Error("bind modal help missing 'enter'")
		}
	})

	t.Run("drain picker shows picker bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.drainPick = &dashboardDrainModal{}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["j/k"] {
			t.Error("drain picker help missing 'j/k'")
		}
		if !found["enter"] {
			t.Error("drain picker help missing 'enter'")
		}
	})

	t.Run("abandon modal shows abandon bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.abandon = &dashboardAbandonModal{}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["y"] {
			t.Error("abandon modal help missing 'y'")
		}
		if !found["enter/n/esc"] {
			t.Error("abandon modal help missing 'enter/n/esc'")
		}
	})
}

func TestQueueDashboardHelpRendering(t *testing.T) {
	m := newQueueDashboard(nil, nil, DashboardSnapshot{})
	m.width = 80
	m.height = 24
	m.showHelp = true
	view := m.View()

	// Check that help overlay is rendered
	if !strings.Contains(view.Content, "Help") {
		t.Error("help overlay should contain 'Help' title")
	}
	if !strings.Contains(view.Content, "C-h toggle") {
		t.Error("help overlay should contain 'C-h toggle' footer")
	}
	if !strings.Contains(view.Content, "Esc close") {
		t.Error("help overlay should contain 'Esc close' footer")
	}
}

func TestQueueDashboardHelpFooterHint(t *testing.T) {
	m := newQueueDashboard(nil, nil, DashboardSnapshot{})
	hint := m.mainHint()
	if !strings.Contains(hint, "C-h help") {
		t.Error("main footer hint should include 'C-h help'")
	}
}

// TestDashboardMainViewTwoLineIntegration wires two-line mode into the default
// Frame + List path: with a long set id every row renders two physical lines —
// the "PROJECT · SETID" identity (plus WORKTREE) on line 1 and STATUS on
// line 2 — and the view clamps to the terminal height instead of overflowing.
func TestDashboardMainViewTwoLineIntegration(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00" + longID, RawStatus: tasks.StatusReady, ID: longID},
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00bbb", RawStatus: tasks.StatusDone, ID: "bbb"},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(QueueDashboard)

	if !dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
		t.Fatalf("expected two-line mode with a long set id")
	}
	if m.list.LinesPerItem() != 2 {
		t.Fatalf("LinesPerItem = %d, want 2", m.list.LinesPerItem())
	}

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("view line count = %d, want %d (clamped to terminal height):\n%s", got, want, view)
	}

	// The line-1 header labels the identity and WORKTREE columns (the DRAIN column
	// is retired, ADR-0111); the line-2 header labels STATUS.
	headerIdx := dashboardTestLineIndex(lines, "TASK SET")
	if headerIdx < 0 {
		t.Fatalf("two-line header missing TASK SET:\n%s", view)
	}
	header := lines[headerIdx]
	for _, want := range []string{"TASK SET", "WORKTREE"} {
		if !strings.Contains(header, want) {
			t.Fatalf("two-line line-1 header missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(header, "DRAIN") {
		t.Fatalf("two-line line-1 header must not carry the retired DRAIN column:\n%s", view)
	}
	if !strings.Contains(lines[headerIdx+1], "STATUS") {
		t.Fatalf("two-line line-2 header missing STATUS:\n%s", view)
	}

	// The data row's first physical line carries PROJECT and the set id; STATUS
	// appears on the following physical line, not line 1.
	idIdx := dashboardTestLineIndex(lines, longID)
	if idIdx < 0 {
		t.Fatalf("set id %q missing from view:\n%s", longID, view)
	}
	if !strings.Contains(lines[idIdx], "pop") {
		t.Fatalf("row line 1 must carry the project:\n%s", view)
	}
	if strings.Contains(lines[idIdx], "READY") {
		t.Fatalf("row line 1 must not contain the status:\n%s", view)
	}
	if !strings.Contains(lines[idIdx+1], "READY") {
		t.Fatalf("row line 2 must contain the status READY:\n%s", view)
	}
}

// TestDashboardTwoLineCursorMovesByLogicalRow confirms that j/k/gg/G move the
// cursor one logical task-set row at a time even though each row renders two
// physical terminal lines.
func TestDashboardTwoLineCursorMovesByLogicalRow(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := make([]DashboardRow, 5)
	for i := range rows {
		id := fmt.Sprintf("%s-%d", longID, i)
		rows[i] = DashboardRow{Project: "pop", CursorKey: "pop\x00" + id, RawStatus: tasks.StatusReady, ID: id}
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m = updated.(QueueDashboard)

	if m.list.LinesPerItem() != 2 {
		t.Fatalf("LinesPerItem = %d, want 2", m.list.LinesPerItem())
	}

	assertCursor := func(name string, want int) {
		t.Helper()
		if m.list.Cursor() != want {
			t.Fatalf("%s: cursor = %d, want %d", name, m.list.Cursor(), want)
		}
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(QueueDashboard)
	assertCursor("j", 1)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(QueueDashboard)
	assertCursor("jj", 2)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m = updated.(QueueDashboard)
	assertCursor("jjk", 1)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	m = updated.(QueueDashboard)
	assertCursor("G", len(rows)-1)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m = updated.(QueueDashboard)
	assertCursor("first g", len(rows)-1)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m = updated.(QueueDashboard)
	assertCursor("gg", 0)
}

// TestDashboardTwoLineClampsToBodyHeight asserts that many rows on a short
// terminal do not overflow the viewport in two-line mode. Each logical item
// consumes two physical lines, so the visible logical row count is halved, but
// the total rendered line count still equals the terminal height.
func TestDashboardTwoLineClampsToBodyHeight(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := make([]DashboardRow, 40)
	for i := range rows {
		id := fmt.Sprintf("%s-%02d", longID, i)
		rows[i] = DashboardRow{Project: "pop", CursorKey: "pop\x00" + id, RawStatus: tasks.StatusReady, ID: id}
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	// Height at the two-line floor (16): roomy enough for two-line mode, still
	// short enough that 40 rows overflow and must clamp to the viewport.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 16})
	m = updated.(QueueDashboard)

	if m.list.LinesPerItem() != 2 {
		t.Fatalf("LinesPerItem = %d, want 2", m.list.LinesPerItem())
	}

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("view line count = %d, want %d (clamped to body height):\n%s", got, want, view)
	}

	// In two-line mode the chrome consumes an extra line for the second header
	// (blank, line-1 header, line-2 STATUS header, separator), and each logical
	// row occupies two physical lines.
	bodyH := m.frameSpec().BodyHeight(m.height) - dashboardTwoLineChromeLines
	visible := m.list.VisibleRows()
	if got := len(visible); got != bodyH {
		t.Fatalf("List VisibleRows = %d, want %d", got, bodyH)
	}
	selected, ok := m.list.Selected()
	if !ok {
		t.Fatal("expected a selected row")
	}
	if selected.ID != rows[0].ID {
		t.Fatalf("selected = %q, want %q", selected.ID, rows[0].ID)
	}
}

// TestDashboardShortPaneCollapsesToSingleLine asserts that a pane below the
// two-line height floor renders single-line rows even when the terminal is
// narrow and a set id is long — a short tmux popup trades id completeness for
// visible-row density (ADR-0107). The collapse must hold in both the main body
// and the action-menu overlay.
func TestDashboardShortPaneCollapsesToSingleLine(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00" + longID, RawStatus: tasks.StatusReady, ID: longID},
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00bbb", RawStatus: tasks.StatusDone, ID: "bbb"},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	// A wide pane (width >= 120) with a long id would force two-line mode were the
	// pane roomy — the id, not the width, is the trigger. But the height is one
	// row below the floor, so the table stays single-line. The width is wide
	// enough that the single-line header renders in full for the assertions.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 130, Height: dashboardTwoLineHeightFloor - 1})
	m = updated.(QueueDashboard)

	if dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
		t.Fatalf("short pane must not activate two-line mode")
	}
	if m.list.LinesPerItem() != 1 {
		t.Fatalf("LinesPerItem = %d, want 1 on a short pane", m.list.LinesPerItem())
	}
	// In single-line mode the id and its status share one physical line; in
	// two-line mode the status would sit on the following line instead.
	assertSingleLineRow := func(view, label string) {
		t.Helper()
		lines := strings.Split(view, "\n")
		idx := dashboardTestLineIndex(lines, longID)
		if idx < 0 {
			t.Fatalf("%s: set id %q missing from render:\n%s", label, longID, view)
		}
		if !strings.Contains(lines[idx], "READY") {
			t.Fatalf("%s: status must share the id's line in single-line mode:\n%s", label, view)
		}
	}
	assertSingleLineRow(m.View().Content, "main body")

	// The action-menu overlay must share the same single-line decision.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)
	if m.menu == nil {
		t.Fatal("a did not open the action menu")
	}
	assertSingleLineRow(m.View().Content, "menu overlay")
}

// TestDashboardMenuTwoLineOverlay verifies that opening the action menu (`a`)
// on a narrow pane renders the table rows in two-line mode and anchors the menu
// relative to the cursor's two-line block.
func TestDashboardMenuTwoLineOverlay(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00" + longID, RawStatus: tasks.StatusReady, ID: longID},
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00bbb", RawStatus: tasks.StatusDone, ID: "bbb"},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(QueueDashboard)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)
	if m.menu == nil {
		t.Fatal("a did not open the action menu")
	}

	view := m.View().Content
	lines := strings.Split(view, "\n")

	if !dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
		t.Fatalf("expected two-line mode with a long set id")
	}

	// The set id rides on line 1 (the identity); STATUS follows on line 2.
	idx := dashboardTestLineIndex(lines, longID)
	if idx < 0 {
		t.Fatalf("set id %q missing from menu overlay:\n%s", longID, view)
	}
	if !strings.Contains(lines[idx+1], "READY") {
		t.Fatalf("row line 2 must contain the status:\n%s", view)
	}

	// The menu must be rendered with the "actions" caption.
	if !strings.Contains(view, "actions") {
		t.Fatalf("menu caption not rendered:\n%s", view)
	}

	// No rendered line may exceed the terminal width.
	for i, line := range lines {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("line %d exceeds terminal width (%d > %d): %q", i, lipgloss.Width(line), m.width, line)
		}
	}
}

// TestDashboardBindModalTwoLineOverlay verifies that the bind modal renders the
// table above its body in two-line mode on a narrow pane without spilling past
// the terminal width.
func TestDashboardBindModalTwoLineOverlay(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00" + longID, RawStatus: tasks.StatusReady, ID: longID},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(QueueDashboard)

	// Inject a bind modal directly so the test does not depend on filesystem/git.
	m.bind = &dashboardBindModal{
		row:   rows[0],
		stage: dashboardBindStageWorktree,
		list: newBindEntryList([]drain.BindEntry{
			{Label: "existing worktree"},
			{Label: "create new"},
		}),
	}

	view := m.View().Content
	lines := strings.Split(view, "\n")

	if !dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
		t.Fatalf("expected two-line mode with a long set id")
	}

	// The set id rides on line 1 (the identity); STATUS follows on line 2.
	idx := dashboardTestLineIndex(lines, longID)
	if idx < 0 {
		t.Fatalf("set id %q missing from bind modal overlay:\n%s", longID, view)
	}
	if !strings.Contains(lines[idx+1], "READY") {
		t.Fatalf("row line 2 must contain the status:\n%s", view)
	}

	// No rendered line may exceed the terminal width (no horizontal spill).
	for i, line := range lines {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("line %d exceeds terminal width (%d > %d): %q", i, lipgloss.Width(line), m.width, line)
		}
	}

	// The modal body must be rendered below the table.
	if !strings.Contains(view, "Bind worktree") {
		t.Fatalf("bind modal body not rendered:\n%s", view)
	}
}

// TestDashboardDrainModalTwoLineOverlay verifies that the drain target modal
// renders the table above its body in two-line mode on a narrow pane.
func TestDashboardDrainModalTwoLineOverlay(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00" + longID, RawStatus: tasks.StatusReady, ID: longID},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(QueueDashboard)

	m.drainPick = newDashboardDrainModal(m.d, rows[0], []drain.DrainEntry{
		{Label: "new managed worktree"},
		{Label: "trunk"},
	}, "")

	view := m.View().Content
	lines := strings.Split(view, "\n")

	if !dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
		t.Fatalf("expected two-line mode with a long set id")
	}

	idx := dashboardTestLineIndex(lines, longID)
	if idx < 0 {
		t.Fatalf("set id %q missing from drain modal overlay:\n%s", longID, view)
	}
	if !strings.Contains(lines[idx+1], "READY") {
		t.Fatalf("row line 2 must contain the status:\n%s", view)
	}

	for i, line := range lines {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("line %d exceeds terminal width (%d > %d): %q", i, lipgloss.Width(line), m.width, line)
		}
	}

	if !strings.Contains(view, "Drain target") {
		t.Fatalf("drain modal body not rendered:\n%s", view)
	}
}

// TestDashboardSearchReevaluatesTwoLineMode verifies that a search re-evaluates
// two-line mode against the narrowed row set: starting with a mix that triggers
// two-line mode and searching down to short ids deactivates it; clearing the
// search reactivates it.
func TestDashboardSearchReevaluatesTwoLineMode(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00short", RawStatus: tasks.StatusReady, ID: "short"},
		{Project: "pop", Worktree: "main", CursorKey: "pop\x00" + longID, RawStatus: tasks.StatusReady, ID: longID},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	// Width at the forced-fit threshold (120): only the long set id, not width,
	// may trigger two-line mode, so filtering it away must drop back to one line.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = updated.(QueueDashboard)

	if !dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
		t.Fatalf("expected two-line mode initially because of the long set id")
	}
	if m.list.LinesPerItem() != 2 {
		t.Fatalf("LinesPerItem = %d, want 2 before filter", m.list.LinesPerItem())
	}

	// Search for a query that matches only the short id.
	m = typeSearch(m, "short", true)
	if m.searchTerm != "short" {
		t.Fatalf("search term = %q, want 'short'", m.searchTerm)
	}

	if len(m.snap.Containers) != 1 {
		t.Fatalf("filtered rows = %d, want 1", len(m.snap.Containers))
	}
	if dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
		t.Fatalf("expected single-line mode after filtering to short id")
	}

	// A render must update LinesPerItem to match the filtered rows.
	_ = m.View()
	if m.list.LinesPerItem() != 1 {
		t.Fatalf("LinesPerItem = %d, want 1 after filtering to short id", m.list.LinesPerItem())
	}

	// Clear the search: two-line mode must return because the long id is back.
	m = typeSearch(m, "", true)
	if m.searchTerm != "" {
		t.Fatalf("search term = %q, want it cleared", m.searchTerm)
	}
	if !dashboardTwoLineMode(m.snap.Containers, m.width, m.height) {
		t.Fatalf("expected two-line mode after clearing filter")
	}
	_ = m.View()
	if m.list.LinesPerItem() != 2 {
		t.Fatalf("LinesPerItem = %d, want 2 after clearing filter", m.list.LinesPerItem())
	}
}

// TestDashboardStatusAppendsAutoDrain confirms the status-label assembly appends
// a plain-text ` · auto-drain` suffix for an auto-drain row (after the yellow
// verify suffix), and nothing for a non-auto-drain row.
func TestDashboardStatusAppendsAutoDrain(t *testing.T) {
	ad := dashboardStatusCellStyled(testKinds(), DashboardRow{RawStatus: tasks.StatusReady, AutoDrain: true})
	// The base label is bucket-colored (READY → blue); the marker follows plainly.
	if !strings.Contains(ad, "\x1b[34mREADY\x1b[m · auto-drain") {
		t.Fatalf("auto-drain suffix missing/misplaced: %q", ad)
	}
	if plain := dashboardStatusCellStyled(testKinds(), DashboardRow{RawStatus: tasks.StatusReady}); strings.Contains(plain, "auto-drain") {
		t.Fatalf("non-auto-drain row should not carry suffix: %q", plain)
	}

	// The yellow verify suffix must still render and precede the uncolored
	// auto-drain suffix: <label> · verified @ <sha> · auto-drain.
	ordered := dashboardStatusCellStyled(testKinds(), DashboardRow{VerifyMark: tasks.VerifyMarkVerified, VerifiedAtSHA: "abcdef1234567890", VerifiedAtDrifted: true, RawStatus: tasks.StatusAwaitingApproval, AutoDrain: true})
	vIdx := strings.Index(ordered, "verified @")
	aIdx := strings.Index(ordered, "auto-drain")
	if vIdx < 0 || aIdx < 0 || vIdx > aIdx {
		t.Fatalf("verify suffix must precede auto-drain: %q", ordered)
	}
	if !strings.Contains(ordered, "\x1b[33m") {
		t.Fatalf("verify suffix should stay yellow: %q", ordered)
	}
	if !strings.Contains(ordered, " · auto-drain") {
		t.Fatalf("auto-drain suffix should be uncolored plain text: %q", ordered)
	}
}

// TestDashboardStatusSuffixesRender pins that the STATUS cell's ` · auto-drain ·
// orphaned` suffixes (composed by the work data core, ADR-0143) survive intact
// through both the single-line and two-line render modes off the one precomputed
// status string. The suffix derivation itself is proven in work's tests; this is
// the queue-side render half.
func TestDashboardStatusSuffixesRender(t *testing.T) {
	both := DashboardRow{
		Project:   "pop",
		ID:        "both",
		RawStatus: tasks.StatusBlocked,
		AutoDrain: true,
		Orphaned:  true,
		Worktree:  "both-branch",
		DestKind:  work.DestBound,
	}
	// Both render modes read the same precomputed status (composition itself is
	// pinned in work.TestStatusCellComposition); widths are wide enough
	// that no truncation clips the suffixes. Column order: PROJECT, TASK SET,
	// STATUS (index 2, given the width), WORKTREE, indicator.
	widths := []int{20, 20, 60, 20, 20}
	single := dashboardTableLine(dashboardRowValues(testKinds(), both, livePaneCache{}), widths)
	if !strings.Contains(single, "· auto-drain · orphaned") {
		t.Fatalf("single-line render missing suffixes:\n%s", single)
	}
	twoLine := dashboardTwoLineRowLine2(testKinds(), both, []int{10, 10, 10, 10})
	if !strings.Contains(twoLine, "· auto-drain · orphaned") {
		t.Fatalf("two-line render missing suffixes:\n%s", twoLine)
	}
}

// TestDashboardParkedAndConfigErrorSuffixes covers ADR-0111's relocation of the
// old DRAIN `parked` / `config error: <msg>` values onto the STATUS cell: each
// renders as a plain ` · parked` / ` · config error: <msg>` suffix in every
// status and both row layouts, the DRAIN string no longer carries either, the
// two compose cleanly after verified/auto-drain/orphaned, and the styled cell's
// measured width matches the plain form (no ANSI leaks into column math).
func TestDashboardParkedAndConfigErrorSuffixes(t *testing.T) {
	// Column order: PROJECT, TASK SET, STATUS, WORKTREE, indicator — STATUS (index
	// 2) is given ample width so its suffixes are not truncated at render.
	statusW := []int{10, 10, 80, 10, 10}

	for _, status := range []tasks.TaskSetStatus{tasks.StatusReady, tasks.StatusBlocked, tasks.StatusAwaitingApproval, tasks.StatusFailed} {
		row := DashboardRow{RawStatus: status, Parked: true}
		single := dashboardTableLine(dashboardRowValues(testKinds(), row, livePaneCache{}), statusW)
		if !strings.Contains(single, "· parked") {
			t.Fatalf("status %s single-line parked render missing suffix:\n%s", status, single)
		}
		twoLine := dashboardTwoLineRowLine2(testKinds(), row, []int{10, 10, 10, 10})
		if !strings.Contains(twoLine, "· parked") {
			t.Fatalf("status %s two-line parked render missing suffix:\n%s", status, twoLine)
		}
	}

	const msg = "no trunk worktree configured"
	ce := DashboardRow{RawStatus: tasks.StatusReady, ConfigError: msg}
	if ce.LiveDrain {
		t.Fatalf("config-error row LiveDrain = true, want false (config error is not a live drain)")
	}
	single := dashboardTableLine(dashboardRowValues(testKinds(), ce, livePaneCache{}), statusW)
	if !strings.Contains(single, "· config error: "+msg) {
		t.Fatalf("single-line config-error render missing suffix:\n%s", single)
	}
	twoLine := dashboardTwoLineRowLine2(testKinds(), ce, []int{10, 10, 10, 10})
	if !strings.Contains(twoLine, "· config error: "+msg) {
		t.Fatalf("two-line config-error render missing suffix:\n%s", twoLine)
	}

	// The five-suffix compose order (verified/auto-drain/orphaned/parked/config
	// error) is pinned in work.TestStatusCellComposition; here the styled cell
	// must measure the same width as the plain cell (no ANSI leaks into column
	// math).
	multi := DashboardRow{
		VerifiedAtSHA: "abcdef123456",
		RawStatus:     tasks.StatusReady,
		AutoDrain:     true,
		Orphaned:      true,
		Parked:        true,
		ConfigError:   "no trunk",
	}
	plain := dashboardStatusCellText(testKinds(), multi)
	if styled := dashboardStatusCellStyled(testKinds(), multi); lipgloss.Width(styled) != lipgloss.Width(plain) {
		t.Fatalf("styled width %d != plain width %d (ANSI leaked into width math)", lipgloss.Width(styled), lipgloss.Width(plain))
	}
}

// withWayfinderMaps overlays wayfinder map folders onto a dashboard test drain.Deps'
// mock FS so dashboardRowsForStatic can discover Map rows alongside Task sets.
func withWayfinderMaps(t *testing.T, d *drain.Deps, storageDir string, files map[string]string) {
	t.Helper()
	fs := d.Tasks.FS.(*deps.MockFileSystem)
	origReadFile := fs.ReadFileFunc
	origReadDir := fs.ReadDirFunc
	fs.ReadDirFunc = func(path string) ([]os.DirEntry, error) {
		entries := mapDirEntries(path, files)
		if entries != nil {
			return entries, nil
		}
		if origReadDir != nil {
			return origReadDir(path)
		}
		return nil, os.ErrNotExist
	}
	fs.ReadFileFunc = func(path string) ([]byte, error) {
		if content, ok := files[path]; ok {
			return []byte(content), nil
		}
		if origReadFile != nil {
			return origReadFile(path)
		}
		return nil, os.ErrNotExist
	}
	_ = storageDir // storageDir is encoded in the file paths callers pass
}

func mapDirEntries(path string, files map[string]string) []os.DirEntry {
	children := map[string]bool{}
	for filePath := range files {
		if !strings.HasPrefix(filePath, path+string(os.PathSeparator)) && filePath != path {
			continue
		}
		rel := strings.TrimPrefix(filePath, path+string(os.PathSeparator))
		if rel == "" || rel == filePath {
			continue
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		name := parts[0]
		children[name] = len(parts) > 1 || children[name]
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

func TestDashboardMapRowQueueVerbsInert(t *testing.T) {
	mapRow := DashboardRow{
		Project: "pop", Kind: ref.KindMap, CursorKey: "pop\x00map\x00demo",
		ID: "demo", MapOpen: 1, MapFrontier: 1,
	}
	setRow := DashboardRow{
		Project: "pop", CursorKey: "pop\x00set",
		RawStatus: tasks.StatusReady, ID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json",
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{mapRow, setRow}})
	m.width, m.height = 120, 40
	m.list.SetCursor(0)

	// A Map's menu is its own kind's: the four frontier verbs (going and staying),
	// the Map-scoped assist session, its own status submenu, and the shared ones —
	// spawning keys before in-place ones. Every Task-set verb stays absent — queue
	// verbs have never applied to a Map, and the status opener it shares with a task
	// set is the surface's verb, not that kind's. Mute is shared too (ADR-0200
	// decision 7): the fixture's Map is unmuted, so only the opener shows.
	items := dashboardMenuItems(testKinds(), mapRow)
	var keys []string
	for _, item := range items {
		keys = append(keys, item.key)
	}
	if want := []string{"I", "A", "S", "O", "m", "i", "a", "s", "y"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("map menu keys = %v, want %v", keys, want)
	}
	for _, item := range items {
		switch item.verb {
		case setkind.VerbDrain, setkind.VerbBind, setkind.VerbUnbind, setkind.VerbAutoDrain,
			setkind.VerbAssist, setkind.VerbArchive:
			t.Fatalf("map menu offers the Task-set verb %q", item.verb)
		}
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a on map row did not open action menu")
	}

	// Former direct bind/unbind keys stay inert at top level on map rows too.
	for _, key := range []string{"b", "u"} {
		updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		got = updated.(QueueDashboard)
		updated, _ = got.Update(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
		got = updated.(QueueDashboard)
		if got.bind != nil || got.abandon != nil || got.menu != nil {
			t.Fatalf("%s on map row opened a modal", key)
		}
	}

	// Set rows still open the action menu on a.
	got.list.SetCursor(1)
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got = updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a on set row did not open action menu")
	}
}

func TestDashboardMapRowTwoLineRender(t *testing.T) {
	row := DashboardRow{
		Project: "pop", Kind: ref.KindMap, ID: "2026-07-01-wayfinding-map",
		MapOpen: 3, MapFrontier: 2,
	}
	widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths([]DashboardRow{row}), 120)
	line1 := dashboardTwoLineRowLine1(row, widths, livePaneCache{})
	line2 := dashboardTwoLineRowLine2(testKinds(), row, widths)
	if !strings.Contains(line1, "pop") || !strings.Contains(line1, "2026-07-01-wayfinding-map") {
		t.Fatalf("two-line line1 missing project/map id: %q", line1)
	}
	// WORKTREE blank: line1 should not carry needs-bind.
	if strings.Contains(line1, work.DestLabelNeedsBind) {
		t.Fatalf("two-line line1 shows needs bind for map: %q", line1)
	}
	if !strings.Contains(line2, "3 open / 2 frontier") {
		t.Fatalf("two-line line2 STATUS = %q", line2)
	}
	wantIndent := dashboardTwoLineStatusIndent(widths)
	if got := len(line2) - len(strings.TrimLeft(line2, " ")); got < wantIndent {
		t.Fatalf("two-line line2 indent = %d, want >= %d: %q", got, wantIndent, line2)
	}
}

func mapDetailTestFiles() (storageDir string, files map[string]string) {
	storageDir = "/data/repos/repo-map-detail"
	activeMap := filepath.Join(storageDir, "maps", "2026-07-01-active")
	files = map[string]string{
		filepath.Join(activeMap, "map.md"): "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(activeMap, "issues", "01-frontier.md"): "" +
			"Type: research\nStatus: open\n\n# Frontier question\n",
		filepath.Join(activeMap, "issues", "02-blocked.md"): "" +
			"Type: prototype\nStatus: open\nBlocked by: 01\n\n# Blocked question\n",
		filepath.Join(activeMap, "issues", "03-grilling.md"): "" +
			"Type: grilling\nStatus: open\n\n# Grilling question\n",
		filepath.Join(activeMap, "issues", "04-resolved.md"): "" +
			"Type: task\nStatus: resolved\n\n# Resolved question\n",
	}
	return storageDir, files
}

// newMapDetailDashboard builds a dashboard whose one row is the Map container
// the Map kind loads from the fixture — items, sections and all, the same
// container the detail view reads in production.
func newMapDetailDashboard(t *testing.T) (QueueDashboard, *drain.Deps) {
	t.Helper()
	storageDir, files := mapDetailTestFiles()
	tasksDir := filepath.Join(storageDir, "tasks")
	d := dashboardTestDeps(t, nil, nil)
	withWayfinderMaps(t, d, storageDir, files)
	cfg := &config.Config{}
	groups := func() ([]repogroup.Group, error) {
		return []repogroup.Group{{
			DefPath:     tasksDir,
			StorageDir:  storageDir,
			RepoKey:     "repo-map-detail",
			ProjectName: "pop",
			Rep:         &repogroup.Checkout{ProjectPath: "/repo/checkout"},
		}}, nil
	}
	rows, err := wayfinder.NewMapKind(d.MapKindDeps(cfg, groups)).Load()
	if err != nil {
		t.Fatalf("map kind Load: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("loaded %d map containers, want 1", len(rows))
	}
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: rows})
	m.width, m.height = 120, 24
	return m, d
}

// openMapDetail presses `l` on the Map row and returns the opened detail.
func openMapDetail(t *testing.T, m QueueDashboard) QueueDashboard {
	t.Helper()
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)
	if got.detail == nil {
		t.Fatal("l on map row did not open the detail")
	}
	if cmd != nil {
		t.Fatal("opening a detail reads the container in hand, not a fresh load")
	}
	return got
}

func TestDashboardMapDetailViewOpenClose(t *testing.T) {
	m, _ := newMapDetailDashboard(t)

	got := openMapDetail(t, m)
	view := got.View().Content
	if strings.Contains(view, "PROJECT") || strings.Contains(view, "TASK SET") {
		t.Fatalf("detail view should replace table:\n%s", view)
	}
	if !strings.Contains(view, "Map · 2026-07-01-active") {
		t.Fatalf("detail missing map header:\n%s", view)
	}

	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{name: "h", msg: tea.KeyPressMsg{Code: 'h', Text: "h"}},
		{name: "left", msg: tea.KeyPressMsg{Code: tea.KeyLeft, Text: "left"}},
		{name: "esc", msg: tea.KeyPressMsg{Code: tea.KeyEscape}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mOpen, _ := newMapDetailDashboard(t)
			loaded := openMapDetail(t, mOpen)
			updated, cmd := loaded.Update(tc.msg)
			closed := updated.(QueueDashboard)
			if cmd != nil {
				t.Fatalf("%s in map detail returned command", tc.name)
			}
			if closed.detail != nil {
				t.Fatalf("%s should close map detail", tc.name)
			}
		})
	}

	m3, _ := newMapDetailDashboard(t)
	updated3, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	opened := updated3.(QueueDashboard)
	if opened.detail == nil || len(opened.detail.row.Items) == 0 {
		t.Fatalf("enter on map row should open the detail over its tickets: %+v", opened.detail)
	}
}

// TestDashboardMapDetailRendersSectionsAndTickets covers the generic detail over
// a Map: the kind's prose sections render above the item list, and its drain.Decision
// tickets are the items, each with its kind-local status and its blockers.
func TestDashboardMapDetailRendersSectionsAndTickets(t *testing.T) {
	m, _ := newMapDetailDashboard(t)
	got := openMapDetail(t, m)
	view := got.View().Content

	for _, want := range []string{
		"Destination", "Ship it",
		"01", "research", "open",
		"02", "prototype",
		"03", "grilling",
		"04", "resolved",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail missing %q:\n%s", want, view)
		}
	}
	lines := strings.Split(view, "\n")
	sectionLine := dashboardTestLineIndex(lines, "Ship it")
	tableLine := dashboardTestLineIndex(lines, "STATUS")
	if sectionLine < 0 || tableLine < 0 || sectionLine > tableLine {
		t.Fatalf("sections must render above the item table (section=%d table=%d):\n%s", sectionLine, tableLine, view)
	}
	blockedLine := ""
	for _, line := range lines {
		if strings.Contains(line, " 02 ") {
			blockedLine = line
		}
	}
	if !strings.HasSuffix(strings.TrimRight(blockedLine, " "), "01") {
		t.Fatalf("blocked ticket should name its blocker in BLOCKED-BY: %q", blockedLine)
	}
}

// TestDashboardMapDetailTitleFallsBackToSlugWhenManifestTitleEmpty covers a
// folded legacy Map, whose manifest tickets carry "title": "" because the
// pre-manifest header lines never named one: the detail view's TITLE column
// must fall back to the ticket's slug. It is the slug alone and not the NN-slug
// display name map.md uses, because this table prints the id in its own column
// already. A ticket that does carry a manifest title renders it unchanged.
func TestDashboardMapDetailTitleFallsBackToSlugWhenManifestTitleEmpty(t *testing.T) {
	storageDir := "/data/repos/repo-map-title-fallback"
	activeMap := filepath.Join(storageDir, "maps", "2026-07-01-active")
	files := map[string]string{
		filepath.Join(activeMap, "map.md"):                   "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(activeMap, "issues", "01-frontier.md"): "## Question\nWhat now?\n",
		filepath.Join(activeMap, "issues", "02-titled.md"):   "## Question\nWhat next?\n",
		filepath.Join(activeMap, "index.json"): `{"tickets":[` +
			`{"id":"01","file":"01-frontier.md","title":"","type":"research","status":"open"},` +
			`{"id":"02","file":"02-titled.md","title":"Custom title","type":"research","status":"open"}` +
			`]}`,
	}
	tasksDir := filepath.Join(storageDir, "tasks")
	d := dashboardTestDeps(t, nil, nil)
	withWayfinderMaps(t, d, storageDir, files)
	cfg := &config.Config{}
	groups := func() ([]repogroup.Group, error) {
		return []repogroup.Group{{
			DefPath:     tasksDir,
			StorageDir:  storageDir,
			RepoKey:     "repo-map-title-fallback",
			ProjectName: "pop",
			Rep:         &repogroup.Checkout{ProjectPath: "/repo/checkout"},
		}}, nil
	}
	rows, err := wayfinder.NewMapKind(d.MapKindDeps(cfg, groups)).Load()
	if err != nil {
		t.Fatalf("map kind Load: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("loaded %d map containers, want 1", len(rows))
	}

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: rows})
	m.width, m.height = 120, 24
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)
	view := got.View().Content

	var line01, line02 string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, " 01 ") {
			line01 = line
		}
		if strings.Contains(line, " 02 ") {
			line02 = line
		}
	}
	if !strings.Contains(line01, "frontier") {
		t.Fatalf("empty manifest title should render the slug fallback in TITLE:\n%q\nfull view:\n%s", line01, view)
	}
	if strings.Contains(line01, "01-frontier") {
		t.Fatalf("slug fallback must not repeat the id already in the ID column:\n%q\nfull view:\n%s", line01, view)
	}
	if !strings.Contains(line02, "Custom title") {
		t.Fatalf("manifest title should render unchanged in TITLE:\n%q\nfull view:\n%s", line02, view)
	}
}

func TestDashboardMapDetailViewVimNavigation(t *testing.T) {
	m, _ := newMapDetailDashboard(t)
	got := openMapDetail(t, m)

	selID := func(m QueueDashboard) string {
		item, _ := m.detail.list.Selected()
		return item.ID
	}

	updated, _ := got.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "02" {
		t.Fatalf("after j: selected = %q, want 02", id)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "04" {
		t.Fatalf("G: selected = %q, want 04", id)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "01" {
		t.Fatalf("gg: selected = %q, want 01", id)
	}
}

func TestDashboardMapDetailViewPeekTicketText(t *testing.T) {
	m, _ := newMapDetailDashboard(t)
	got := openMapDetail(t, m)

	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got = updated.(QueueDashboard)
	if got.detail.peek == nil || !got.detail.peek.loading || got.detail.peek.itemID != "01" {
		t.Fatalf("peek = %+v, want loading for 01", got.detail.peek)
	}
	if cmd == nil {
		t.Fatal("l in map detail did not return load command")
	}
	updated, _ = got.Update(cmd())
	got = updated.(QueueDashboard)
	if got.detail.peek == nil || got.detail.peek.loading || got.detail.peek.err != nil {
		t.Fatalf("loaded peek = %+v", got.detail.peek)
	}
	view := got.View().Content
	ticketPath := filepath.Join("/data/repos/repo-map-detail/maps/2026-07-01-active/issues/01-frontier.md")
	for _, want := range []string{
		"2026-07-01-active / 01",
		ticketPath,
		"# Frontier question",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("peek view missing %q:\n%s", want, view)
		}
	}

	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	got = updated.(QueueDashboard)
	if cmd != nil {
		t.Fatal("h from ticket peek returned command")
	}
	if got.detail == nil || got.detail.peek != nil {
		t.Fatalf("h should close peek but keep map detail: detail=%+v", got.detail)
	}
	if !strings.Contains(got.View().Content, "Map · 2026-07-01-active") {
		t.Fatalf("should return to map detail list after closing peek")
	}
}

// TestDashboardActionErrorSticky covers the sticky row-verb error (task
// 04-sticky-dashboard-errors): a refused action's message survives the periodic
// reload tick, is replaced by a newer error, and is cleared by the next keypress.
func TestDashboardActionErrorSticky(t *testing.T) {
	row := TestDashboardRow("proj", "set-1", DashboardRow{ID: "set-1", RuntimePath: "/repo/wt", Bound: true})
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 40

	// A refused unbind surfaces its full message.
	const refusal = "runtime checkout is locked for another set"
	updated, _ := m.Update(dashboardAbandonMsg{err: errors.New(refusal)})
	got := updated.(QueueDashboard)
	if got.actionErr == nil || got.actionErr.Error() != refusal {
		t.Fatalf("action error = %v, want %q", got.actionErr, refusal)
	}
	if view := got.View().Content; !strings.Contains(view, refusal) {
		t.Fatalf("view missing error message; got:\n%s", view)
	}

	// A background reload tick (its dashboardRowsMsg result, err nil) must not
	// clear the sticky action error.
	updated, _ = got.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: []DashboardRow{row}}})
	got = updated.(QueueDashboard)
	if got.actionErr == nil || got.actionErr.Error() != refusal {
		t.Fatalf("reload cleared sticky action error: %v", got.actionErr)
	}
	if view := got.View().Content; !strings.Contains(view, refusal) {
		t.Fatalf("view lost error after reload; got:\n%s", view)
	}

	// A newer action error replaces the message.
	const newer = "no drain target available for set-1"
	updated, _ = got.Update(dashboardKindVerbMsg{row: row, verb: setkind.VerbArchive, err: errors.New(newer)})
	got = updated.(QueueDashboard)
	if got.actionErr == nil || got.actionErr.Error() != newer {
		t.Fatalf("newer error did not replace: %v", got.actionErr)
	}

	// The next keypress (a deliberate interaction) clears it.
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got = updated.(QueueDashboard)
	if got.actionErr != nil {
		t.Fatalf("keypress did not clear action error: %v", got.actionErr)
	}
}

// TestDashboardActionErrorLongMessageReadable verifies a long refusal is rendered
// intact (no column-math truncation into meaninglessness).
func TestDashboardActionErrorLongMessageReadable(t *testing.T) {
	row := TestDashboardRow("proj", "set-1", DashboardRow{ID: "set-1", RuntimePath: "/repo/wt", Bound: true})
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 80, 40

	long := "runtime checkout /some/very/long/path/to/a/worktree is locked for another set and cannot be released without --force"
	updated, _ := m.Update(dashboardHandoffMsg{err: errors.New(long)})
	got := updated.(QueueDashboard)
	if view := got.View().Content; !strings.Contains(view, long) {
		t.Fatalf("long error message truncated in view; got:\n%s", view)
	}
}
