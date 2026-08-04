package setkind

import (
	"slices"
	"testing"

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
// verb before every in-place one, `I V F S O` then `b u a s r x y p` — the fix
// for the old interleaved `I V b u a s S F r O x y`. The container below trips
// every conditional verb so the full list is exercised in one pass.
func TestActionsOrderSpawningFirst(t *testing.T) {
	k := New(nil)
	c := work.Container{
		ID:         "2026-07-01-demo",
		Bound:      true,
		Orphaned:   false,
		Parked:     true,
		VerifyMark: tasks.VerifyMarkUnverified,
		RawStatus:  tasks.StatusDone,
	}
	want := []work.Verb{
		VerbDrain, VerbVerify, VerbFold, VerbAssist, work.VerbShell,
		VerbBind, VerbUnbind, VerbAutoDrain, work.VerbStatus, VerbUnpark,
		VerbArchive, work.VerbCopyName, VerbCopyPath,
	}
	if got := verbsOffered(k.Actions(c)); !slices.Equal(got, want) {
		t.Fatalf("Actions = %v, want %v", got, want)
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
