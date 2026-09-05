package setkind

import (
	"slices"
	"testing"
	"time"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

func verbsOffered(actions []work.Action) []work.Verb {
	verbs := make([]work.Verb, 0, len(actions))
	for _, a := range actions {
		verbs = append(verbs, a.Verb)
	}
	return verbs
}

// TestActionsOrderSpawningFirst pins the ordering rule: every spawning (handoff)
// verb before every in-place one, `I V F A O` then `b u a r` — the fix for
// the old interleaved `I V b u a s S F r O x y`. The container below trips every
// conditional verb so the full list is exercised in one pass.
func TestActionsOrderSpawningFirst(t *testing.T) {
	k := New(nil)
	c := work.Container{
		ID:          "2026-07-01-demo",
		Bound:       true,
		Provisioned: true,
		Orphaned:    false,
		Parked:      true,
		VerifyMark:  tasks.VerifyMarkUnverified,
		RawStatus:   tasks.StatusDone,
		MutedUntil:  time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC),
	}
	want := []work.Verb{
		VerbDrain, VerbVerify, VerbFold, VerbAssist, work.VerbShell,
		VerbBind, VerbUnbind, VerbAutoDrain, VerbUnpark,
	}
	if got := verbsOffered(k.Actions(c)); !slices.Equal(got, want) {
		t.Fatalf("Actions = %v, want %v", got, want)
	}
}

// The `u` contest is over: unmute moved into the Mute menu (ADR-0236 decision 5),
// so on a set that carries both a mute and a worktree `u` is unbind and nothing
// else. A key whose meaning turned on the row's state is the thing this pins
// against coming back.
func TestUnbindOwnsUOnAMutedBoundSet(t *testing.T) {
	k := New(nil)
	c := work.Container{
		ID: "2026-07-01-demo", Bound: true, Provisioned: true,
		RawStatus:  tasks.StatusReady,
		MutedUntil: time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC),
	}
	var onU []work.Action
	for _, a := range k.Actions(c) {
		if a.Key == "u" {
			onU = append(onU, a)
		}
	}
	if len(onU) != 1 || onU[0].Verb != VerbUnbind {
		t.Fatalf("`u` on a muted, bound set = %+v, want unbind worktree alone", onU)
	}
}

// TestVerbCapabilities is the Task-set kind's half of the grant list ADR-0254
// decision 5 asks to be reviewable: every verb this kind owns, with the one bit
// that says whether a Selection may run it. The container below trips every
// conditional verb, so a verb added without a decision about its capability
// fails here rather than reaching a plural menu unnoticed.
func TestVerbCapabilities(t *testing.T) {
	k := New(nil)
	c := work.Container{
		ID: "2026-07-01-demo", Bound: true, Provisioned: true, Parked: true,
		VerifyMark: tasks.VerifyMarkUnverified, RawStatus: tasks.StatusDone,
		MutedUntil: time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC),
	}
	plural := map[work.Verb]bool{
		VerbArchive: true, work.VerbCopyName: true,
		VerbComplete: true, VerbOpen: true, VerbSkip: true, VerbUnarchive: true,
	}
	for _, action := range append(k.Actions(c), k.StatusActions(c)...) {
		if got := action.Modes.AllowsPlural(); got != plural[action.Verb] {
			t.Fatalf("%s plural = %v, want %v", action.Verb, got, plural[action.Verb])
		}
	}
	// Item-level bulk is out of scope, so no task verb is plural.
	item := work.Item{ID: "01", Status: string(tasks.TaskOpen)}
	for _, action := range k.ItemActions(c, item) {
		if action.Modes.AllowsPlural() {
			t.Fatalf("item verb %s is plural, want every item verb singular", action.Verb)
		}
	}
}

// TestCopyMenuOffersNameFolderAndWorktree pins the set's copy menu: the name on
// `n`, the set's own definition folder on `y` — so `y` `y` copies it — and the
// bound worktree on `w`, hidden rather than shown-and-erroring on an unbound set
// exactly like `u` (unbind). Neither copy verb is in Actions any more: the menu
// owns them (ADR-0236 decision 6).
func TestCopyMenuOffersNameFolderAndWorktree(t *testing.T) {
	k := New(nil)

	unbound := work.Container{ID: "2026-07-01-demo", DefPath: "/repo/tasks"}
	if got, want := keyedVerbs(k.CopyActions(unbound)), []string{"n=copy-name", "y=copy-definition-path"}; !slices.Equal(got, want) {
		t.Fatalf("unbound copy menu = %v, want %v", got, want)
	}
	if slices.Contains(verbsOffered(k.Actions(unbound)), VerbUnbind) {
		t.Fatalf("unbound set offered unbind: %v", verbsOffered(k.Actions(unbound)))
	}

	bound := work.Container{ID: "2026-07-01-demo", DefPath: "/repo/tasks", Bound: true, RuntimePath: "/repo/worktrees/demo"}
	if got, want := keyedVerbs(k.CopyActions(bound)), []string{"n=copy-name", "y=copy-definition-path", "w=copy-path"}; !slices.Equal(got, want) {
		t.Fatalf("bound copy menu = %v, want %v", got, want)
	}
	for _, verb := range []work.Verb{work.VerbCopyName, VerbCopyPath, VerbCopyDefinitionPath, work.VerbCopy} {
		if slices.Contains(verbsOffered(k.Actions(bound)), verb) {
			t.Fatalf("Actions offered %s: %v", verb, verbsOffered(k.Actions(bound)))
		}
	}

	out, err := k.Perform(bound, nil, VerbCopyPath)
	if err != nil {
		t.Fatal(err)
	}
	if out.Clipboard != "/repo/worktrees/demo" {
		t.Fatalf("copy-path clipboard = %q, want the bound worktree's runtime path", out.Clipboard)
	}
	// The new capability: the set itself, which nothing could copy before.
	out, err = k.Perform(bound, nil, VerbCopyDefinitionPath)
	if err != nil {
		t.Fatal(err)
	}
	if out.Clipboard != "/repo/tasks/2026-07-01-demo" {
		t.Fatalf("copy-definition-path clipboard = %q, want the set's own folder", out.Clipboard)
	}
	if _, err := k.Perform(work.Container{ID: "2026-07-01-demo"}, nil, VerbCopyDefinitionPath); err == nil {
		t.Fatal("a container carrying no definition path must refuse rather than copy a bare id")
	}
}

// keyedVerbs renders an action list as `key=verb` pairs, which is what a menu
// whose keys are chosen for the fingers has to be pinned on.
func keyedVerbs(actions []work.Action) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		out = append(out, a.Key+"="+string(a.Verb))
	}
	return out
}

func TestTaskCopyPathUsesTaskFile(t *testing.T) {
	k := New(nil)
	c := work.Container{ID: "2026-07-01-demo", RuntimePath: "/repo/worktrees/demo"}
	item := work.Item{ID: "01-task", Status: string(tasks.TaskOpen), File: "/repo/tasks/2026-07-01-demo/01-task.md"}

	actions := k.ItemActions(c, item)
	if !slices.Contains(verbsOffered(actions), VerbCopyPath) {
		t.Fatalf("task item actions = %v, want copy-path", verbsOffered(actions))
	}
	out, err := k.Perform(c, &item, VerbCopyPath)
	if err != nil {
		t.Fatal(err)
	}
	if out.Clipboard != item.File {
		t.Fatalf("copy-path clipboard = %q, want task path %q", out.Clipboard, item.File)
	}

	noFile := item
	noFile.File = ""
	if slices.Contains(verbsOffered(k.ItemActions(c, noFile)), VerbCopyPath) {
		t.Fatal("task without a file offered copy-path")
	}
}
