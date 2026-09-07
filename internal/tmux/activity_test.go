package tmux

import (
	"errors"
	"reflect"
	"testing"
)

func TestListActivityPanesBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "proj\t%1\tset-a\t\t\t\t\tnode\nother\t%2\t\tset-b\t\t\t2\tzsh\nidle\t%3\t\t\t\t\t\tbash\n"}
	tm := &realTmux{run: r}
	panes, err := tm.ListActivityPanes()
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := [][]string{{"list-panes", "-a", "-F", "#{session_name}\t#{pane_id}\t#{@pop_set}\t#{@pop_verify}\t#{@pop_fold}\t#{@pop_assist}\t#{@pop_slot}\t#{pane_current_command}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	// A pane carrying no @pop_slot reads as the first slot, which is the pre-slot
	// pane's case; a stamped one reads as the digit it holds (ADR-0263).
	want := []ActivityPane{
		{Session: "proj", PaneID: "%1", Set: "set-a", Slot: FirstPaneSlot, Command: "node"},
		{Session: "other", PaneID: "%2", Verify: "set-b", Slot: 2, Command: "zsh"},
	}
	if !reflect.DeepEqual(panes, want) {
		t.Fatalf("panes = %+v, want %+v", panes, want)
	}
}

func TestListActivityPanesPropagatesRunnerError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: errors.New("boom")}}
	if _, err := tm.ListActivityPanes(); err == nil {
		t.Fatal("expected error")
	}
}

func TestListActivityPanesAbsentServerReportsEmpty(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: errors.New("no server running")}}
	panes, err := tm.ListActivityPanes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(panes) != 0 {
		t.Fatalf("panes = %v, want empty", panes)
	}
}

func TestIsBareShell(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"zsh", true},
		{"-zsh", true},
		{"bash", true},
		{"-bash", true},
		{"fish", true},
		{"sh", true},
		{"  Zsh  ", true},
		{"node", false},
		{"pop", false},
		{"claude", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsBareShell(tc.cmd); got != tc.want {
			t.Errorf("IsBareShell(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}
