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
// verb before every in-place one, `I V F S O` then `m u b u a s r x y p` — the
// fix for the old interleaved `I V b u a s S F r O x y`. The container below
// trips every conditional verb so the full list is exercised in one pass, mute
// included, which is also where the deliberate `u` collision shows: unmute
// precedes unbind, so a row carrying both answers `u` with the mute (ADR-0200
// decision 4).
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
		work.VerbMute, work.VerbUnmute, VerbBind, VerbUnbind, VerbAutoDrain,
		work.VerbStatus, VerbUnpark, VerbArchive, work.VerbCopyName, VerbCopyPath,
	}
	if got := verbsOffered(k.Actions(c)); !slices.Equal(got, want) {
		t.Fatalf("Actions = %v, want %v", got, want)
	}
}

// TestVerbCapabilities is the Task-set kind's half of the grant list ADR-0215
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
		work.VerbMute: true, work.VerbUnmute: true, work.VerbStatus: true,
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

// TestCopyPathHiddenOnUnboundSet pins that `p` is hidden — not offered at all —
// on an unbound set, exactly like `u` (unbind), rather than shown and erroring
// when pressed. A bound set offers it immediately after `y` and Perform copies
// the bound worktree's runtime path.
func TestCopyPathHiddenOnUnboundSet(t *testing.T) {
	k := New(nil)

	unbound := work.Container{ID: "2026-07-01-demo", Bound: false}
	actions := k.Actions(unbound)
	if slices.Contains(verbsOffered(actions), VerbCopyPath) {
		t.Fatalf("unbound set offered copy-path: %v", verbsOffered(actions))
	}
	if slices.Contains(verbsOffered(actions), VerbUnbind) {
		t.Fatalf("unbound set offered unbind: %v", verbsOffered(actions))
	}

	bound := work.Container{ID: "2026-07-01-demo", Bound: true, RuntimePath: "/repo/worktrees/demo"}
	actions = k.Actions(bound)
	i := slices.Index(verbsOffered(actions), work.VerbCopyName)
	if i < 0 || i+1 >= len(actions) || actions[i+1].Verb != VerbCopyPath {
		t.Fatalf("copy-path does not sit immediately after copy-name: %v", verbsOffered(actions))
	}
	if key := actions[i+1].Key; key != "p" {
		t.Fatalf("copy-path key = %q, want %q", key, "p")
	}

	out, err := k.Perform(bound, nil, VerbCopyPath)
	if err != nil {
		t.Fatal(err)
	}
	if out.Clipboard != "/repo/worktrees/demo" {
		t.Fatalf("copy-path clipboard = %q, want the bound worktree's runtime path", out.Clipboard)
	}
}
