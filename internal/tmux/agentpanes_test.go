package tmux

import (
	"fmt"
	"reflect"
	"testing"
)

func TestHasAgentWindow(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		r := &recordingRunner{out: "main\nagent\nlogs"}
		tm := &realTmux{run: r}
		if !tm.HasAgentWindow("proj") {
			t.Error("expected agent window present")
		}
		wantArgs := [][]string{{"list-windows", "-t", "proj", "-F", "#{window_name}"}}
		if !reflect.DeepEqual(r.calls, wantArgs) {
			t.Errorf("args = %v, want %v", r.calls, wantArgs)
		}
	})
	t.Run("absent", func(t *testing.T) {
		tm := &realTmux{run: &recordingRunner{out: "main\nlogs"}}
		if tm.HasAgentWindow("proj") {
			t.Error("expected no agent window")
		}
	})
	t.Run("error reports false", func(t *testing.T) {
		tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("boom")}}
		if tm.HasAgentWindow("proj") {
			t.Error("expected false on error")
		}
	})
}

func TestAgentPanesBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "server\t%5\ndb\t%6\nlogs\t%7"}
	tm := &realTmux{run: r}

	panes, err := tm.AgentPanes("proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-panes", "-t", "proj:agent", "-F", "#{pane_title}\t#{pane_id}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	want := []AgentPane{{Title: "server", ID: "%5"}, {Title: "db", ID: "%6"}, {Title: "logs", ID: "%7"}}
	if !reflect.DeepEqual(panes, want) {
		t.Fatalf("panes = %v, want %v", panes, want)
	}
}

func TestAgentPanesErrorsWhenNoWindow(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("can't find window")}}
	if _, err := tm.AgentPanes("proj"); err == nil {
		t.Fatal("expected error for missing agent window")
	}
}

func TestFindAgentPane(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{out: "server\t%5\ndb\t%6"}}
	id, err := tm.FindAgentPane("proj", "db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "%6" {
		t.Errorf("id = %q, want %%6", id)
	}
	if _, err := tm.FindAgentPane("proj", "missing"); err == nil {
		t.Error("expected error for missing pane")
	}
}

func TestNewAgentWindowBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "%9"}
	tm := &realTmux{run: r}
	id, err := tm.NewAgentWindow("proj", "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "%9" {
		t.Errorf("id = %q, want %%9", id)
	}
	wantArgs := [][]string{{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "proj", "-n", "agent", "-c", "/dir"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestSplitAgentPaneBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "%9"}
	tm := &realTmux{run: r}
	id, err := tm.SplitAgentPane("proj", "/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "%9" {
		t.Errorf("id = %q, want %%9", id)
	}
	wantArgs := [][]string{{"split-window", "-d", "-P", "-F", "#{pane_id}", "-t", "proj:agent", "-c", "/dir"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestRetileAgentWindowBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}
	if err := tm.RetileAgentWindow("proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"select-layout", "-t", "proj:agent", "tiled"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestSetPaneTitleBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}
	if err := tm.SetPaneTitle("%5", "server"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"select-pane", "-t", "%5", "-T", "server"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestSetRemainOnExitBuildsArgs(t *testing.T) {
	t.Run("on", func(t *testing.T) {
		r := &recordingRunner{}
		tm := &realTmux{run: r}
		if err := tm.SetRemainOnExit("%5", true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantArgs := [][]string{{"set-option", "-p", "-t", "%5", "remain-on-exit", "on"}}
		if !reflect.DeepEqual(r.calls, wantArgs) {
			t.Errorf("args = %v, want %v", r.calls, wantArgs)
		}
	})
	t.Run("off", func(t *testing.T) {
		r := &recordingRunner{}
		tm := &realTmux{run: r}
		if err := tm.SetRemainOnExit("%5", false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantArgs := [][]string{{"set-option", "-p", "-t", "%5", "remain-on-exit", "off"}}
		if !reflect.DeepEqual(r.calls, wantArgs) {
			t.Errorf("args = %v, want %v", r.calls, wantArgs)
		}
	})
}

func TestSendKeysBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}
	if err := tm.SendKeys("%5", "echo hi", "Enter"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"send-keys", "-t", "%5", "echo hi", "Enter"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestKillPaneBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}
	if err := tm.KillPane("%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"kill-pane", "-t", "%5"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestCapturePaneBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "line one\nline two"}
	tm := &realTmux{run: r}
	out, err := tm.CapturePane("%5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "line one\nline two" {
		t.Errorf("out = %q", out)
	}
	wantArgs := [][]string{{"capture-pane", "-p", "-S", "-50", "-t", "%5"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestPaneDead(t *testing.T) {
	tests := []struct {
		name string
		out  string
		err  error
		want bool
	}{
		{name: "dead", out: "1", want: true},
		{name: "alive", out: "0", want: false},
		{name: "error reports false", err: fmt.Errorf("no pane"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &recordingRunner{out: tt.out, err: tt.err}
			tm := &realTmux{run: r}
			if got := tm.PaneDead("%5"); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
			if tt.err == nil {
				wantArgs := [][]string{{"display-message", "-t", "%5", "-p", "#{pane_dead}"}}
				if !reflect.DeepEqual(r.calls, wantArgs) {
					t.Errorf("args = %v, want %v", r.calls, wantArgs)
				}
			}
		})
	}
}

func TestCurrentSessionBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "proj-x"}
	tm := &realTmux{run: r}
	session, err := tm.CurrentSession()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session != "proj-x" {
		t.Errorf("session = %q, want proj-x", session)
	}
	wantArgs := [][]string{{"display-message", "-p", "#S"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}
