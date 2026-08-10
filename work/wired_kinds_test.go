package work_test

import (
	"slices"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
)

// The conformance table above drives each adapter as its own package builds it.
// That is one wiring short of the truth: the product does not hand a surface a
// bare adapter, it hands it whatever the wiring list composed — and an optional
// seam is obtained by type assertion, so a wrapper that carries its inner kind
// as the plain work.Kind interface swallows every seam the inner kind had. The
// verb still shows in the menu, the assertion still fails, and no per-kind test
// notices, because none of them holds the wired value. The guard below is the
// one that does.

// probeMuted is the mute a probe container wears so a kind that offers unmute
// only over a muted row is asked the question it answers differently.
var probeMuted = time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC)

// TestWiredKindsOfferingMuteImplementTheMuteSeam drives the two lists the
// product wires — the Work page's and the Routine page's — and holds their two
// halves of mute together: a kind whose Actions offer the mute pair over a
// container must satisfy work.Muter, because that assertion is the surface's
// only route to the write (dashboard/work_rows.go muterFor). A kind that offers
// neither verb is asked for nothing, which is how a Routine passes.
//
// It fails on a wrapper that embeds work.Kind rather than its inner kind's
// concrete type: the verbs are promoted through the interface, the seam is not.
func TestWiredKindsOfferingMuteImplementTheMuteSeam(t *testing.T) {
	f := newFixture(t)
	d := &drain.Deps{
		Tasks: f.tasks,
		// The lists resolve the reader's checkout while they build; nothing here
		// reads a repository, so the seam only has to answer.
		Project: &project.Deps{FS: f.fs, Git: &deps.MockGit{}},
	}
	lists := []struct {
		name  string
		kinds []work.Kind
	}{
		{"work page", d.WorkKinds(f.cfg)},
		{"routine page", d.RoutinePageKinds(f.cfg)},
	}

	sawMute := false
	for _, list := range lists {
		for _, k := range list.kinds {
			// One unmuted probe and one muted one: mute is offered over the first,
			// unmute only over the second, and both halves need the same seam.
			for _, c := range []work.Container{
				{Kind: k.ID(), ID: "probe"},
				{Kind: k.ID(), ID: "probe", MutedUntil: probeMuted},
			} {
				for _, verb := range []work.Verb{work.VerbMute, work.VerbUnmute} {
					if !slices.Contains(verbsOf(k.Actions(c)), verb) {
						continue
					}
					sawMute = true
					if _, ok := k.(work.Muter); !ok {
						t.Errorf("%s: kind %s offers %q but does not satisfy work.Muter, so every mute of it dies at the surface", list.name, k.ID(), verb)
					}
				}
			}
		}
	}
	if !sawMute {
		t.Fatal("no wired kind offered the mute pair over the probe containers; the guard asserted nothing")
	}
}
