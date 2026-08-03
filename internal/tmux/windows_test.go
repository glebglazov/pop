package tmux

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestWindowExistsBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "main\npop-work\nother"}
	tm := &realTmux{run: r}

	got, err := tm.WindowExists("proj", "pop-work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-windows", "-t", "proj", "-F", "#{window_name}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if !got {
		t.Fatal("WindowExists = false, want true")
	}
}

func TestNewWindowBuildsArgsAndReturnsPane(t *testing.T) {
	r := &recordingRunner{out: "%9\n"}
	tm := &realTmux{run: r}

	id, err := tm.NewWindow("proj", "pop-work", "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "proj:", "-n", "pop-work", "-c", "/dir"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if id != "%9" {
		t.Fatalf("id = %q, want %%9", id)
	}
	// Regression: never pass -a or an explicit window index. -a (insert after
	// current) and session:N collide with an occupied next index in a live
	// interactive session ("index N in use"). Let tmux append at the first free
	// index instead — target the bare session, which -t only reads as a session
	// when it ends in a colon (otherwise it prefix-matches a window).
	for _, arg := range r.calls[0] {
		if arg == "-a" {
			t.Fatal("new-window must not pass -a")
		}
		if idx := strings.Index(arg, ":"); idx >= 0 && !strings.HasPrefix(arg, "#{") && idx != len(arg)-1 {
			t.Fatalf("new-window must not pass an explicit window index target, got %q", arg)
		}
	}
}

func TestNewWindowErrorsOnEmptyPaneID(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{out: "  "}}
	if _, err := tm.NewWindow("proj", "w", "/dir"); err == nil {
		t.Fatal("expected error when tmux returns no pane id")
	}
}

func TestSplitWindowBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "%12"}
	tm := &realTmux{run: r}

	id, err := tm.SplitWindow("proj", "pop-work", "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"split-window", "-d", "-P", "-F", "#{pane_id}", "-t", "proj:pop-work", "-c", "/dir"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if id != "%12" {
		t.Fatalf("id = %q, want %%12", id)
	}
}

func TestRetileWindowBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.RetileWindow("proj", "pop-work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"select-layout", "-t", "proj:pop-work", "tiled"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestWindowPanesBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "%1\n%2\n"}
	tm := &realTmux{run: r}

	panes, err := tm.WindowPanes("proj", "w")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-panes", "-t", "proj:w", "-F", "#{pane_id}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if !reflect.DeepEqual(panes, []string{"%1", "%2"}) {
		t.Fatalf("panes = %v", panes)
	}
}

func TestSelectPaneBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.SelectPane("%3"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"select-pane", "-t", "%3"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestSelectWindowBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.SelectWindow("mysession", "myproject"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"select-window", "-t", "mysession:myproject"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestWindowExistsPropagatesRunnerError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("no server")}}
	if _, err := tm.WindowExists("proj", "w"); err == nil {
		t.Fatal("expected error")
	}
}

func TestWindowTitledPanesBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "server\t%5\ndb\t%6\nlogs\t%7"}
	tm := &realTmux{run: r}

	panes, err := tm.WindowTitledPanes("proj", "agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-panes", "-t", "proj:agent", "-F", "#{pane_title}\t#{pane_id}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	want := []TitledPane{{Title: "server", ID: "%5"}, {Title: "db", ID: "%6"}, {Title: "logs", ID: "%7"}}
	if !reflect.DeepEqual(panes, want) {
		t.Fatalf("panes = %v, want %v", panes, want)
	}
}

func TestWindowTitledPanesErrorsWhenNoWindow(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("can't find window")}}
	if _, err := tm.WindowTitledPanes("proj", "agent"); err == nil {
		t.Fatal("expected error for missing window")
	}
}

func TestFindPaneByTitle(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{out: "server\t%5\ndb\t%6"}}
	id, err := tm.FindPaneByTitle("proj", "agent", "db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "%6" {
		t.Errorf("id = %q, want %%6", id)
	}
	if _, err := tm.FindPaneByTitle("proj", "agent", "missing"); err == nil {
		t.Error("expected error for missing pane")
	}
}
