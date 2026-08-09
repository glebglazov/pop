package tmux

import (
	"fmt"
	"reflect"
	"testing"
)

func TestSessionsBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "session1\t1234567890\nrails (work)\t1234567891"}
	tm := &realTmux{run: r}

	sessions, err := tm.Sessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Argument construction is asserted here, once for this verb.
	wantArgs := [][]string{{"list-sessions", "-F", "#{session_name}\t#{session_activity}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}

	// Parsing preserves session names with spaces and typed timestamps.
	want := []SessionActivity{
		{Name: "session1", Activity: 1234567890},
		{Name: "rails (work)", Activity: 1234567891},
	}
	if !reflect.DeepEqual(sessions, want) {
		t.Fatalf("sessions = %v, want %v", sessions, want)
	}
}

func TestSessionsEmptyOutput(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{out: ""}}

	sessions, err := tm.Sessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("want no sessions, got %v", sessions)
	}
}

// TestSessionsAbsentServerReportsEmpty covers ADR-0199 decision 8: list-sessions
// against a socket with no server must read as "no sessions", never as a failure
// that could push a caller toward Ensure.
func TestSessionsAbsentServerReportsEmpty(t *testing.T) {
	for _, msg := range []string{
		"no server running on /tmp/tmux-501/default",
		"error connecting to /private/tmp/tmux-501/pop (No such file or directory)",
	} {
		tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("%s", msg)}}
		sessions, err := tm.Sessions()
		if err != nil {
			t.Fatalf("%q: unexpected error %v", msg, err)
		}
		if len(sessions) != 0 {
			t.Fatalf("%q: sessions = %v, want empty", msg, sessions)
		}
	}
}

func TestSessionsPropagatesNonAbsentError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("permission denied")}}

	if _, err := tm.Sessions(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCurrentPaneBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "%7"}
	tm := &realTmux{run: r}

	pane, err := tm.CurrentPane()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"display-message", "-p", "#{pane_id}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if pane != "%7" {
		t.Fatalf("pane = %q, want %%7", pane)
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

func TestSessionsSkipsMalformedLines(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{out: "good\t100\nnotabhere\nother\t200"}}

	sessions, err := tm.Sessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []SessionActivity{
		{Name: "good", Activity: 100},
		{Name: "other", Activity: 200},
	}
	if !reflect.DeepEqual(sessions, want) {
		t.Fatalf("sessions = %v, want %v", sessions, want)
	}
}
