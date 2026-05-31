package session

import (
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

type attachCallLog struct {
	hasSession   []string
	newSession   [][2]string // name, dir
	switchClient []string
	attach       []string
}

func (l *attachCallLog) mock(sessionExists bool) *deps.MockTmux {
	return &deps.MockTmux{
		HasSessionFunc: func(name string) bool {
			l.hasSession = append(l.hasSession, name)
			return sessionExists
		},
		NewSessionFunc: func(name, dir string) error {
			l.newSession = append(l.newSession, [2]string{name, dir})
			return nil
		},
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

func TestAttachWith_ExistingSessionInTmux(t *testing.T) {
	var log attachCallLog
	d := &Deps{
		Tmux:   log.mock(true),
		InTmux: func() bool { return true },
	}

	if err := AttachWith(d, "my-session", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(log.hasSession) != 1 || log.hasSession[0] != "my-session" {
		t.Errorf("HasSession calls = %v, want [my-session]", log.hasSession)
	}
	if len(log.newSession) != 0 {
		t.Errorf("NewSession called %d times, want 0", len(log.newSession))
	}
	if len(log.switchClient) != 1 || log.switchClient[0] != "my-session" {
		t.Errorf("SwitchClient calls = %v, want [my-session]", log.switchClient)
	}
	if len(log.attach) != 0 {
		t.Errorf("AttachSession called %d times, want 0", len(log.attach))
	}
}

func TestAttachWith_ExistingSessionOutsideTmux(t *testing.T) {
	var log attachCallLog
	d := &Deps{
		Tmux:   log.mock(true),
		InTmux: func() bool { return false },
	}

	if err := AttachWith(d, "my-session", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(log.hasSession) != 1 {
		t.Errorf("HasSession calls = %d, want 1", len(log.hasSession))
	}
	if len(log.newSession) != 0 {
		t.Errorf("NewSession called %d times, want 0", len(log.newSession))
	}
	if len(log.switchClient) != 0 {
		t.Errorf("SwitchClient called %d times, want 0", len(log.switchClient))
	}
	if len(log.attach) != 1 || log.attach[0] != "my-session" {
		t.Errorf("AttachSession calls = %v, want [my-session]", log.attach)
	}
}

func TestAttachWith_NewSessionInTmux(t *testing.T) {
	var log attachCallLog
	d := &Deps{
		Tmux:   log.mock(false),
		InTmux: func() bool { return true },
	}

	if err := AttachWith(d, "new-session", "/new/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(log.hasSession) != 1 {
		t.Errorf("HasSession calls = %d, want 1", len(log.hasSession))
	}
	if len(log.newSession) != 1 {
		t.Fatalf("NewSession calls = %d, want 1", len(log.newSession))
	}
	if log.newSession[0][0] != "new-session" || log.newSession[0][1] != "/new/proj" {
		t.Errorf("NewSession args = %v, want [new-session /new/proj]", log.newSession[0])
	}
	if len(log.switchClient) != 1 || log.switchClient[0] != "new-session" {
		t.Errorf("SwitchClient calls = %v, want [new-session]", log.switchClient)
	}
	if len(log.attach) != 0 {
		t.Errorf("AttachSession called %d times, want 0", len(log.attach))
	}
}

func TestEnsureWith_ExistingSession(t *testing.T) {
	var log attachCallLog
	d := &Deps{
		Tmux: log.mock(true),
	}

	if err := EnsureWith(d, "my-session", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(log.hasSession) != 1 || log.hasSession[0] != "my-session" {
		t.Errorf("HasSession calls = %v, want [my-session]", log.hasSession)
	}
	if len(log.newSession) != 0 {
		t.Errorf("NewSession called %d times, want 0", len(log.newSession))
	}
	if len(log.switchClient) != 0 {
		t.Errorf("SwitchClient called %d times, want 0", len(log.switchClient))
	}
	if len(log.attach) != 0 {
		t.Errorf("AttachSession called %d times, want 0", len(log.attach))
	}
}

func TestEnsureWith_NewSession(t *testing.T) {
	var log attachCallLog
	d := &Deps{
		Tmux: log.mock(false),
	}

	if err := EnsureWith(d, "new-session", "/new/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(log.hasSession) != 1 {
		t.Errorf("HasSession calls = %d, want 1", len(log.hasSession))
	}
	if len(log.newSession) != 1 {
		t.Fatalf("NewSession calls = %d, want 1", len(log.newSession))
	}
	if log.newSession[0][0] != "new-session" || log.newSession[0][1] != "/new/proj" {
		t.Errorf("NewSession args = %v, want [new-session /new/proj]", log.newSession[0])
	}
	if len(log.switchClient) != 0 {
		t.Errorf("SwitchClient called %d times, want 0", len(log.switchClient))
	}
	if len(log.attach) != 0 {
		t.Errorf("AttachSession called %d times, want 0", len(log.attach))
	}
}

func TestAttachWith_NewSessionOutsideTmux(t *testing.T) {
	var log attachCallLog
	d := &Deps{
		Tmux:   log.mock(false),
		InTmux: func() bool { return false },
	}

	if err := AttachWith(d, "new-session", "/new/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(log.hasSession) != 1 {
		t.Errorf("HasSession calls = %d, want 1", len(log.hasSession))
	}
	if len(log.newSession) != 1 {
		t.Fatalf("NewSession calls = %d, want 1", len(log.newSession))
	}
	if log.newSession[0][0] != "new-session" || log.newSession[0][1] != "/new/proj" {
		t.Errorf("NewSession args = %v, want [new-session /new/proj]", log.newSession[0])
	}
	if len(log.switchClient) != 0 {
		t.Errorf("SwitchClient called %d times, want 0", len(log.switchClient))
	}
	if len(log.attach) != 1 || log.attach[0] != "new-session" {
		t.Errorf("AttachSession calls = %v, want [new-session]", log.attach)
	}
}
