package tmux

import (
	"reflect"
	"testing"
)

func TestTagPaneBuildsArgsPerTag(t *testing.T) {
	tests := []struct {
		tag  PaneTag
		want string
	}{
		{TagRoutine, "@pop_routine"},
		{TagSet, "@pop_set"},
	}
	for _, tt := range tests {
		r := &recordingRunner{}
		tm := &realTmux{run: r}
		if err := tm.TagPane("%4", tt.tag, "val"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantArgs := [][]string{{"set-option", "-p", "-t", "%4", tt.want, "val"}}
		if !reflect.DeepEqual(r.calls, wantArgs) {
			t.Fatalf("args = %v, want %v", r.calls, wantArgs)
		}
	}
}

func TestFindTaggedPaneBuildsArgsAndMatches(t *testing.T) {
	r := &recordingRunner{out: "other-set\t%1\n2026-06-14-queue\t%7\n"}
	tm := &realTmux{run: r}

	id, err := tm.FindTaggedPane("proj", TagSet, "2026-06-14-queue")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-panes", "-t", "proj:pop-queue", "-F", "#{@pop_set}\t#{pane_id}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if id != "%7" {
		t.Fatalf("id = %q, want %%7", id)
	}
}

func TestFindTaggedPaneMissingWindowIsNoPaneNotError(t *testing.T) {
	// An absent window makes list-panes fail; the lookup reports "no pane" so a
	// preview/lookup never creates the window as a side effect.
	tm := &realTmux{run: &recordingRunner{err: errNoWindow}}
	id, err := tm.FindTaggedPane("proj", TagRoutine, "r1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Fatalf("id = %q, want empty", id)
	}
}

var errNoWindow = errTest("can't find window")

type errTest string

func (e errTest) Error() string { return string(e) }
