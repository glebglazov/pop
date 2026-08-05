package setkind

import (
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/work"
)

// newVerdictFixture builds one repo group whose refresh holds two terminal (DONE)
// sets and one READY set, with a NEEDS-HUMAN verdict stored at HEAD for each
// terminal set. It returns the fork counter beside the deps, which is what the
// narrowing is about: a load that renders no terminal row must ask git nothing.
// The returned closure rebuilds the same refresh, so a second build reads the same
// rows a reload would.
func newVerdictFixture(t *testing.T) (*Deps, *int, func() *tasks.RefreshResult) {
	t.Helper()
	doneManifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "done"}},
	}
	readyManifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"}},
	}
	refresh := func() *tasks.RefreshResult {
		return &tasks.RefreshResult{
			Rows: []tasks.Row{
				{ID: "done-one", Status: tasks.StatusDone},
				{ID: "done-two", Status: tasks.StatusDone},
				{ID: "ready", Status: tasks.StatusReady},
			},
			Manifests: map[string]*tasks.Manifest{
				"done-one": doneManifest,
				"done-two": doneManifest,
				"ready":    readyManifest,
			},
		}
	}
	td := workDataDeps(t)
	d := testDeps(t, nil)
	d.Tasks = td
	d.Refresh = func(string) (*tasks.RefreshResult, error) { return refresh(), nil }
	forks := 0
	d.Tasks.Git = &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		forks++
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir":
			return "/repo/.git", nil
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			return "shaCUR", nil
		}
		return "", nil
	}}
	mkdirDrainStoreDir(t, td)
	runtime := t.TempDir()
	seedBindingStore(t, td, map[string]binding.Binding{
		binding.ScopedKey("repo-key", "done-one"): {RuntimePath: runtime, Branch: "main"},
		binding.ScopedKey("repo-key", "done-two"): {RuntimePath: runtime, Branch: "main"},
		binding.ScopedKey("repo-key", "ready"):    {RuntimePath: runtime, Branch: "main"},
	})
	s, err := store.Open(tasks.DrainStorePathWith(td), func(int, string) bool { return true })
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	for _, setID := range []string{"done-one", "done-two"} {
		if err := s.PutVerifyVerdict(store.VerifyVerdict{
			Repo: "/repo/.git", SetID: setID, WorkSHA: "shaCUR", Verdict: "NEEDS-HUMAN", Findings: "criterion drift",
		}); err != nil {
			t.Fatalf("PutVerifyVerdict: %v", err)
		}
	}
	_ = s.Close()
	return d, &forks, refresh
}

func verdictFixtureGroup() repogroup.Group {
	return staticForScan(scanFixture{
		Name:           "pop",
		ProjectPath:    "/repo/main",
		DefinitionPath: "/def",
		RepoKey:        "repo-key",
		RepoCommonDir:  "/repo/.git",
	}, "main", false)
}

func containerByID(containers []work.Container, id string) *work.Container {
	for i := range containers {
		if containers[i].ID == id {
			return &containers[i]
		}
	}
	return nil
}

// TestVerdictsResolveOnlyForRenderedRows is the narrowing (ADR-0189): with DONE
// rows hidden, the verify overlay asks git nothing at all — no repo identity, no
// work SHA — because no row it would gate is on screen. The one rendered row is
// the READY set, which never consults a verdict.
func TestVerdictsResolveOnlyForRenderedRows(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	d, forks, _ := newVerdictFixture(t)

	got, err := rowsForStatic(d, enabled, verdictFixtureGroup())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "ready" {
		t.Fatalf("rendered rows = %+v, want only the READY set", got)
	}
	if *forks != 0 {
		t.Fatalf("git forks = %d, want 0 (every terminal row is hidden)", *forks)
	}
	if got[0].VerifyMark != tasks.VerifyMarkNone {
		t.Fatalf("READY row VerifyMark = %q, want none", got[0].VerifyMark)
	}
}

// TestRevealingDoneRowsResolvesTheirVerdicts covers the other half of the trade:
// the `f` toggle flips the session flag and reloads, and the reload — this second
// build over the same deps — resolves what the first one skipped. The revealed
// rows carry the verdict-derived status, mark and findings, with nothing missing.
func TestRevealingDoneRowsResolvesTheirVerdicts(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	d, forks, refreshOf := newVerdictFixture(t)

	if _, err := rowsForStatic(d, enabled, verdictFixtureGroup()); err != nil {
		t.Fatal(err)
	}
	before := *forks

	// The toggle's reload: the same deps with the session flag flipped.
	d.IncludeDone = true
	got, err := rowsForStatic(d, enabled, verdictFixtureGroup())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("rendered rows = %d, want 3 (DONE revealed)", len(got))
	}
	if *forks == before {
		t.Fatal("revealing the DONE rows resolved no verdict: git was never asked")
	}
	for _, id := range []string{"done-one", "done-two"} {
		row := containerByID(got, id)
		if row == nil {
			t.Fatalf("set %s missing from the revealed rows: %+v", id, got)
		}
		if row.RawStatus != tasks.StatusVerifyFailed {
			t.Fatalf("set %s RawStatus = %q, want VERIFY-FAILED", id, row.RawStatus)
		}
		if row.VerifyMark != tasks.VerifyMarkFailed {
			t.Fatalf("set %s VerifyMark = %q, want %q", id, row.VerifyMark, tasks.VerifyMarkFailed)
		}
	}

	// Byte-identical to the unnarrowed path: the same refresh run through the
	// overlay with no row filter at all resolves each rendered row to exactly the
	// status, mark and immunization badge the narrowed build produced.
	unnarrowed := refreshOf()
	tasks.ApplyVerifyVerdictsWith(d.Tasks, unnarrowed, enabled, func(setID string) string {
		return binding.RuntimeForSet(mustBindings(t, d), "repo-key", setID)
	})
	for _, want := range unnarrowed.Rows {
		row := containerByID(got, want.ID)
		if row == nil {
			t.Fatalf("set %s missing from the narrowed build: %+v", want.ID, got)
		}
		if row.RawStatus != want.Status || row.VerifyMark != want.VerifyMark ||
			row.VerifiedAtSHA != want.VerifiedAtSHA || row.VerifiedAtDrifted != want.VerifiedAtDrifted {
			t.Fatalf("set %s narrowed = {%q %q %q %v}, unnarrowed = {%q %q %q %v}",
				want.ID, row.RawStatus, row.VerifyMark, row.VerifiedAtSHA, row.VerifiedAtDrifted,
				want.Status, want.VerifyMark, want.VerifiedAtSHA, want.VerifiedAtDrifted)
		}
	}
}

// TestKindSummaryAggregatesOnlyResolvedRows pins the invariant the narrowing owes
// (ADR-0189): a verdict-derived aggregate may only be computed over rows whose
// verdicts were resolved. The Task-set kind's own roll-up counts containers, and a
// container exists only for a row that rendered — which is the same set of rows
// the overlay resolved — so the unresolved DONE sets are outside every tally
// rather than silently inflating one.
func TestKindSummaryAggregatesOnlyResolvedRows(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	d, _, _ := newVerdictFixture(t)

	got, err := rowsForStatic(d, enabled, verdictFixtureGroup())
	if err != nil {
		t.Fatal(err)
	}
	phrases := New(d).Summary(got)
	if len(phrases) == 0 || phrases[0] != "1 task set" {
		t.Fatalf("summary = %v, want a 1-task-set count over the rendered rows alone", phrases)
	}
}

func mustBindings(t *testing.T, d *Deps) map[string]binding.Binding {
	t.Helper()
	bindings, err := binding.AllBindings(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	return bindings
}
