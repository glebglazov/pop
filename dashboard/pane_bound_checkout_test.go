package dashboard

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/drain"
)

// The ladder's weakest rungs (ADR-0201 decision 1, ADR-0209 decision 2): an
// ordinary shell the human opened themselves, standing in a checkout work is bound
// to. Every test here drives the whole path — a fake tmux pane with a real
// directory, real bindings and a real store, through BuildSeededPageSnapshot into a
// model — because the rung is only worth anything if the directory, the ranking and
// the lifted block agree about which sets they are naming.

// boundCheckoutFixture is the shared shape: three registered sets, one real
// checkout, and the caller sitting in an untagged shell inside it. Which sets are
// bound there is the caller's business, since that is what each rung varies.
func boundCheckoutFixture(t *testing.T, bind ...int) (*drain.Deps, *config.Config, []string, string) {
	t.Helper()
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "2026-01-01-done-1", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, _, rt := dashboardLaunchFixture(t, repo, setID)
	stems := registerDoneSets(t, repo, 3)
	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
	d.ViewPreset.Lift = true
	checkout := bindStemsToOneCheckout(t, d, repo, stems, bind...)
	inPane(rt.Fake, "editor", "%7")
	if rt.Fake.PaneCwd == nil {
		rt.Fake.PaneCwd = map[string]string{}
	}
	rt.Fake.PaneCwd["%7"] = checkout
	return d, cfg, stems, checkout
}

// bindStemsToOneCheckout binds the named sets (by index) to a single real
// worktree, which is the ambiguity this rung exists to resolve: one checkout,
// several sets, and no timestamp anywhere in the bindings to rank them by.
func bindStemsToOneCheckout(t *testing.T, d *drain.Deps, repo string, stems []string, which ...int) string {
	t.Helper()
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(t.TempDir(), "shared-wt")
	runGit(t, repo, "worktree", "add", "-b", "wt/shared", checkout)
	for _, i := range which {
		if err := binding.Put(d.Tasks, drain.SetScopedKey(repoKey, stems[i]), binding.Binding{
			RuntimePath: checkout, Branch: "wt/shared",
		}); err != nil {
			t.Fatal(err)
		}
	}
	return checkout
}

// A bare shell in a checkout bound to exactly one set: the rung fires, and that set
// alone is lifted. The shell stands in a subdirectory, which is where an editor
// shell usually is — the directory resolves to the checkout containing it, not only
// to the checkout root itself.
func TestBareShellInACheckoutBoundToOneSetLiftsIt(t *testing.T) {
	d, cfg, stems, checkout := boundCheckoutFixture(t, 1)
	sub := filepath.Join(checkout, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	d.Tmux.(*queuetest.RecordingTmux).Fake.PaneCwd["%7"] = sub

	m := openFromPane(t, d, cfg)

	if got := liftedBlock(t, m); !slices.Equal(got, []string{stems[1]}) {
		t.Fatalf("lifted %v, want the one set bound to the checkout the shell is in (%q)", got, stems[1])
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence when only one set was a candidate", m.flash.Text())
	}
}

// A live Drain at the pane's directory is a nearer rung than the bindings: one
// process, working one set, in that checkout, right now. It wins even where a
// checkout claim held by another set would otherwise have decided.
func TestLiveDrainAtThePaneDirectoryBeatsTheBindingFallback(t *testing.T) {
	d, cfg, stems, checkout := boundCheckoutFixture(t, 0, 1, 2)
	d.LiveDrains = func() ([]tasks.RunningDrain, error) {
		return []tasks.RunningDrain{{SetID: stems[1], RuntimePath: checkout, PID: 4242}}, nil
	}
	registerClaimHeldBy(t, d, stems[0], checkout)

	m := openFromPane(t, d, cfg)

	if got := liftedBlock(t, m); !slices.Equal(got, []string{stems[1]}) {
		t.Fatalf("lifted %v, want only the set the live drain is running (%q)", got, stems[1])
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence: a live drain names its set outright", m.flash.Text())
	}
}

// With several sets bound to one checkout, every one of them lifts: the shell
// really is standing in all of their work. The claim only decides who leads, and
// nothing is said about it — a choice nobody made needs no confessing (ADR-0209
// decision 2).
func TestEveryBoundSetLiftsWithTheClaimHolderLeading(t *testing.T) {
	d, cfg, stems, checkout := boundCheckoutFixture(t, 0, 1, 2)
	baseline := unliftedOrder(t, d, cfg)
	registerClaimHeldBy(t, d, stems[0], checkout)

	m := openFromPane(t, d, cfg)

	got := attributedSets(t, m)
	if len(got) != 3 || got[0] != stems[0] {
		t.Fatalf("attributed %v, want all three bound sets with the claim holder %q leading", got, stems[0])
	}
	if lifted := liftedBlock(t, m); !slices.Equal(lifted, got) {
		t.Fatalf("lifted %v, want every attributed set in attribution order %v", lifted, got)
	}
	if rows := rowIDs(m); !slices.Equal(rows, wantLiftedFirst(baseline, got...)) {
		t.Fatalf("rows = %v, want %v — each lifted set once, the rest as they were", rows, wantLiftedFirst(baseline, got...))
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence: nothing was chosen", m.flash.Text())
	}
}

// With no claim, the set drained most recently leads the lifted block, and the
// sets that never drained follow in the order the active sort already puts them
// in. Every rung of the sub-ladder now orders candidates instead of picking one,
// so all three lift either way.
func TestLiftedCandidatesAreOrderedByDrainRecencyThenSortOrder(t *testing.T) {
	t.Run("most recently drained leads", func(t *testing.T) {
		d, cfg, stems, checkout := boundCheckoutFixture(t, 0, 1, 2)
		recordFinishedDrain(t, d, checkout, stems[0])
		recordFinishedDrain(t, d, checkout, stems[1])

		m := openFromPane(t, d, cfg)

		want := []string{stems[1], stems[0], stems[2]}
		if got := liftedBlock(t, m); !slices.Equal(got, want) {
			t.Fatalf("lifted %v, want %v — recency first, the never-drained set last", got, want)
		}
		if m.flash.Text() != "" {
			t.Fatalf("status = %q, want silence", m.flash.Text())
		}
	})

	t.Run("the current sort orders the rest", func(t *testing.T) {
		d, cfg, stems, _ := boundCheckoutFixture(t, 0, 1, 2)
		// The order the page has with no pane behind it, which is the order the
		// candidates keep inside the block: the lift moves rows, it does not
		// re-sort them.
		var want []string
		for _, id := range unliftedOrder(t, d, cfg) {
			if slices.Contains(stems, id) {
				want = append(want, id)
			}
		}

		m := openFromPane(t, d, cfg)

		if got := liftedBlock(t, m); !slices.Equal(got, want) {
			t.Fatalf("lifted %v, want the rows in their current sort order %v", got, want)
		}
		if m.flash.Text() != "" {
			t.Fatalf("status = %q, want silence", m.flash.Text())
		}
	})
}

// attributedSets is the ranked ids the launch attributed the pane to.
func attributedSets(t *testing.T, m QueueDashboard) []string {
	t.Helper()
	if m.snap.Attribution == nil {
		t.Fatal("attribution = none, want the sets bound to the checkout the shell is in")
	}
	var ids []string
	for _, c := range m.snap.Attribution.Containers {
		ids = append(ids, c.Ref.ContainerID)
	}
	return ids
}

// A shell somewhere no work is bound is the common case, and it is silent: a
// "nothing found" line on every launch trains the human to ignore the status line.
func TestShellInADirectoryWithNoBoundWorkIsSilent(t *testing.T) {
	d, cfg, _, checkout := boundCheckoutFixture(t)

	m := openFromPane(t, d, cfg)

	if m.snap.Attribution != nil {
		t.Fatalf("attribution = %+v from %s, want none", *m.snap.Attribution, checkout)
	}
	if got := liftedBlock(t, m); len(got) != 0 {
		t.Fatalf("lifted %v, want nothing", got)
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence", m.flash.Text())
	}
	if m.ListCursor() != 0 {
		t.Fatalf("cursor = %d, want the untouched first row", m.ListCursor())
	}
}

// registerClaimHeldBy makes one set the live holder of a checkout through the one
// claim source that is not itself a running drain, so a test can tell the claim
// rung apart from the live-drain rung above it.
func registerClaimHeldBy(t *testing.T, d *drain.Deps, setID, checkout string) {
	t.Helper()
	if err := tasks.RegisterCheckoutGateHold(d.Tasks, setID, checkout, true); err != nil {
		t.Fatalf("RegisterCheckoutGateHold: %v", err)
	}
	t.Cleanup(func() { _ = tasks.ReleaseCheckoutGateHold(d.Tasks, setID, checkout) })
}

// recordFinishedDrain writes one set's drain history at the checkout: a start and
// a clean terminal, which is the only per-set recency pop records.
func recordFinishedDrain(t *testing.T, d *drain.Deps, checkout, setID string) {
	t.Helper()
	handle, err := tasks.BeginDrain(d.Tasks, checkout, setID, nil)
	if err != nil {
		t.Fatalf("BeginDrain(%s): %v", setID, err)
	}
	if err := handle.Finish(store.DrainEnding{State: store.StateFinished}); err != nil {
		t.Fatalf("finish drain(%s): %v", setID, err)
	}
}
