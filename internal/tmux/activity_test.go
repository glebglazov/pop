package tmux

import (
	"errors"
	"reflect"
	"testing"
)

func TestListActivityPanesBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "proj\t%1\tset-a\t\t\t\tnode\nother\t%2\t\tset-b\t\t\tzsh\nidle\t%3\t\t\t\t\tbash\n"}
	tm := &realTmux{run: r}
	panes, err := tm.ListActivityPanes()
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := [][]string{{"list-panes", "-a", "-F", "#{session_name}\t#{pane_id}\t#{@pop_set}\t#{@pop_verify}\t#{@pop_fold}\t#{@pop_assist}\t#{pane_current_command}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	want := []ActivityPane{
		{Session: "proj", PaneID: "%1", Set: "set-a", Command: "node"},
		{Session: "other", PaneID: "%2", Verify: "set-b", Command: "zsh"},
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
