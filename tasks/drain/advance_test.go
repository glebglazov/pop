package drain

import (
	"github.com/glebglazov/pop/internal/queuetest"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// TestOnlyTheTaskSetKindAdvances pins the asymmetry the split exists for: the
// wired Task-set kind answers the supervisor's Advancer assertion and the Map
// kind does not, because every Decision ticket a Map holds is resolved in a
// session a human opens.
func TestOnlyTheTaskSetKindAdvances(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	kinds := (&Deps{}).WorkKinds(nil)
	if len(kinds) < 2 {
		t.Fatalf("wired kinds = %d, want the Task-set and Map kinds", len(kinds))
	}
	advanceable := map[work.KindID]bool{}
	for _, k := range kinds {
		_, ok := k.(work.Advancer)
		advanceable[k.ID()] = ok
	}
	if !advanceable[ref.KindTaskSet] {
		t.Fatal("the Task-set kind must satisfy work.Advancer: the supervisor drains through it")
	}
	if advanceable[ref.KindMap] {
		t.Fatal("the Map kind must not satisfy work.Advancer: Maps are never advanced unattended")
	}
	if got := len(work.Advancers(kinds)); got != 1 {
		t.Fatalf("work.Advancers over the wired list = %d, want only the Task-set kind", got)
	}
}

// TestTaskSetCandidatesApplyAutoDrainConsent proves consent lives inside
// Candidates: a Ready set the human has not consented to auto-drain is never
// surfaced, so the supervisor never learns it exists and no generic consent bit
// is needed anywhere above the kind.
func TestTaskSetCandidatesApplyAutoDrainConsent(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "consent", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	d := &Deps{Tasks: queuetest.TasksDeps(t, true), Project: project.DefaultDeps(), LoadConfig: func(string) (*config.Config, error) { return cfg, nil }}
	bindSetInPlace(t, d, repo, setID)

	candidates := taskSetCandidates(t, d, cfg)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want the one consented Ready set", candidates)
	}
	got := candidates[0]
	if got.Ref != (ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: setID}) {
		t.Fatalf("candidate ref = %s, want task-set:%s", got.Ref, setID)
	}
	if got.Refused() {
		t.Fatalf("consented Ready set must advance, got refusal %q", got.Verdict.Reason)
	}
	if got.Checkout != repo {
		t.Fatalf("candidate checkout = %q, want the set's bound checkout %q", got.Checkout, repo)
	}

	// Withdraw consent. The set is still Ready and still bound; it simply stops
	// being something the daemon may start.
	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, repo)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	if _, err := tasks.ToggleAutoDrainWith(d.Tasks, id.TasksDir, tasks.StatePathFor(id.TasksDir), setID); err != nil {
		t.Fatalf("ToggleAutoDrainWith: %v", err)
	}
	if candidates := taskSetCandidates(t, d, cfg); len(candidates) != 0 {
		t.Fatalf("candidates after withdrawing auto-drain = %+v, want none", candidates)
	}
}

// TestTaskSetDeferralBecomesRefusalCandidate pins that a deferred Ready set
// crosses the seam as a refusal carrying the deferral's own wording (ADR-0106),
// and that advancing that refusal starts nothing.
func TestTaskSetDeferralBecomesRefusalCandidate(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "quota-refusal", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := queuetest.NewRecordingTmux(false, "0")
	d := &Deps{Tasks: queuetest.TasksDeps(t, true), Project: project.DefaultDeps(), Tmux: rt, LoadConfig: func(string) (*config.Config, error) { return cfg, nil }}
	bindSetInPlace(t, d, repo, setID)
	if _, err := tasks.RegisterRecoveryWaiter(d.Tasks, tasks.RecoveryWaiter{
		SetID:       setID,
		Preset:      "codex",
		ResetAt:     time.Now().UTC().Add(time.Hour),
		RuntimePath: repo,
	}); err != nil {
		t.Fatalf("RegisterRecoveryWaiter: %v", err)
	}

	advancer := taskSetAdvancer(t, d, cfg)
	candidates, err := advancer.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(candidates) != 1 || !candidates[0].Refused() {
		t.Fatalf("candidates = %+v, want one refusal for the deferred set", candidates)
	}
	want := SpawnDeferral{Reason: DeferQuotaRecovery}.Message()
	if candidates[0].Verdict.Reason != want {
		t.Fatalf("refusal reason = %q, want the deferral's own message %q", candidates[0].Verdict.Reason, want)
	}

	outcome, err := advancer.Advance(candidates[0])
	if err != nil {
		t.Fatalf("Advance(refusal): %v", err)
	}
	if outcome.Message != "" {
		t.Fatalf("refusal outcome message = %q, want none: the run-output diff already reports every deferral", outcome.Message)
	}
	if _, spawned := queuetest.ExtractSpawnCommand(rt); spawned {
		t.Fatalf("a refusal must spawn nothing, got %v", rt.Commands)
	}
}

// TestReadSurfacesWriteNothing is the purity guard: neither Candidates nor the
// two calls `pop queue status` makes may touch the machine's state. It snapshots
// every file under the data dir across each call — and proves the snapshot can
// see a write at all by making one.
func TestReadSurfacesWriteNothing(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "pure-reads", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	d := &Deps{Tasks: queuetest.TasksDeps(t, true), Project: project.DefaultDeps(), LoadConfig: func(string) (*config.Config, error) { return cfg, nil }}
	bindSetInPlace(t, d, repo, setID)
	dataDir := filepath.Dir(tasks.DrainStorePathWith(d.Tasks))

	// Settle the data dir first: the very first read opens the store and creates
	// its sidecar files, which is a one-off, not a write the surface performs.
	statusReads(t, d, cfg)

	before := queuetest.DataDirSnapshot(t, dataDir)
	statusReads(t, d, cfg)
	queuetest.AssertSameSnapshot(t, "pop queue status", before, queuetest.DataDirSnapshot(t, dataDir))

	advancer := taskSetAdvancer(t, d, cfg)
	before = queuetest.DataDirSnapshot(t, dataDir)
	if _, err := advancer.Candidates(); err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	queuetest.AssertSameSnapshot(t, "Candidates", before, queuetest.DataDirSnapshot(t, dataDir))

	// The detector must be able to fail: one real store write moves the snapshot.
	if err := tasks.RecordSpawnIntent(d.Tasks, repo, setID); err != nil {
		t.Fatalf("RecordSpawnIntent: %v", err)
	}
	if queuetest.SameSnapshot(before, queuetest.DataDirSnapshot(t, dataDir)) {
		t.Fatal("the data-dir snapshot did not notice a real store write; the purity guard above proves nothing")
	}
}

// statusReads performs exactly what `pop queue status` does: the scheduling
// snapshot and the Work snapshot its table is rendered from.
func statusReads(t *testing.T, d *Deps, cfg *config.Config) {
	t.Helper()
	if _, err := BuildStatus(d, cfg); err != nil {
		t.Fatalf("BuildStatus: %v", err)
	}
	if _, err := work.BuildSnapshot(d.WorkKinds(cfg)); err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
}

// taskSetAdvancer returns the wired Task-set kind's advance seam.
func taskSetAdvancer(t *testing.T, d *Deps, cfg *config.Config) work.Advancer {
	t.Helper()
	advancers := work.Advancers(d.WorkKinds(cfg))
	if len(advancers) != 1 {
		t.Fatalf("advanceable kinds = %d, want the Task-set kind alone", len(advancers))
	}
	return advancers[0]
}

func taskSetCandidates(t *testing.T, d *Deps, cfg *config.Config) []work.Candidate {
	t.Helper()
	candidates, err := taskSetAdvancer(t, d, cfg).Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	return candidates
}
