package tmux

import (
	"fmt"
	"reflect"
	"testing"
)

// Argument construction for each lifecycle primitive is asserted here, once
// per verb, against the recording runner.

func TestHasSessionBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if !tm.HasSession("work") {
		t.Fatal("HasSession = false, want true on zero-exit")
	}
	want := [][]string{{"has-session", "-t=work"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestHasSessionFalseOnError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("no such session")}}
	if tm.HasSession("gone") {
		t.Fatal("HasSession = true, want false on non-zero exit")
	}
}

func TestNewSessionBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.NewSession("work", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"new-session", "-ds", "work", "-c", "/proj"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestSwitchClientBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.SwitchClient("%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"switch-client", "-t", "%5"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestAttachSessionUsesAttachRunner(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.AttachSession("work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// attach-session must go through the stdio-wired attach seam, not output.
	if len(r.calls) != 0 {
		t.Fatalf("output calls = %v, want none", r.calls)
	}
	want := [][]string{{"attach-session", "-t", "work"}}
	if !reflect.DeepEqual(r.attachCalls, want) {
		t.Fatalf("attach args = %v, want %v", r.attachCalls, want)
	}
}

func TestKillSessionBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.KillSession("work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"kill-session", "-t", "work"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestKillSessionPropagatesError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("boom")}}
	if err := tm.KillSession("work"); err == nil {
		t.Fatal("expected error")
	}
}
