package session

import (
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

type switchTargetCallLog struct {
	switchClient []string
	attach       []string
}

func (l *switchTargetCallLog) mock() *deps.MockTmux {
	return &deps.MockTmux{
		SwitchClientFunc: func(name string) error {
			l.switchClient = append(l.switchClient, name)
			return nil
		},
		AttachSessionFunc: func(name string) error {
			l.attach = append(l.attach, name)
			return nil
		},
	}
}

func TestSwitchTargetWith_InTmux(t *testing.T) {
	var log switchTargetCallLog
	d := &Deps{
		Tmux:   log.mock(),
		InTmux: func() bool { return true },
	}

	if err := SwitchTargetWith(d, "%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(log.switchClient) != 1 || log.switchClient[0] != "%5" {
		t.Errorf("SwitchClient calls = %v, want [%%5]", log.switchClient)
	}
	if len(log.attach) != 0 {
		t.Errorf("AttachSession called %d times, want 0", len(log.attach))
	}
}

func TestSwitchTargetWith_OutsideTmux(t *testing.T) {
	var log switchTargetCallLog
	d := &Deps{
		Tmux:   log.mock(),
		InTmux: func() bool { return false },
	}

	if err := SwitchTargetWith(d, "my-session"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(log.switchClient) != 0 {
		t.Errorf("SwitchClient called %d times, want 0", len(log.switchClient))
	}
	if len(log.attach) != 1 || log.attach[0] != "my-session" {
		t.Errorf("AttachSession calls = %v, want [my-session]", log.attach)
	}
}
