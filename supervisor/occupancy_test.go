package supervisor

import (
	"bytes"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

const occupiedCheckout = "/rt"

// TestSupervisorBlocksAnOccupyingKindThatOmitsItsOwnCheck is the backstop the
// invariant exists for: a kind whose advance would mutate a tree names the tree
// on its candidate and performs no claim check of its own, and the supervisor
// still refuses to start it into a checkout another piece of Work holds. The kind
// is never asked, so forgetting the check cannot produce a collision.
func TestSupervisorBlocksAnOccupyingKindThatOmitsItsOwnCheck(t *testing.T) {
	td := claimingTasksDeps(t)
	startLiveDrain(t, td, "set-a", occupiedCheckout)

	// A stand-in for a future occupying kind: it fills Checkout and checks nothing.
	// Its id is only "not the Task set's", so the ruling is proven across kinds.
	kind := &queuetest.RecordingAdvancer{
		Kind: ref.KindMap,
		CandidateList: []work.Candidate{{
			Ref:      ref.WorkRef{Kind: ref.KindMap, ContainerID: "occupier"},
			Label:    "repo/occupier",
			Checkout: occupiedCheckout,
			Verdict:  work.Advance(),
		}},
		Message: func(c work.Candidate) string { return "work: " + c.Label + " started" },
	}

	var out bytes.Buffer
	tick(supervisorDepsOver(td, kind), &out, newRunOutputState())

	for _, call := range kind.Calls() {
		if strings.HasPrefix(call, "advance") {
			t.Fatalf("an occupied checkout must not reach the kind at all, got %v", kind.Calls())
		}
	}
	want := "work: repo/occupier: skip; checkout claimed by set set-a (running drain)"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("supervisor output missing %q:\n%s", want, out.String())
	}
}

// TestSupervisorOccupancyAgreesWithTheTaskSetDeferral pins the two-sided
// enforcement: over one store state, the Task-set adapter's own deferral (which
// it keeps computing, because its display needs the species and the holder) and
// the supervisor's cross-kind backstop reach the same ruling in the same words.
// They cannot drift — both are pure reads of the store's claim union.
func TestSupervisorOccupancyAgreesWithTheTaskSetDeferral(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "claim-agreement", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	td := claimingTasksDeps(t)
	d := &drain.Deps{Tasks: td, Project: project.DefaultDeps(), LoadConfig: func(string) (*config.Config, error) { return cfg, nil }}
	bindSetInPlace(t, d, repo, setID)

	// Another set is parked at a Failed gate over a dirty tree on that checkout:
	// a claim-bearing hold, so admitting our set would rewrite work under review.
	s, ok, err := td.Store(true)
	if err != nil || !ok {
		t.Fatalf("Store: ok=%v err=%v", ok, err)
	}
	if err := s.PutCheckoutGateHold(store.CheckoutGateHold{
		RuntimePath: repo,
		SetID:       "other-set",
		PID:         liveClaimPID,
		ProcStart:   liveClaimToken,
		Claim:       true,
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}

	candidates := taskSetCandidates(t, d, cfg)
	if len(candidates) != 1 || !candidates[0].Refused() {
		t.Fatalf("candidates = %+v, want the adapter's own claim refusal", candidates)
	}
	adapter := candidates[0].Verdict.Reason
	if !strings.Contains(adapter, "other-set") {
		t.Fatalf("adapter refusal %q should name the holding set", adapter)
	}

	backstop := newCheckoutOccupancy(d).refusal(work.Candidate{
		Ref:      ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: setID},
		Checkout: repo,
		Verdict:  work.Advance(),
	})
	if backstop != adapter {
		t.Fatalf("supervisor backstop = %q, adapter deferral = %q: the two must not disagree", backstop, adapter)
	}
}

// TestEmptyCheckoutIsNeverBlocked pins the opt-in half: a kind that occupies no
// tree leaves Checkout empty, and is started even while every checkout in sight
// is claimed — including twice in one tick, since two such advances collide over
// nothing.
func TestEmptyCheckoutIsNeverBlocked(t *testing.T) {
	td := claimingTasksDeps(t)
	startLiveDrain(t, td, "set-a", occupiedCheckout)

	kind := &queuetest.RecordingAdvancer{
		Kind: ref.KindMap,
		CandidateList: []work.Candidate{
			{Ref: ref.WorkRef{Kind: ref.KindMap, ContainerID: "map-a"}, Label: "repo/map-a", Verdict: work.Advance()},
			{Ref: ref.WorkRef{Kind: ref.KindMap, ContainerID: "map-b"}, Label: "repo/map-b", Verdict: work.Advance()},
		},
		Message: func(c work.Candidate) string { return "work: " + c.Label + " started" },
	}

	var out bytes.Buffer
	tick(supervisorDepsOver(td, kind), &out, newRunOutputState())

	want := []string{"advance map:map-a", "advance map:map-b"}
	if strings.Join(kind.Calls()[len(kind.Calls())-2:], ",") != strings.Join(want, ",") {
		t.Fatalf("kind driven as %v, want both no-checkout candidates advanced", kind.Calls())
	}
	for _, line := range []string{"work: repo/map-a started", "work: repo/map-b started"} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("supervisor output missing %q:\n%s", line, out.String())
		}
	}
}

// TestOccupancyLedgerDefersTheSecondCandidateOnOneCheckout covers the half the
// store cannot see: a drain dispatched moments ago holds no claim yet, so
// without the tick's own ledger a second candidate would follow it into the same
// tree. First wins, the rest defer to a later tick — which is why dispatch is
// serial and ordered.
func TestOccupancyLedgerDefersTheSecondCandidateOnOneCheckout(t *testing.T) {
	td := claimingTasksDeps(t)
	kind := &queuetest.RecordingAdvancer{
		CandidateList: []work.Candidate{
			{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Label: "repo/set-a", Checkout: occupiedCheckout, Verdict: work.Advance()},
			{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-b"}, Label: "repo/set-b", Checkout: occupiedCheckout, Verdict: work.Advance()},
		},
		Message: func(c work.Candidate) string { return "work: " + c.Label + " started" },
	}

	var out bytes.Buffer
	tick(supervisorDepsOver(td, kind), &out, newRunOutputState())

	if !strings.Contains(out.String(), "work: repo/set-a started") {
		t.Fatalf("the first candidate must take the checkout:\n%s", out.String())
	}
	want := "work: repo/set-b: skip; checkout " + occupiedCheckout + " already taken by task-set:set-a this tick"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("supervisor output missing %q:\n%s", want, out.String())
	}
	if strings.Contains(strings.Join(kind.Calls(), ","), "advance task-set:set-b") {
		t.Fatalf("the deferred candidate must not reach the kind, got %v", kind.Calls())
	}
}

// TestOccupancyAdmitsAClaimHeldByTheCandidateItself pins that occupancy is about
// *other* holders: a set resuming past its own quota waiter's claim is the
// ordinary case, not a collision.
func TestOccupancyAdmitsAClaimHeldByTheCandidateItself(t *testing.T) {
	td := claimingTasksDeps(t)
	startLiveDrain(t, td, "set-a", occupiedCheckout)

	occupancy := newCheckoutOccupancy(&drain.Deps{Tasks: td})
	own := work.Candidate{
		Ref:      ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"},
		Checkout: occupiedCheckout,
		Verdict:  work.Advance(),
	}
	if reason := occupancy.refusal(own); reason != "" {
		t.Fatalf("own claim refused with %q, want admission", reason)
	}
	occupancy.occupy(own)
	if reason := occupancy.refusal(own); reason != "" {
		t.Fatalf("own ledger entry refused with %q, want admission", reason)
	}
}

const (
	liveClaimPID   = 4242
	liveClaimToken = "live-tok"
)

// claimingTasksDeps isolates a store whose liveness policy calls exactly one PID
// alive, so a seeded drain or gate hold is a live claim rather than crash debris
// the read filters out.
func claimingTasksDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	td := queuetest.TasksDeps(t, true)
	td.ProcessAlive = func(pid int) bool { return pid == liveClaimPID }
	td.ProcessStartToken = func(pid int) (string, bool) {
		if pid == liveClaimPID {
			return liveClaimToken, true
		}
		return "", false
	}
	return td
}

func startLiveDrain(t *testing.T, td *tasks.Deps, setID, runtimePath string) {
	t.Helper()
	s, ok, err := td.Store(true)
	if err != nil || !ok {
		t.Fatalf("Store: ok=%v err=%v", ok, err)
	}
	if _, err := s.StartDrain(store.Drain{
		Repo:        "/repo/.git",
		SetID:       setID,
		RuntimePath: runtimePath,
		PID:         liveClaimPID,
		ProcStart:   liveClaimToken,
		StartedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("StartDrain on %s: %v", runtimePath, err)
	}
}

// supervisorDepsOver wires a supervisor over synthetic kinds and a config with no
// projects, so a tick exercises the seam and the invariant above it and nothing
// else.
func supervisorDepsOver(td *tasks.Deps, kinds ...work.Kind) *drain.Deps {
	cfg := &config.Config{}
	return &drain.Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		Kinds:      func(*drain.Deps, *config.Config) []work.Kind { return kinds },
	}
}
