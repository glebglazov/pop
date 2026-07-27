package tmux

import (
	"fmt"
	"reflect"
	"testing"
)

func TestReadTopicBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "proj-x\tauth refactor\tseed"}
	tm := &realTmux{run: r}

	st, err := tm.ReadTopic("%7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"display-message", "-p", "-t", "%7", "#{session_name}\t#{@pop_topic}\t#{@pop_topic_kind}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	if st != (TopicState{Session: "proj-x", Topic: "auth refactor", Kind: "seed"}) {
		t.Fatalf("state = %+v", st)
	}
}

func TestReadTopicEmptyOptions(t *testing.T) {
	// A pane with no Topic: the runner trims trailing tabs, so only the session
	// survives — Topic and Kind stay empty.
	tm := &realTmux{run: &recordingRunner{out: "proj-x"}}
	st, err := tm.ReadTopic("%7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Session != "proj-x" || st.Topic != "" || st.Kind != "" {
		t.Errorf("state = %+v, want only the session set", st)
	}
}

func TestReadTopicPropagatesRunnerError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("no such pane")}}
	if _, err := tm.ReadTopic("%9"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetTopicBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}
	if err := tm.SetTopic("%7", "auth refactor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"set-option", "-p", "-t", "%7", "@pop_topic", "auth refactor"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestSetTopicWithKindBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}
	if err := tm.SetTopicWithKind("%7", "auth-refactor", "seed"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{
		{"set-option", "-p", "-t", "%7", "@pop_topic", "auth-refactor"},
		{"set-option", "-p", "-t", "%7", "@pop_topic_kind", "seed"},
	}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestClearTopicBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}
	if err := tm.ClearTopic("%7"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{
		{"set-option", "-p", "-t", "%7", "@pop_topic", ""},
		{"set-option", "-p", "-t", "%7", "@pop_topic_kind", ""},
	}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Errorf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestPaneTopicsBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "%1\tauth refactor\n%2\t\n%3\twrite the tests"}
	tm := &realTmux{run: r}

	topics, err := tm.PaneTopics()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"list-panes", "-a", "-F", "#{pane_id}\t#{@pop_topic}"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	want := map[string]string{"%1": "auth refactor", "%3": "write the tests"}
	if !reflect.DeepEqual(topics, want) {
		t.Fatalf("topics = %v, want %v", topics, want)
	}
}

func TestPaneTopicsPropagatesRunnerError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("tmux unavailable")}}
	if _, err := tm.PaneTopics(); err == nil {
		t.Fatal("expected error")
	}
}
