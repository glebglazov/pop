package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/drain"
)

// The ladder's weakest rungs (ADR-0201 decisions 1 and 2): an ordinary shell the
// human opened themselves, standing in a checkout work is bound to. Every test
// here drives the whole path — a fake tmux pane with a real directory, real
// bindings and a real store, through BuildSeededPageSnapshot into a model —
// because the rung is only worth anything if the directory, the tie-break and the
// status line agree about one set.

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

// A bare shell in a checkout bound to exactly one set: the rung fires, and it says
// nothing, because there was nothing to choose between. The shell stands in a
// subdirectory, which is where an editor shell usually is — the directory resolves
// to the checkout containing it, not only to the checkout root itself.
func TestBareShellInACheckoutBoundToOneSetSeedsIt(t *testing.T) {
	d, cfg, stems, checkout := boundCheckoutFixture(t, 1)
	sub := filepath.Join(checkout, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	d.Tmux.(*queuetest.RecordingTmux).Fake.PaneCwd["%7"] = sub

	m := openSeeded(t, d, cfg)

	if got := cursorRow(t, m); got != stems[1] {
		t.Fatalf("cursor on %q, want the one set bound to the checkout the shell is in (%q)", got, stems[1])
	}
	if m.statusMsg != "" {
		t.Fatalf("status = %q, want silence when only one set was a candidate", m.statusMsg)
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

	m := openSeeded(t, d, cfg)

	if got := cursorRow(t, m); got != stems[1] {
		t.Fatalf("cursor on %q, want the set the live drain is running (%q)", got, stems[1])
	}
	if m.statusMsg != "" {
		t.Fatalf("status = %q, want silence: a live drain names its set outright", m.statusMsg)
	}
}

// With several sets bound to one checkout, the claim decides — and because a
// choice was made, it is named: which set, out of how many, and why.
func TestCheckoutClaimDecidesAmongSeveralBoundSets(t *testing.T) {
	d, cfg, stems, checkout := boundCheckoutFixture(t, 0, 1, 2)
	registerClaimHeldBy(t, d, stems[0], checkout)

	m := openSeeded(t, d, cfg)

	if got := cursorRow(t, m); got != stems[0] {
		t.Fatalf("cursor on %q, want the claim holder (%q)", got, stems[0])
	}
	for _, want := range []string{stems[0], "3", store.ClaimFailedGate.Phrase()} {
		if !strings.Contains(m.statusMsg, want) {
			t.Fatalf("status = %q, want it to carry %q", m.statusMsg, want)
		}
	}
}

// With no claim, the set drained most recently is the best evidence there is; with
// neither, the topmost bound row under the active sort — a defined answer rather
// than an arbitrary one. Both name the choice.
func TestBoundTieBreaksOnDrainRecencyThenSortOrder(t *testing.T) {
	t.Run("most recently drained", func(t *testing.T) {
		d, cfg, stems, checkout := boundCheckoutFixture(t, 0, 1, 2)
		recordFinishedDrain(t, d, checkout, stems[0])
		recordFinishedDrain(t, d, checkout, stems[1])

		m := openSeeded(t, d, cfg)

		if got := cursorRow(t, m); got != stems[1] {
			t.Fatalf("cursor on %q, want the most recently drained set (%q)", got, stems[1])
		}
		if !strings.Contains(m.statusMsg, stems[1]) || !strings.Contains(m.statusMsg, "recently") {
			t.Fatalf("status = %q, want it to name %q and the recency it was chosen by", m.statusMsg, stems[1])
		}
	})

	t.Run("topmost under the current sort", func(t *testing.T) {
		d, cfg, _, _ := boundCheckoutFixture(t, 0, 1, 2)

		m := openSeeded(t, d, cfg)

		want := m.snap.Containers[0].ID
		if got := cursorRow(t, m); got != want {
			t.Fatalf("cursor on %q, want the topmost row under the current sort (%q)", got, want)
		}
		if !strings.Contains(m.statusMsg, want) || !strings.Contains(m.statusMsg, "topmost") {
			t.Fatalf("status = %q, want it to name %q and the sort it was chosen by", m.statusMsg, want)
		}
	})
}

// A shell somewhere no work is bound is the common case, and it is silent: a
// "nothing found" line on every launch trains the human to ignore the status line.
func TestShellInADirectoryWithNoBoundWorkIsSilent(t *testing.T) {
	d, cfg, _, checkout := boundCheckoutFixture(t)

	m := openSeeded(t, d, cfg)

	if m.snap.Attribution != nil {
		t.Fatalf("attribution = %+v from %s, want none", *m.snap.Attribution, checkout)
	}
	if m.statusMsg != "" {
		t.Fatalf("status = %q, want silence", m.statusMsg)
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
	if err := handle.Finish(store.StateFinished, "", false, time.Time{}); err != nil {
		t.Fatalf("finish drain(%s): %v", setID, err)
	}
}
