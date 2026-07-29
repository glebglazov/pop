package tmux

import (
	"fmt"
	"reflect"
	"testing"
)

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
