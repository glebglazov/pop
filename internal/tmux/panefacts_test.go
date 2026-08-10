package tmux

import (
	"errors"
	"reflect"
	"testing"
)

// The whole of the round-trip: one display-message, targeted at the pane tmux
// exported, whose format carries every fact attribution reads. The field order in
// the format and the field order in the parse are the same arithmetic seen twice,
// so the assertion is on both halves of one call.
func TestCurrentPaneFactsReadsEveryFactInOneCall(t *testing.T) {
	t.Setenv("TMUX", "/tmp/sock,1,0")
	t.Setenv("TMUX_PANE", "%7")
	r := &recordingRunner{out: "%7\tpop-map-2026-08-10-mute\t/repo/trunk\t\t\t\t\t03-ticket\t\tmap\t2026-08-10-mute\n"}
	tm := &realTmux{run: r}

	facts, err := tm.CurrentPaneFacts()
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := [][]string{{"display-message", "-t", "%7", "-p",
		"#{pane_id}\t#{session_name}\t#{pane_current_path}\t#{@pop_set}\t#{@pop_verify}\t#{@pop_fold}\t#{@pop_assist}\t#{@pop_ticket}\t#{@pop_routine}\t#{@pop_work_kind}\t#{@pop_work_id}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args =\n%v\nwant\n%v", r.calls, wantArgs)
	}
	want := PaneFacts{
		PaneID:    "%7",
		Session:   "pop-map-2026-08-10-mute",
		Directory: "/repo/trunk",
		Tags:      map[PaneTag]string{TagTicket: "03-ticket"},
		WorkKind:  "map",
		WorkID:    "2026-08-10-mute",
	}
	if !reflect.DeepEqual(facts, want) {
		t.Fatalf("facts = %+v, want %+v", facts, want)
	}
	if got := facts.Tag(TagSet); got != "" {
		t.Fatalf("Tag(TagSet) = %q on a pane carrying no set tag", got)
	}
}

// Outside the configured server there is no pane to ask about, and asking anyway
// would answer about whichever pane an attached client happens to sit in. The
// runner must not be reached at all.
func TestCurrentPaneFactsOutsideTmuxAsksNothing(t *testing.T) {
	t.Setenv("TMUX", "")
	r := &recordingRunner{out: "%1\tsome-session\t/tmp\t\t\t\t\t\t\t\t\n"}
	tm := &realTmux{run: r}

	facts, err := tm.CurrentPaneFacts()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(facts, PaneFacts{}) {
		t.Fatalf("facts = %+v, want zero outside tmux", facts)
	}
	if len(r.calls) != 0 {
		t.Fatalf("calls = %v, want none outside tmux", r.calls)
	}
}

func TestCurrentPaneFactsAbsentServerIsSilent(t *testing.T) {
	t.Setenv("TMUX", "/tmp/sock,1,0")
	tm := &realTmux{run: &recordingRunner{err: errors.New("no server running")}}
	facts, err := tm.CurrentPaneFacts()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(facts, PaneFacts{}) {
		t.Fatalf("facts = %+v, want zero", facts)
	}
}
