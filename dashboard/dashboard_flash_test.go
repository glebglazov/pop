package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"
)

// TestWorkDashboardMessagesLiveOnTheHintLine is the Work dashboard's half of
// ADR-0204: a verb's confirmation lands on the hint line, the hints stand down
// while it is there, and the expiry the model armed for it — arriving as the
// message any of its views may have set — puts the hints back with nothing left
// to clear by hand. It waits out the real three seconds, which is the point.
func TestWorkDashboardMessagesLiveOnTheHintLine(t *testing.T) {
	row := DashboardRow{
		Project: "pop", CursorKey: "pop\x00my-set",
		RawStatus: tasks.StatusReady, ID: "my-set", DefPath: "/repo/tasks",
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 24
	m.copyFunc = func(string) error { return nil }

	// `y` opens the copy menu and `n` is the name inside it (ADR-0236 decision 6);
	// the confirmation the flash carries is the same one it always was.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	updated, cmd := updated.(QueueDashboard).Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	got := updated.(QueueDashboard)
	if got.flash.Text() != "copied my-set" {
		t.Fatalf("flash = %q, want the copy confirmation", got.flash.Text())
	}
	if cmd == nil {
		t.Fatal("the copy armed no expiry: its message would never go away")
	}

	shown := got.View().Content
	if !strings.Contains(shown, "copied my-set") {
		t.Fatalf("flash not rendered:\n%s", shown)
	}
	if strings.Contains(shown, "y copy ▸") {
		t.Fatalf("hints still shown while the flash holds the line:\n%s", shown)
	}

	msg := cmd()
	expiry, ok := msg.(ui.FlashExpiredMsg)
	if !ok {
		t.Fatalf("the copy's command produced %T, want the flash expiry", msg)
	}
	updated, _ = got.Update(expiry)
	got = updated.(QueueDashboard)
	if got.flash.Text() != "" {
		t.Fatalf("flash = %q after expiry, want it gone", got.flash.Text())
	}
	if back := got.View().Content; !strings.Contains(back, "y copy ▸") {
		t.Fatalf("hints did not come back after the flash expired:\n%s", back)
	}
}
