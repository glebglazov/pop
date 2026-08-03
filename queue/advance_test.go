package queue

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

// TestSupervisorReconcilesBeforeCandidatesThenDispatches pins the phase order the
// seam defines — reconcile, then a pure candidate read, then dispatch — and that
// a refusal verdict reaches dispatch instead of being filtered out before it.
func TestSupervisorReconcilesBeforeCandidatesThenDispatches(t *testing.T) {
	kind := &recordingAdvancer{
		candidates: []work.Candidate{
			{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Label: "repo/set-a", Verdict: work.Advance()},
			{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-b"}, Label: "repo/set-b", Verdict: work.Refuse("set parked after repeated abnormal drain exits")},
		},
		message: func(c work.Candidate) string { return "queue: " + c.Label + " handled" },
	}

	var out bytes.Buffer
	tick(supervisorTestDeps(t, kind), &out, newRunOutputState())

	want := []string{
		"reconcile",
		"candidates",
		"advance task-set:set-a",
		"advance task-set:set-b",
	}
	if strings.Join(kind.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("supervisor drove the kind as %v, want %v", kind.calls, want)
	}
	for _, line := range []string{"queue: repo/set-a handled", "queue: repo/set-b handled"} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("supervisor output missing %q:\n%s", line, out.String())
		}
	}
}

// TestSupervisorReportsAdvanceFailure pins that a kind's dispatch error is
// reported as the kind worded it and never halts the rest of the pass.
func TestSupervisorReportsAdvanceFailure(t *testing.T) {
	kind := &recordingAdvancer{
		candidates: []work.Candidate{
			{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Label: "repo/set-a", Verdict: work.Advance()},
			{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-b"}, Label: "repo/set-b", Verdict: work.Advance()},
		},
		err: func(c work.Candidate) error {
			if c.Ref.ContainerID == "set-a" {
				return errors.New("queue: repo: spawn set-a: tmux refused pane")
			}
			return nil
		},
		message: func(c work.Candidate) string { return "queue: " + c.Label + " handled" },
	}

	var out bytes.Buffer
	tick(supervisorTestDeps(t, kind), &out, newRunOutputState())

	if !strings.Contains(out.String(), "queue: repo: spawn set-a: tmux refused pane") {
		t.Fatalf("supervisor output missing the kind's failure line:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "queue: repo/set-b handled") {
		t.Fatalf("one candidate's failure must not stop the pass:\n%s", out.String())
	}
}

// TestSupervisorStopsOnCandidateError pins that a scan failure is reported once
// and dispatches nothing — the pre-seam behaviour, now expressed as the kind's
// own error crossing the seam.
func TestSupervisorStopsOnCandidateError(t *testing.T) {
	kind := &recordingAdvancer{candidatesErr: errors.New("queue: scan: store unreadable")}

	var out bytes.Buffer
	tick(supervisorTestDeps(t, kind), &out, newRunOutputState())

	if !strings.Contains(out.String(), "queue: scan: store unreadable") {
		t.Fatalf("supervisor output missing the scan error:\n%s", out.String())
	}
	for _, call := range kind.calls {
		if strings.HasPrefix(call, "advance") {
			t.Fatalf("candidate failure must dispatch nothing, got %v", kind.calls)
		}
	}
}

// TestTaskSetCandidatesApplyAutoDrainConsent proves consent lives inside
// Candidates: a Ready set the human has not consented to auto-drain is never
// surfaced, so the supervisor never learns it exists and no generic consent bit
// is needed anywhere above the kind.
func TestTaskSetCandidatesApplyAutoDrainConsent(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "consent", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	d := &Deps{Tasks: queueTestTasksDeps(t, true), Project: project.DefaultDeps(), LoadConfig: func(string) (*config.Config, error) { return cfg, nil }}
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
	repo, setID, _ := setupSupervisorSpawnRepo(t, "quota-refusal", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := newRecordingTmux(false, "0")
	d := &Deps{Tasks: queueTestTasksDeps(t, true), Project: project.DefaultDeps(), Tmux: rt, LoadConfig: func(string) (*config.Config, error) { return cfg, nil }}
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
	if _, spawned := extractSpawnCommand(rt); spawned {
		t.Fatalf("a refusal must spawn nothing, got %v", rt.commands)
	}
}

// TestReadSurfacesWriteNothing is the purity guard: neither Candidates nor the
// two calls `pop queue status` makes may touch the machine's state. It snapshots
// every file under the data dir across each call — and proves the snapshot can
// see a write at all by making one.
func TestReadSurfacesWriteNothing(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "pure-reads", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	d := &Deps{Tasks: queueTestTasksDeps(t, true), Project: project.DefaultDeps(), LoadConfig: func(string) (*config.Config, error) { return cfg, nil }}
	bindSetInPlace(t, d, repo, setID)
	dataDir := filepath.Dir(tasks.DrainStorePathWith(d.Tasks))

	// Settle the data dir first: the very first read opens the store and creates
	// its sidecar files, which is a one-off, not a write the surface performs.
	statusReads(t, d, cfg)

	before := dataDirSnapshot(t, dataDir)
	statusReads(t, d, cfg)
	assertSameSnapshot(t, "pop queue status", before, dataDirSnapshot(t, dataDir))

	advancer := taskSetAdvancer(t, d, cfg)
	before = dataDirSnapshot(t, dataDir)
	if _, err := advancer.Candidates(); err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	assertSameSnapshot(t, "Candidates", before, dataDirSnapshot(t, dataDir))

	// The detector must be able to fail: one real store write moves the snapshot.
	if err := tasks.RecordSpawnIntent(d.Tasks, repo, setID); err != nil {
		t.Fatalf("RecordSpawnIntent: %v", err)
	}
	if sameSnapshot(before, dataDirSnapshot(t, dataDir)) {
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

// supervisorTestDeps wires a supervisor over one synthetic kind and a config with
// no projects, so the tick exercises the seam and nothing else.
func supervisorTestDeps(t *testing.T, kind work.Kind) *Deps {
	t.Helper()
	cfg := &config.Config{}
	return &Deps{
		Tasks:      queueTestTasksDeps(t, true),
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		Kinds:      func(*config.Config) []work.Kind { return []work.Kind{kind} },
	}
}

// recordingAdvancer is a Work kind that also advances, recording the order the
// supervisor drives its phases in. The phase hooks let a test hold one kind
// inside a phase while another enters it, which is how the concurrent phases are
// told apart from a serial loop.
type recordingAdvancer struct {
	id            work.KindID
	candidates    []work.Candidate
	candidatesErr error
	message       func(work.Candidate) string
	err           func(work.Candidate) error
	onReconcile   func()
	onCandidates  func()
	onAdvance     func(work.Candidate)

	mu    sync.Mutex
	calls []string
}

func (k *recordingAdvancer) record(call string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, call)
}

func (k *recordingAdvancer) ID() work.KindID {
	if k.id == "" {
		return ref.KindTaskSet
	}
	return k.id
}
func (k *recordingAdvancer) Load() ([]work.Container, error) { return nil, nil }
func (k *recordingAdvancer) Less(a, b work.Container) bool   { return a.ID < b.ID }
func (k *recordingAdvancer) StatusCell(work.Container) []work.StatusSegment {
	return nil
}
func (k *recordingAdvancer) Actions(work.Container) []work.Action                { return nil }
func (k *recordingAdvancer) ItemActions(work.Container, work.Item) []work.Action { return nil }
func (k *recordingAdvancer) Perform(work.Container, *work.Item, work.Verb) (work.Outcome, error) {
	return work.Outcome{}, nil
}
func (k *recordingAdvancer) Summary([]work.Container) []string { return nil }
func (k *recordingAdvancer) Columns() []string                 { return nil }

func (k *recordingAdvancer) Reconcile() error {
	k.record("reconcile")
	if k.onReconcile != nil {
		k.onReconcile()
	}
	return nil
}

func (k *recordingAdvancer) Candidates() ([]work.Candidate, error) {
	k.record("candidates")
	if k.onCandidates != nil {
		k.onCandidates()
	}
	if k.candidatesErr != nil {
		return nil, k.candidatesErr
	}
	return k.candidates, nil
}

func (k *recordingAdvancer) Advance(c work.Candidate) (work.Outcome, error) {
	k.record("advance " + c.Ref.String())
	if k.onAdvance != nil {
		k.onAdvance(c)
	}
	if k.err != nil {
		if err := k.err(c); err != nil {
			return work.Outcome{}, err
		}
	}
	message := ""
	if k.message != nil {
		message = k.message(c)
	}
	return work.Outcome{Kind: work.OutcomeMessage, Message: message}, nil
}

// dataDirSnapshot hashes every file under the pop data dir. The sqlite shared-
// memory sidecar is excluded: it is mmap coordination state a plain read may
// touch, while a real write always lands in the database or its write-ahead log.
func dataDirSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasSuffix(path, "-shm") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(dir, path)
		snapshot[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return snapshot
}

func sameSnapshot(before, after map[string]string) bool {
	return snapshotDiff(before, after) == ""
}

func assertSameSnapshot(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if diff := snapshotDiff(before, after); diff != "" {
		t.Fatalf("%s wrote to the data dir:\n%s", what, diff)
	}
}

func snapshotDiff(before, after map[string]string) string {
	var diffs []string
	for name, sum := range after {
		switch prev, ok := before[name]; {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("  created %s", name))
		case prev != sum:
			diffs = append(diffs, fmt.Sprintf("  changed %s", name))
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			diffs = append(diffs, fmt.Sprintf("  removed %s", name))
		}
	}
	sort.Strings(diffs)
	return strings.Join(diffs, "\n")
}
