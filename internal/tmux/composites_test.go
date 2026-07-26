package tmux_test

import (
	"testing"

	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

// The composites (Ensure, Attach, SwitchTarget) carry the create-if-missing
// and switch-vs-attach policy. They are covered here against the stateful
// fake — no argument arrays.

func TestSwitchTargetInsideTmux(t *testing.T) {
	f := &tmuxtest.Fake{Inside: true}

	if err := tmux.SwitchTarget(f, "%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Switched) != 1 || f.Switched[0] != "%5" {
		t.Errorf("Switched = %v, want [%%5]", f.Switched)
	}
	if len(f.Attached) != 0 {
		t.Errorf("Attached = %v, want none", f.Attached)
	}
}

func TestSwitchTargetOutsideTmux(t *testing.T) {
	f := &tmuxtest.Fake{Inside: false}

	if err := tmux.SwitchTarget(f, "work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Attached) != 1 || f.Attached[0] != "work" {
		t.Errorf("Attached = %v, want [work]", f.Attached)
	}
	if len(f.Switched) != 0 {
		t.Errorf("Switched = %v, want none", f.Switched)
	}
}

func TestEnsureCreatesWhenMissing(t *testing.T) {
	f := &tmuxtest.Fake{}

	if err := tmux.Ensure(f, "work", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Live["work"] != "/proj" {
		t.Errorf("Live[work] = %q, want /proj", f.Live["work"])
	}
}

func TestEnsureNoopWhenPresent(t *testing.T) {
	f := &tmuxtest.Fake{Live: map[string]string{"work": "/old"}}

	if err := tmux.Ensure(f, "work", "/new"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Live["work"] != "/old" {
		t.Errorf("Live[work] = %q, want /old (unchanged)", f.Live["work"])
	}
}

func TestAttachNewSessionInsideTmux(t *testing.T) {
	f := &tmuxtest.Fake{Inside: true}

	if err := tmux.Attach(f, "work", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Live["work"] != "/proj" {
		t.Errorf("Live[work] = %q, want /proj", f.Live["work"])
	}
	if len(f.Switched) != 1 || f.Switched[0] != "work" {
		t.Errorf("Switched = %v, want [work]", f.Switched)
	}
}

func TestAttachExistingSessionOutsideTmux(t *testing.T) {
	f := &tmuxtest.Fake{Inside: false, Live: map[string]string{"work": "/proj"}}

	if err := tmux.Attach(f, "work", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Attached) != 1 || f.Attached[0] != "work" {
		t.Errorf("Attached = %v, want [work]", f.Attached)
	}
	if len(f.Switched) != 0 {
		t.Errorf("Switched = %v, want none (session already existed)", f.Switched)
	}
}
