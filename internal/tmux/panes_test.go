package tmux

import (
	"fmt"
	"reflect"
	"testing"
)

func TestPaneInfoBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "project-a\topencode"}
	tm := &realTmux{run: r}

	info, err := tm.PaneInfo("%1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantArgs := [][]string{{"display-message", "-t", "%1", "-p", "#{session_name}\t#{pane_current_command}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if info != (PaneInfo{Session: "project-a", Command: "opencode"}) {
		t.Fatalf("info = %+v", info)
	}
}

func TestPaneInfoErrorsOnMalformedOutput(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{out: "no-tab-here"}}
	if _, err := tm.PaneInfo("%1"); err == nil {
		t.Fatal("expected error on malformed output")
	}
}

func TestPaneInfoPropagatesRunnerError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("pane not found")}}
	if _, err := tm.PaneInfo("%nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPaneSessionBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "project-a"}
	tm := &realTmux{run: r}

	session, err := tm.PaneSession("%1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"display-message", "-t", "%1", "-p", "#{session_name}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if session != "project-a" {
		t.Fatalf("session = %q, want project-a", session)
	}
}

func TestIsActivePane(t *testing.T) {
	tests := []struct {
		name string
		out  string
		err  error
		want bool
	}{
		{name: "attended", out: "1 1 1", want: true},
		{name: "inactive pane", out: "0 1 1", want: false},
		{name: "detached session", out: "1 1 0", want: false},
		{name: "error reports false", err: fmt.Errorf("boom"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &recordingRunner{out: tt.out, err: tt.err}
			tm := &realTmux{run: r}
			if got := tm.IsActivePane("%1"); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
			if tt.err == nil {
				wantArgs := [][]string{{"display-message", "-t", "%1", "-p", "#{pane_active} #{window_active} #{session_attached}"}}
				if !reflect.DeepEqual(r.calls, wantArgs) {
					t.Errorf("args = %v, want %v", r.calls, wantArgs)
				}
			}
		})
	}
}

func TestLivePanesBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "%1\n%2\n%3"}
	tm := &realTmux{run: r}

	panes, err := tm.LivePanes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-panes", "-a", "-F", "#{pane_id}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if !reflect.DeepEqual(panes, []string{"%1", "%2", "%3"}) {
		t.Fatalf("panes = %v", panes)
	}
}

func TestLivePanesPropagatesRunnerError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("tmux unavailable")}}
	if _, err := tm.LivePanes(); err == nil {
		t.Fatal("expected error")
	}
}
