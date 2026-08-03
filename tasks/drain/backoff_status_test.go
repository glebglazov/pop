package drain

import (
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

func TestCrashBackoffEscalatesThenParksFromDrainHistory(t *testing.T) {
	td := queuetest.DataDeps(t)
	repo := initGitRepoWithBase(t)
	commonDir := queuetest.RepoCommonDir(t, td, repo)
	delays := []time.Duration{time.Minute, 5 * time.Minute}

	// First abnormal terminal → backoff one delay from the terminal instant.
	queuetest.SeedAbnormalDrain(t, td, repo, "set-crash")
	info, err := tasks.ReadSetBackoff(td, commonDir, "set-crash")
	if err != nil {
		t.Fatalf("ReadSetBackoff: %v", err)
	}
	if info.ConsecutiveAbnormal != 1 {
		t.Fatalf("consecutive abnormal = %d, want 1", info.ConsecutiveAbnormal)
	}
	if parked, until := setBackoffStatus(info, delays, info.LastAbnormalAt); parked || !until.Equal(info.LastAbnormalAt.Add(time.Minute)) {
		t.Fatalf("first backoff = (parked %v, until %s), want until+1m", parked, until)
	}

	// Second abnormal terminal → escalates to the second delay.
	queuetest.SeedAbnormalDrain(t, td, repo, "set-crash")
	info, _ = tasks.ReadSetBackoff(td, commonDir, "set-crash")
	if info.ConsecutiveAbnormal != 2 {
		t.Fatalf("consecutive abnormal = %d, want 2", info.ConsecutiveAbnormal)
	}
	if parked, until := setBackoffStatus(info, delays, info.LastAbnormalAt); parked || !until.Equal(info.LastAbnormalAt.Add(5*time.Minute)) {
		t.Fatalf("second backoff = (parked %v, until %s), want until+5m", parked, until)
	}

	// Third abnormal terminal exhausts the schedule (park threshold = len+1).
	queuetest.SeedAbnormalDrain(t, td, repo, "set-crash")
	info, _ = tasks.ReadSetBackoff(td, commonDir, "set-crash")
	if info.ConsecutiveAbnormal != 3 {
		t.Fatalf("consecutive abnormal = %d, want 3", info.ConsecutiveAbnormal)
	}
	if parked, _ := setBackoffStatus(info, delays, info.LastAbnormalAt); !parked {
		t.Fatalf("third abnormal terminal must park the set")
	}
}

// TestInterruptedTerminalDoesNotBackoffOrPark locks ADR-0120: repeated
// interrupted terminals are clean stops, so the derived backoff/park never
// escalates or parks the set — a manual interrupt clears Auto-drain, so there is
// no re-spawn thrash to throttle.
func TestInterruptedTerminalDoesNotBackoffOrPark(t *testing.T) {
	td := queuetest.DataDeps(t)
	repo := initGitRepoWithBase(t)
	commonDir := queuetest.RepoCommonDir(t, td, repo)
	delays := []time.Duration{time.Minute}

	seedInterruptDrain := func() {
		h, err := tasks.BeginDrain(td, repo, "set-int", nil)
		if err != nil {
			t.Fatalf("BeginDrain: %v", err)
		}
		if err := h.Finish(store.StateInterrupted, "", false, time.Time{}); err != nil {
			t.Fatalf("Finish: %v", err)
		}
	}

	// Two interrupts would exceed the single-entry schedule if they counted as
	// abnormal — they must not.
	seedInterruptDrain()
	seedInterruptDrain()
	info, err := tasks.ReadSetBackoff(td, commonDir, "set-int")
	if err != nil {
		t.Fatalf("ReadSetBackoff: %v", err)
	}
	if info.ConsecutiveAbnormal != 0 {
		t.Fatalf("consecutive abnormal after interrupts = %d, want 0", info.ConsecutiveAbnormal)
	}
	if parked, until := setBackoffStatus(info, delays, time.Now().UTC()); parked || !until.IsZero() {
		t.Fatalf("interrupted set must stay spawnable, got (parked %v, until %s)", parked, until)
	}
}

func TestCleanTerminalResetsBackoffCountFromDrainHistory(t *testing.T) {
	td := queuetest.DataDeps(t)
	repo := initGitRepoWithBase(t)
	commonDir := queuetest.RepoCommonDir(t, td, repo)

	queuetest.SeedAbnormalDrain(t, td, repo, "set-1")
	queuetest.SeedAbnormalDrain(t, td, repo, "set-1")
	info, err := tasks.ReadSetBackoff(td, commonDir, "set-1")
	if err != nil {
		t.Fatalf("ReadSetBackoff: %v", err)
	}
	if info.ConsecutiveAbnormal != 2 {
		t.Fatalf("consecutive abnormal = %d, want 2 before clean terminal", info.ConsecutiveAbnormal)
	}

	// A clean (finished) terminal breaks the abnormal run, resetting the count
	// for free — no stored counter to clear.
	h, err := tasks.BeginDrain(td, repo, "set-1", nil)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	if err := h.Finish(store.StateFinished, "", false, time.Time{}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	info, _ = tasks.ReadSetBackoff(td, commonDir, "set-1")
	if info.ConsecutiveAbnormal != 0 {
		t.Fatalf("consecutive abnormal after clean terminal = %d, want 0", info.ConsecutiveAbnormal)
	}
	if parked, until := setBackoffStatus(info, []time.Duration{time.Minute}, time.Now().UTC()); parked || !until.IsZero() {
		t.Fatalf("clean terminal must leave the set spawnable, got (parked %v, until %s)", parked, until)
	}
}

func TestUnparkSetClearsPark(t *testing.T) {
	td := queuetest.DataDeps(t)
	repo := initGitRepoWithBase(t)
	commonDir := queuetest.RepoCommonDir(t, td, repo)
	delays := []time.Duration{time.Minute}

	// Two abnormal terminals exceed the single-entry schedule and park the set.
	queuetest.SeedAbnormalDrain(t, td, repo, "set-1")
	queuetest.SeedAbnormalDrain(t, td, repo, "set-1")
	info, err := tasks.ReadSetBackoff(td, commonDir, "set-1")
	if err != nil {
		t.Fatalf("ReadSetBackoff: %v", err)
	}
	if parked, _ := setBackoffStatus(info, delays, time.Now().UTC()); !parked {
		t.Fatal("set should be parked before unpark")
	}

	d := &Deps{Tasks: td}
	row := DashboardRow{ID: "set-1", RepoCommonDir: commonDir, RuntimePath: repo}
	if err := UnparkSet(d, row); err != nil {
		t.Fatalf("UnparkSet: %v", err)
	}

	info, _ = tasks.ReadSetBackoff(td, commonDir, "set-1")
	if info.ParkClearedAt.IsZero() {
		t.Fatal("park-clear event was not recorded")
	}
	if parked, until := setBackoffStatus(info, delays, time.Now().UTC()); parked || !until.IsZero() {
		t.Fatalf("set should be spawnable after unpark, got (parked %v, until %s)", parked, until)
	}
}
