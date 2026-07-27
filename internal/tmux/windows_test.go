package tmux

import (
	"fmt"
	"reflect"
	"testing"
)

func TestWindowExistsBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "main\npop-queue\nother"}
	tm := &realTmux{run: r}

	got, err := tm.WindowExists("proj", "pop-queue")
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

	id, err := tm.NewWindow("proj", "pop-queue", "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "proj", "-n", "pop-queue", "-c", "/dir"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if id != "%9" {
		t.Fatalf("id = %q, want %%9", id)
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

	id, err := tm.SplitWindow("proj", "pop-queue", "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"split-window", "-d", "-P", "-F", "#{pane_id}", "-t", "proj:pop-queue", "-c", "/dir"}}
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

	if err := tm.RetileWindow("proj", "pop-queue"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"select-layout", "-t", "proj:pop-queue", "tiled"}}
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

func TestWindowExistsPropagatesRunnerError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("no server")}}
	if _, err := tm.WindowExists("proj", "w"); err == nil {
		t.Fatal("expected error")
	}
}
