package wayfinder

import (
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/work"
)

// Mute driven through the real registration store, mirroring setkind's own
// mute tests: what the human sees on the row afterwards and how the window
// ends. A Map has no Auto-drain bit and is not driven by the daemon, so there
// is no supervision-reach half to pin here — mute is purely a view fact
// (ADR-0200 decision 7).

// muteMapFixture is a registered Map with the clock pinned to the Monday
// ADR-0200's worked example is written against, so a window is an exact
// instant rather than a moving target.
func muteMapFixture(t *testing.T, mapID string) (*MapKind, *Deps, work.Container) {
	t.Helper()
	k, d, _ := statusKindFixture(t, mapID)
	mustRegister(t, d, mapID)
	k.d.Now = func() time.Time { return time.Date(2026, time.August, 10, 14, 23, 45, 0, time.UTC) }
	row := mapContainer(t, k, mapID)
	return k, d, row
}

func muteMapUnder(t *testing.T, k *MapKind, mapID, presetName string) []string {
	t.Helper()
	preset, ok := config.ShippedWorkViewPreset(presetName)
	if !ok {
		t.Fatalf("no shipped preset named %q", presetName)
	}
	prev := k.d.ViewPreset
	k.d.ViewPreset = preset
	defer func() { k.d.ViewPreset = prev }()
	return loadedMapIDs(t, k)
}

// muteMapContainer reads the Map back under `all` — the shipped preset that
// asks nothing about mute — so these tests see the row whether or not it is
// muted. Which presets admit a muted row is TestMutedMapLeavesTheDefaultView…'s
// subject, not this one's.
func muteMapContainer(t *testing.T, k *MapKind, mapID string) work.Container {
	t.Helper()
	preset, ok := config.ShippedWorkViewPreset("all")
	if !ok {
		t.Fatalf("no shipped preset named %q", "all")
	}
	prev := k.d.ViewPreset
	k.d.ViewPreset = preset
	defer func() { k.d.ViewPreset = prev }()
	return mapContainer(t, k, mapID)
}

// Decision 8, both halves at once, for a Map exactly as for a Task set: muting
// takes the row out of the default view, the shipped `muted` preset is where it
// went, and once the window elapses the row is simply back on the next load —
// no write, no notification, nothing but a later `now`.
func TestMutedMapLeavesTheDefaultViewAndComesBackWhenTheWindowEnds(t *testing.T) {
	k, _, row := muteMapFixture(t, "2026-08-03-demo-map")
	until := time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC)

	if !sliceContains(muteMapUnder(t, k, "2026-08-03-demo-map", "active"), "2026-08-03-demo-map") {
		t.Fatal("fixture map is not in the default view before muting")
	}
	if sliceContains(muteMapUnder(t, k, "2026-08-03-demo-map", "muted"), "2026-08-03-demo-map") {
		t.Fatal("an unmuted map showed up in the muted preset")
	}

	if _, err := k.Mute(row, until, false); err != nil {
		t.Fatalf("Mute: %v", err)
	}
	if sliceContains(muteMapUnder(t, k, "2026-08-03-demo-map", "active"), "2026-08-03-demo-map") {
		t.Fatal("a muted map is still in the default view")
	}
	if !sliceContains(muteMapUnder(t, k, "2026-08-03-demo-map", "muted"), "2026-08-03-demo-map") {
		t.Fatal("the muted preset does not hold the muted map; nothing could unmute it")
	}

	k.d.Now = func() time.Time { return until.Add(time.Second) }
	if !sliceContains(muteMapUnder(t, k, "2026-08-03-demo-map", "active"), "2026-08-03-demo-map") {
		t.Fatal("the map did not return to the default view once its window had passed")
	}
	if sliceContains(muteMapUnder(t, k, "2026-08-03-demo-map", "muted"), "2026-08-03-demo-map") {
		t.Fatal("an elapsed window still reads as muted")
	}
}

// The mute pair on a Map: the row reads back exactly the words the submenu
// offered, unmute clears it, and — unlike a Task set — there is no Auto-drain
// consequence to name either way, since a Map has none to clear.
func TestMapMuteAndUnmuteReadBackOnTheRow(t *testing.T) {
	k, _, row := muteMapFixture(t, "2026-08-03-demo-map")
	until := time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC)

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

	muted := muteMapContainer(t, k, "2026-08-03-demo-map")
	if !muted.MutedUntil.Equal(until) {
		t.Fatalf("muted until %s, want %s", muted.MutedUntil, until)
	}
	if cell := work.StatusCellText(k.StatusCell(muted)); !strings.Contains(cell, "unmuted on Fri 14 Aug, 09:00 UTC") {
		t.Fatalf("status cell = %q, want the mute read back in full", cell)
	}
	if !muted.MutedUntil.IsZero() {
		if got, want := verbsOf(k.Actions(muted)), work.VerbUnmute; !containsVerb(got, want) {
			t.Fatalf("actions on a muted map = %v, want unmute offered", got)
		}
	}

	if _, err := k.Unmute(row); err != nil {
		t.Fatalf("Unmute: %v", err)
	}
	unmuted := muteMapContainer(t, k, "2026-08-03-demo-map")
	if !unmuted.MutedUntil.IsZero() {
		t.Fatalf("still muted until %s after unmute", unmuted.MutedUntil)
	}
	if containsVerb(verbsOf(k.Actions(unmuted)), work.VerbUnmute) {
		t.Fatal("an unmuted map still offers unmute")
	}
}

// The random window's instant is never disclosed on a Map's row either — the
// same secret a Task set's row keeps (decision 6).
func TestMapSecretMuteNeverShowsItsInstant(t *testing.T) {
	k, _, row := muteMapFixture(t, "2026-08-03-demo-map")
	until := time.Date(2026, time.August, 14, 3, 41, 21, 0, time.UTC)

	out, err := k.Mute(row, until, true)
	if err != nil {
		t.Fatalf("Mute: %v", err)
	}
	if !strings.Contains(out.Message, "unmuted on [?]") {
		t.Fatalf("mute message = %q, want the secret withheld", out.Message)
	}

	c := muteMapContainer(t, k, "2026-08-03-demo-map")
	if !c.MuteSecret || !c.MutedUntil.Equal(until) {
		t.Fatalf("secret mute read back as %s secret=%v", c.MutedUntil, c.MuteSecret)
	}
	cell := work.StatusCellText(k.StatusCell(c))
	if !strings.Contains(cell, "unmuted on [?]") {
		t.Fatalf("status cell = %q, want the secret marked rather than dated", cell)
	}
	for _, leak := range []string{"14 Aug", "03:41", "Aug"} {
		if strings.Contains(cell, leak) {
			t.Fatalf("status cell %q discloses the roll (%q)", cell, leak)
		}
	}
}

// Expiry is a read-time comparison and nothing else, on a Map exactly as on a
// Task set: the window passes, the row comes back on the next load, and the
// instant is still sitting in pop.db untouched (decision 1).
func TestMapMuteExpiresOnReadWithoutWritingAnything(t *testing.T) {
	k, d, row := muteMapFixture(t, "2026-08-03-demo-map")
	until := time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC)

	if _, err := k.Mute(row, until, false); err != nil {
		t.Fatalf("Mute: %v", err)
	}
	if muteMapContainer(t, k, "2026-08-03-demo-map").MutedUntil.IsZero() {
		t.Fatal("mute did not take")
	}

	k.d.Now = func() time.Time { return until.Add(time.Second) }
	expired := muteMapContainer(t, k, "2026-08-03-demo-map")
	if !expired.MutedUntil.IsZero() {
		t.Fatalf("an elapsed window still reads as muted: %s", expired.MutedUntil)
	}
	if cell := work.StatusCellText(k.StatusCell(expired)); strings.Contains(cell, "unmuted on") {
		t.Fatalf("status cell = %q, want no mute suffix once the window has passed", cell)
	}

	s, err := openWorkRegistry(d)
	if err != nil {
		t.Fatalf("openWorkRegistry: %v", err)
	}
	stored, ok, err := s.FindWorkContainer(MapRef("2026-08-03-demo-map"))
	if err != nil || !ok {
		t.Fatalf("FindWorkContainer: %v (found=%v)", err, ok)
	}
	if !stored.MutedUntil.Equal(until) {
		t.Fatalf("expiry rewrote the row: stored %s, want the original %s", stored.MutedUntil, until)
	}
}

// Muting an unregistered Map is refused the same way archiving one is: the bit
// rides a registration, and creating one here would be a second, hidden
// registration path.
func TestMapMuteOfUnregisteredMapIsRefused(t *testing.T) {
	k, _, _ := statusKindFixture(t, "2026-08-03-demo-map")
	row := mapContainer(t, k, "2026-08-03-demo-map")

	if _, err := k.Mute(row, time.Now().Add(24*time.Hour), false); err == nil {
		t.Fatal("muting an unregistered map succeeded")
	}
}

func sliceContains(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

func containsVerb(verbs []work.Verb, want work.Verb) bool {
	for _, v := range verbs {
		if v == want {
			return true
		}
	}
	return false
}

func verbsOf(actions []work.Action) []work.Verb {
	out := make([]work.Verb, 0, len(actions))
	for _, a := range actions {
		out = append(out, a.Verb)
	}
	return out
}
