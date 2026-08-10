package setkind

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// Mute driven through the real registration store: what the human sees on the
// row afterwards, what the write did to Auto-drain, and how the window ends.

const muteSetID = "2026-07-01-demo"

// muteFixture is archiveFixture's registered set with the clock pinned to the
// Monday ADR-0200's worked example is written against, so a window is an exact
// instant rather than a moving target.
func muteFixture(t *testing.T) (*Deps, repogroup.Group, work.Container) {
	t.Helper()
	d, g := archiveFixture(t)
	d.Now = func() time.Time { return time.Date(2026, time.August, 10, 14, 23, 45, 0, time.UTC) }
	row := work.Container{ID: muteSetID, DefPath: g.DefPath, StatePath: g.StatePath}
	return d, g, row
}

func muteLoad(t *testing.T, d *Deps, g repogroup.Group) work.Container {
	t.Helper()
	containers, err := containersForGroup(d, d.config(), g)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range containers {
		if c.ID == muteSetID {
			return c
		}
	}
	t.Fatalf("set %s missing from load: %+v", muteSetID, containers)
	return work.Container{}
}

// The whole of mute's reach into supervision, and its deliberate asymmetry:
// muting destroys the Auto-drain bit, unmuting does not give it back, and the
// human may turn it on again while the set is still muted (ADR-0200 decision 2).
func TestMuteClearsAutoDrainAndUnmuteDoesNotRestoreIt(t *testing.T) {
	d, g, row := muteFixture(t)
	k := New(d)
	until := time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC)

	if _, err := tasks.SetTaskSetAutoDrain(d.Tasks, g.DefPath, muteSetID, true); err != nil {
		t.Fatalf("seed auto-drain: %v", err)
	}
	if c := muteLoad(t, d, g); !c.AutoDrain {
		t.Fatal("fixture did not start with auto-drain on")
	}

	out, err := k.Mute(row, until, false)
	if err != nil {
		t.Fatalf("Mute: %v", err)
	}
	if out.Kind != work.OutcomeRefresh {
		t.Fatalf("mute outcome = %+v, want a refresh", out)
	}
	if !strings.Contains(out.Message, "unmuted on Fri 14 Aug, 09:00 UTC") {
		t.Fatalf("mute message = %q, want the window worded as the row will read", out.Message)
	}

	muted := muteLoad(t, d, g)
	if !muted.MutedUntil.Equal(until) {
		t.Fatalf("muted until %s, want %s", muted.MutedUntil, until)
	}
	if muted.AutoDrain {
		t.Fatal("muting left the auto-drain bit standing")
	}
	if cell := tasks.WorkRowStatusCell(muted); !strings.Contains(cell, "unmuted on Fri 14 Aug, 09:00 UTC") {
		t.Fatalf("status cell = %q, want the mute read back in full", cell)
	}

	if _, err := k.Unmute(row); err != nil {
		t.Fatalf("Unmute: %v", err)
	}
	unmuted := muteLoad(t, d, g)
	if !unmuted.MutedUntil.IsZero() {
		t.Fatalf("still muted until %s after unmute", unmuted.MutedUntil)
	}
	if unmuted.AutoDrain {
		t.Fatal("unmute restored the auto-drain bit; it must not")
	}

	// Auto-drain is standing consent to act, so an explicit instruction outranks
	// the view gesture: turning it back on under a live mute is honoured.
	if _, err := k.Mute(row, until, false); err != nil {
		t.Fatalf("re-mute: %v", err)
	}
	if _, err := tasks.SetTaskSetAutoDrain(d.Tasks, g.DefPath, muteSetID, true); err != nil {
		t.Fatalf("re-enable auto-drain under a mute: %v", err)
	}
	both := muteLoad(t, d, g)
	if !both.AutoDrain || both.MutedUntil.IsZero() {
		t.Fatalf("want a muted set with auto-drain back on, got muted=%s auto-drain=%v",
			both.MutedUntil, both.AutoDrain)
	}
}

// The random window's instant is never disclosed — not on the row, and not on
// the line that confirms the mute, which is a read surface too (decision 6).
func TestSecretMuteNeverShowsItsInstant(t *testing.T) {
	d, g, row := muteFixture(t)
	k := New(d)
	until := time.Date(2026, time.August, 14, 3, 41, 21, 0, time.UTC)

	out, err := k.Mute(row, until, true)
	if err != nil {
		t.Fatalf("Mute: %v", err)
	}
	if !strings.Contains(out.Message, "unmuted on [?]") {
		t.Fatalf("mute message = %q, want the secret withheld", out.Message)
	}

	c := muteLoad(t, d, g)
	if !c.MuteSecret || !c.MutedUntil.Equal(until) {
		t.Fatalf("secret mute read back as %s secret=%v", c.MutedUntil, c.MuteSecret)
	}
	cell := tasks.WorkRowStatusCell(c)
	if !strings.Contains(cell, "unmuted on [?]") {
		t.Fatalf("status cell = %q, want the secret marked rather than dated", cell)
	}
	for _, leak := range []string{"14 Aug", "03:41", "Aug"} {
		if strings.Contains(cell, leak) {
			t.Fatalf("status cell %q discloses the roll (%q)", cell, leak)
		}
	}
}

// Expiry is a read-time comparison and nothing else: the window passes, the row
// comes back on the next load, and the instant is still sitting in pop.db
// untouched because no job ever ran to clear it (decision 1).
func TestMuteExpiresOnReadWithoutWritingAnything(t *testing.T) {
	d, g, row := muteFixture(t)
	k := New(d)
	until := time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC)

	if _, err := k.Mute(row, until, false); err != nil {
		t.Fatalf("Mute: %v", err)
	}
	if muteLoad(t, d, g).MutedUntil.IsZero() {
		t.Fatal("mute did not take")
	}

	d.Now = func() time.Time { return until.Add(time.Second) }
	expired := muteLoad(t, d, g)
	if !expired.MutedUntil.IsZero() {
		t.Fatalf("an elapsed window still reads as muted: %s", expired.MutedUntil)
	}
	if cell := tasks.WorkRowStatusCell(expired); strings.Contains(cell, "unmuted on") {
		t.Fatalf("status cell = %q, want no mute suffix once the window has passed", cell)
	}

	s, _, err := d.Tasks.Store(true)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	stored, ok, err := s.FindWorkContainer(expired.Ref())
	if err != nil || !ok {
		t.Fatalf("FindWorkContainer: %v (found=%v)", err, ok)
	}
	if !stored.MutedUntil.Equal(until) {
		t.Fatalf("expiry rewrote the row: stored %s, want the original %s", stored.MutedUntil, until)
	}
}

// Muting a busy set succeeds and says what is still live there. Refusing would
// disable mute in the exact moment a noisy set is what the human wants out of
// their view (decision 3).
func TestMuteSucceedsOnABusySetAndReportsWhatIsLive(t *testing.T) {
	d, g, row := muteFixture(t)
	k := New(d)
	checkout := t.TempDir()
	row.RuntimePath = checkout

	s, _, err := d.Tasks.Store(true)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := s.StartDrain(store.Drain{
		Repo:        "repo-key",
		SetID:       muteSetID,
		RuntimePath: checkout,
		PID:         os.Getpid(),
		StartedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}

	out, err := k.Mute(row, time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatalf("muting a busy set was refused: %v", err)
	}
	if !strings.Contains(out.Message, "running drain still live") {
		t.Fatalf("mute message = %q, want the live drain named", out.Message)
	}
	if !strings.Contains(out.Message, "pid") {
		t.Fatalf("mute message = %q, want the pid that would have to be dealt with", out.Message)
	}
	if muteLoad(t, d, g).MutedUntil.IsZero() {
		t.Fatal("the mute did not take on a busy set")
	}
}
