package work_test

import (
	"testing"

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

// TestWiredMutableKindsImplementTheMuteSeam drives the two lists the product
// wires — the Work page's and the Routine page's — and holds mute's two halves
// together: a kind whose containers may be muted must satisfy work.Muter,
// because that assertion is the surface's only route to the write
// (dashboard/work_rows.go muterFor). Eligibility is ref.Kind.Mutable's answer
// rather than a verb in Actions, the Mute menu having moved to the row list
// (ADR-0236 decision 5), and a kind that is not mutable must not carry the seam
// either — a Routine with a Mute method is a mute no verb can clear.
//
// It fails on a wrapper that embeds work.Kind rather than its inner kind's
// concrete type: the verbs are promoted through the interface, the seam is not.
func TestWiredMutableKindsImplementTheMuteSeam(t *testing.T) {
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

	sawMutable := false
	for _, list := range lists {
		for _, k := range list.kinds {
			_, seam := k.(work.Muter)
			if !k.ID().Mutable() {
				if seam {
					t.Errorf("%s: kind %s is not mutable but satisfies work.Muter, so a mute of it could be written and never cleared", list.name, k.ID())
				}
				continue
			}
			sawMutable = true
			if !seam {
				t.Errorf("%s: kind %s is mutable but does not satisfy work.Muter, so every mute of it dies at the surface", list.name, k.ID())
			}
		}
	}
	if !sawMutable {
		t.Fatal("no wired kind was mutable; the guard asserted nothing")
	}
}
