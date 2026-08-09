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

func TestPaneCurrentPathBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "/repo/api"}
	tm := &realTmux{run: r}

	path, err := tm.PaneCurrentPath("%1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"display-message", "-t", "%1", "-p", "#{pane_current_path}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if path != "/repo/api" {
		t.Fatalf("path = %q, want /repo/api", path)
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

func TestPaneCommandsBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "%1 zsh\n%2 node\n%3 vim"}
	tm := &realTmux{run: r}

	commands, err := tm.PaneCommands()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-panes", "-a", "-F", "#{pane_id} #{pane_current_command}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	want := map[string]string{"%1": "zsh", "%2": "node", "%3": "vim"}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %v, want %v", commands, want)
	}
}

func TestPaneCommandsPropagatesRunnerError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("permission denied")}}
	if _, err := tm.PaneCommands(); err == nil {
		t.Fatal("expected error")
	}
}

func TestPaneCommandsAbsentServerReportsEmpty(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("no server running")}}
	commands, err := tm.PaneCommands()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("commands = %v, want empty", commands)
	}
}

func TestAllPanesBuildsArgsAndParses(t *testing.T) {
	// A pane with no pid column is still a pane: the listing is one fork for the
	// whole server, so one odd row must not lose the rest of it.
	r := &recordingRunner{out: "%1 zsh 28405\n%2 claude 28406\n%3 vim"}
	tm := &realTmux{run: r}

	panes, err := tm.AllPanes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-panes", "-a", "-F", "#{pane_id} #{pane_current_command} #{pane_pid}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	want := map[string]PaneProcess{
		"%1": {Command: "zsh", PID: 28405},
		"%2": {Command: "claude", PID: 28406},
		"%3": {Command: "vim"},
	}
	if !reflect.DeepEqual(panes, want) {
		t.Fatalf("panes = %v, want %v", panes, want)
	}
}

func TestAllPanesPropagatesRunnerError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("no server running")}}
	if _, err := tm.AllPanes(); err == nil {
		t.Fatal("expected an error, so a caller can tell no server from an empty one")
	}
}

func TestCapturePreviewBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "line 1\nline 2"}
	tm := &realTmux{run: r}

	out, err := tm.CapturePreview("%5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// -e preserves escape sequences for coloured preview (vs CapturePane).
	wantArgs := [][]string{{"capture-pane", "-p", "-e", "-S", "-50", "-t", "%5"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if out != "line 1\nline 2" {
		t.Fatalf("out = %q", out)
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
