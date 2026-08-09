package tmux

import (
	"fmt"
	"reflect"
	"testing"
)

func TestStampWorkSessionBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.StampWorkSession("pop-map-demo", "map", "demo"); err != nil {
		t.Fatalf("StampWorkSession: %v", err)
	}
	want := [][]string{
		{"set-option", "-t", "pop-map-demo", "@pop_work_kind", "map"},
		{"set-option", "-t", "pop-map-demo", "@pop_work_id", "demo"},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

// Unstamped sessions drop out, so a consumer asking for Work sessions never has
// to filter the whole server's session list itself.
func TestWorkSessionsParsesStampedOnly(t *testing.T) {
	r := &recordingRunner{out: "pop-map-demo\tmap\tdemo\t/src/hawk\npop\t\t\t/src/pop\nrails (work)\ttask-set\t2026-08-03-thing\t/wt/rails/work"}
	tm := &realTmux{run: r}

	sessions, err := tm.WorkSessions()
	if err != nil {
		t.Fatalf("WorkSessions: %v", err)
	}
	wantArgs := [][]string{{"list-sessions", "-F", "#{session_name}\t#{@pop_work_kind}\t#{@pop_work_id}\t#{session_path}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	want := []WorkSession{
		{Session: "pop-map-demo", Kind: "map", ID: "demo", Dir: "/src/hawk"},
		{Session: "rails (work)", Kind: "task-set", ID: "2026-08-03-thing", Dir: "/wt/rails/work"},
	}
	if !reflect.DeepEqual(sessions, want) {
		t.Fatalf("sessions = %v, want %v", sessions, want)
	}
}

func TestWorkSessionsAbsentServerReportsEmpty(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("no server running")}}
	sessions, err := tm.WorkSessions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions = %v, want empty", sessions)
	}
}

func TestNewSessionWithWindowBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "%12\n"}
	tm := &realTmux{run: r}

	paneID, err := tm.NewSessionWithWindow("pop-map-demo", "/repo/trunk", "map")
	if err != nil {
		t.Fatalf("NewSessionWithWindow: %v", err)
	}
	if paneID != "%12" {
		t.Fatalf("paneID = %q, want %%12", paneID)
	}
	want := [][]string{{
		"new-session", "-d", "-s", "pop-map-demo", "-c", "/repo/trunk",
		"-n", "map", "-P", "-F", "#{pane_id}",
	}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}
