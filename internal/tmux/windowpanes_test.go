package tmux

import (
	"errors"
	"reflect"
	"testing"
)

func TestListWindowPanesBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "pop\t2026-07-01-active\t%3\tclaude\n"}
	tm := &realTmux{run: r}
	panes, err := tm.ListWindowPanes()
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := [][]string{{"list-panes", "-a", "-F", "#{session_name}\t#{window_name}\t#{pane_id}\t#{pane_current_command}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	want := []WindowPane{{Session: "pop", WindowName: "2026-07-01-active", PaneID: "%3", Command: "claude"}}
	if !reflect.DeepEqual(panes, want) {
		t.Fatalf("panes = %+v, want %+v", panes, want)
	}
}

func TestListWindowPanesPropagatesRunnerError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: errors.New("boom")}}
	if _, err := tm.ListWindowPanes(); err == nil {
		t.Fatal("expected error")
	}
}
