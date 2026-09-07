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
		{TagVerify, "@pop_verify"},
		{TagFold, "@pop_fold"},
		{TagAssist, "@pop_assist"},
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

	id, err := tm.FindTaggedPane("proj", DrainWindow, TagSet, "2026-06-14-queue")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-panes", "-t", "proj:pop-work", "-F", "#{@pop_set}\t#{pane_id}"}}
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
	id, err := tm.FindTaggedPane("proj", DrainWindow, TagRoutine, "r1")
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

func TestListTaggedPanesReadsSlotAndLiveness(t *testing.T) {
	r := &recordingRunner{out: "other-set\t1\t%1\tclaude\nset-1\t\t%4\tzsh\nset-1\t3\t%7\tclaude\n"}
	tm := &realTmux{run: r}

	panes, err := tm.ListTaggedPanes("proj", DrainWindow, TagAssist, "set-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-panes", "-t", "proj:pop-work", "-F", "#{@pop_assist}\t#{@pop_slot}\t#{pane_id}\t#{pane_current_command}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	// The slotless pane is the pre-slot single pane: it reads as slot 1 and its
	// bare shell reads as not live.
	want := []TaggedPane{
		{PaneID: "%4", Slot: FirstPaneSlot, Command: "zsh"},
		{PaneID: "%7", Slot: 3, Command: "claude"},
	}
	if !reflect.DeepEqual(panes, want) {
		t.Fatalf("panes = %+v, want %+v", panes, want)
	}
	if panes[0].Live() {
		t.Errorf("bare shell pane reports live")
	}
	if !panes[1].Live() {
		t.Errorf("running pane reports not live")
	}
}

func TestListTaggedPanesMissingWindowIsNoPanesNotError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: errNoWindow}}
	panes, err := tm.ListTaggedPanes("proj", DrainWindow, TagAssist, "set-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(panes) != 0 {
		t.Fatalf("panes = %+v, want none", panes)
	}
}
