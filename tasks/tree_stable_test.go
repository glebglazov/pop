package tasks

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// treeStableFixture builds a real checkout with a "demo" set whose one done AFK
// task has a resolvable commit range, so the Verifier and the Refiner both
// reach their agent seam rather than refusing on the range. It returns the deps
// (wired to poll admission fast), the checkout, its repository identity and the
// definition path.
func treeStableFixture(t *testing.T) (*Deps, string, string, string) {
	t.Helper()
	base, repo := drainTestRepo(t)
	d := waitingDeps(t, base, "s001")
	head, err := realGitInDir(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	defPath := filepath.Join(t.TempDir(), "tasks")
	setDir := filepath.Join(defPath, "demo")
	tasks := []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: TaskDone}}
	setupManifest(t, defPath, "demo", tasks)
	writeManifestWithSetKeys(t, setDir, tasks, map[string]any{"base_commit": strings.TrimSpace(head)})
	id, err := ResolveRepositoryIdentity(d, repo)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	return d, repo, id.CommonDir, defPath
}

// queuedSets is the read a Work dashboard row uses for its Admission indicator:
// every set with a live command in a checkout's queue.
func queuedSets(t *testing.T, d *Deps) []string {
	t.Helper()
	queued, err := LiveAdmissionWaiters(d)
	if err != nil {
		t.Fatalf("LiveAdmissionWaiters: %v", err)
	}
	var ids []string
	for _, q := range queued {
		ids = append(ids, q.SetID)
	}
	return ids
}

// assertLeftNoDrainBehind pins the half of ADR-0238 that keeps the standalone
// surfaces light: they take the claim and nothing else — no drain row survives
// them, so no terminal lands in the set's history and no ● lights on its row.
func assertLeftNoDrainBehind(t *testing.T, d *Deps, repo, setID string) {
	t.Helper()
	drains, err := AllDrains(d)
	if err != nil {
		t.Fatalf("AllDrains: %v", err)
	}
	for _, dr := range drains {
		if dr.SetID == setID {
			t.Fatalf("a tree-stable operation left a drain behind: %+v", dr)
		}
	}
	intents, err := PendingSpawns(d, repo)
	if err != nil {
		t.Fatalf("PendingSpawns: %v", err)
	}
	if len(intents) != 0 {
		t.Fatalf("a tree-stable operation recorded a spawn intent: %+v", intents)
	}
}

// A standalone verify no longer judges a tree another drain is rewriting: it
// queues for the checkout, is visible in the queue while it waits (the dashboard
// row's Admission indicator), and runs the Verifier only once the tree is its
// own.
func TestStandaloneVerifyWaitsForTheTreeToHoldStill(t *testing.T) {
	d, repo, commonDir, defPath := treeStableFixture(t)

	rival, err := BeginDrain(d, repo, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival drain: %v", err)
	}

	var mu sync.Mutex
	var out bytes.Buffer
	verifierRan := make(chan struct{}, 1)
	type result struct {
		res *VerifyResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := verifyResolvedSet(d, nil, verifyCoreOptions{
			Repo:        commonDir,
			DefPath:     defPath,
			RuntimePath: repo,
			SetID:       "demo",
			Output:      &lockedWriter{mu: &mu, w: &out},
			admission:   AdmissionWait,
			runVerifier: func(string) (string, error) {
				verifierRan <- struct{}{}
				return "VERDICT: PASS\nFINDINGS: none\n", nil
			},
		})
		done <- result{res, err}
	}()

	waitForAdmissionQueue(t, d, repo, "demo")
	if got := queuedSets(t, d); len(got) != 1 || got[0] != "demo" {
		t.Fatalf("queued sets = %v, want the waiting verify to show as queued for its set", got)
	}
	select {
	case <-verifierRan:
		t.Fatal("the Verifier ran while another drain held the checkout")
	case r := <-done:
		t.Fatalf("verify finished before the checkout was free: %+v", r)
	case <-time.After(20 * time.Millisecond):
	}

	if err := rival.Finish(store.DrainEnding{State: store.StateFinished}); err != nil {
		t.Fatalf("rival finish: %v", err)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("verify after the wait: %v", r.err)
		}
		if r.res.Verdict != VerdictPass {
			t.Fatalf("verdict = %q, want PASS", r.res.Verdict)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("verify never ran after the holder finished")
	}
	select {
	case <-verifierRan:
	default:
		t.Fatal("the Verifier never ran")
	}
	assertLeftNoDrainBehind(t, d, commonDir, "demo")
}

// The Refiner waits for the same reason and gives the checkout back the same
// way: files moving underneath it would produce a report of a state that never
// existed.
func TestStandaloneRefineWaitsForTheTreeToHoldStill(t *testing.T) {
	d, repo, commonDir, defPath := treeStableFixture(t)

	rival, err := BeginDrain(d, repo, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival drain: %v", err)
	}

	var mu sync.Mutex
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := refineResolvedSet(d, nil, refineCoreOptions{
			Repo:        commonDir,
			DefPath:     defPath,
			RuntimePath: repo,
			SetID:       "demo",
			Output:      &lockedWriter{mu: &mu, w: &out},
			admission:   AdmissionWait,
			runRefiner: func(string) (string, string, error) {
				if status := ReadRuntimeLockStatus(d, repo); !status.Locked || status.Metadata.SetID != "demo" {
					return "", "", exitErr(ExitOperational, "the Refiner ran without holding the checkout: %#v", status)
				}
				return "## Naming\n\nfine", "claude", nil
			},
		})
		done <- err
	}()

	waitForAdmissionQueue(t, d, repo, "demo")
	if err := rival.Finish(store.DrainEnding{State: store.StateFinished}); err != nil {
		t.Fatalf("rival finish: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("refine pass after the wait: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("refine pass never ran after the holder finished")
	}
	assertLeftNoDrainBehind(t, d, commonDir, "demo")
}

// A machine driving verify keeps today's behaviour: it exits naming the claim
// rather than blocking forever, and the Verifier is never invoked.
func TestStandaloneVerifyRefusesWhenUnattended(t *testing.T) {
	d, repo, commonDir, defPath := treeStableFixture(t)
	rival, err := BeginDrain(d, repo, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival drain: %v", err)
	}
	defer func() { _ = rival.Finish(store.DrainEnding{State: store.StateFinished}) }()

	_, err = verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo:        commonDir,
		DefPath:     defPath,
		RuntimePath: repo,
		SetID:       "demo",
		Output:      &bytes.Buffer{},
		admission:   AdmissionRefuse,
		runVerifier: func(string) (string, error) {
			t.Fatal("a refused verify must not invoke the Verifier")
			return "", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "claimed by set rival") {
		t.Fatalf("err = %v, want a refusal naming the holding set", err)
	}
}

// The claim is a claim: while a standalone verify runs, the checkout is held
// against everything else that wants the tree to hold still.
func TestStandaloneVerifyHoldsTheCheckoutWhileItRuns(t *testing.T) {
	d, repo, commonDir, defPath := treeStableFixture(t)

	held := false
	_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo:        commonDir,
		DefPath:     defPath,
		RuntimePath: repo,
		SetID:       "demo",
		Output:      &bytes.Buffer{},
		runVerifier: func(string) (string, error) {
			_, beginErr := BeginDrain(d, repo, "other", io.Discard)
			held = beginErr != nil
			return "VERDICT: PASS\nFINDINGS: none\n", nil
		},
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if !held {
		t.Fatal("another drain was admitted while the Verifier was judging the tree")
	}
	// And the checkout is free the moment the verdict is in hand.
	after, err := BeginDrain(d, repo, "other", io.Discard)
	if err != nil {
		t.Fatalf("the checkout was not released after verify: %v", err)
	}
	_ = after.Finish(store.DrainEnding{State: store.StateFinished})
	assertLeftNoDrainBehind(t, d, commonDir, "demo")
}

// The reachable regression behind the re-entrancy exemption: an Assist session's
// Fold takes the checkout for the set, the rebase conflicts, and the conflict
// prompt's "Verify set" asks for a tree that is already standing still — held by
// this very process. It must run, not be refused by its own claim.
func TestFoldConflictVerifyRunsInsideTheSessionsOwnHold(t *testing.T) {
	d, repo, _, _ := treeStableFixture(t)
	id, err := ResolveRepositoryIdentity(d, repo)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	// The fold conflict prompt resolves the set through canonical Task storage
	// rather than a caller-supplied definition path, so the set must live there.
	head, err := realGitInDir(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	setTasks := []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: TaskDone}}
	setupManifest(t, id.TasksDir, "demo", setTasks)
	writeManifestWithSetKeys(t, filepath.Join(id.TasksDir, "demo"), setTasks,
		map[string]any{"base_commit": strings.TrimSpace(head)})

	// This is what the Assist menu does when Fold is chosen.
	hold, err := AcquireTreeStable(d, repo, "demo", io.Discard, AdmissionWait)
	if err != nil {
		t.Fatalf("assist Fold could not take the checkout: %v", err)
	}
	defer func() { _ = hold.Release() }()

	var out bytes.Buffer
	verified := false
	err = runFoldSetVerify(d, nil, FoldConflictContext{
		SetID:       "demo",
		RuntimePath: repo,
	}, FoldConflictAssistanceOptions{
		RunVerifier: func(string) (string, error) {
			verified = true
			return "VERDICT: PASS\nFINDINGS: none\n", nil
		},
	}, &out)
	if err != nil {
		t.Fatalf("verify inside the fold's own hold: %v", err)
	}
	if !verified {
		t.Fatal("the Verifier never ran")
	}
	// The inner verify released nothing: the session still holds the checkout for
	// the rest of the fold.
	if _, err := BeginDrain(d, repo, "other", io.Discard); err == nil {
		t.Fatal("the fold's hold was released by its own nested verify")
	}
}
