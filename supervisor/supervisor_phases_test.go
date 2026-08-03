package supervisor

import (
	"bytes"
	"errors"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// TestReconcileAndCandidatesRunConcurrentlyAcrossKinds drives both phases through
// a meeting point no kind can pass alone: if the supervisor ran the kinds one
// after another, the first would wait for a partner that never arrives. Safe now
// that candidates are pure — neither phase's writes are ordered against the
// other kind's.
func TestReconcileAndCandidatesRunConcurrentlyAcrossKinds(t *testing.T) {
	reconciled := newMeetingPoint(2)
	read := newMeetingPoint(2)
	var missed []string
	var mu sync.Mutex
	meet := func(phase string, m *meetingPoint) func() {
		return func() {
			if !m.arrive() {
				mu.Lock()
				missed = append(missed, phase)
				mu.Unlock()
			}
		}
	}
	kinds := []work.Kind{
		&queuetest.RecordingAdvancer{Kind: ref.KindTaskSet, OnReconcile: meet("task-set reconcile", reconciled), OnCandidates: meet("task-set candidates", read)},
		&queuetest.RecordingAdvancer{Kind: ref.KindMap, OnReconcile: meet("map reconcile", reconciled), OnCandidates: meet("map candidates", read)},
	}

	var out bytes.Buffer
	tick(supervisorDepsOver(claimingTasksDeps(t), kinds...), &out, newRunOutputState())

	if len(missed) > 0 {
		t.Fatalf("the kinds never met inside %v: the phases ran serially", missed)
	}
}

// TestDispatchIsSerialInKindPrecedenceOrder pins the other half: dispatch mutates
// the shared checkout ledger, so it runs one candidate at a time and in kind
// precedence order — Task sets before Maps — however the wiring list is ordered.
func TestDispatchIsSerialInKindPrecedenceOrder(t *testing.T) {
	var mu sync.Mutex
	var order []string
	inFlight := 0
	overlapped := false
	advance := func(name string) func(work.Candidate) {
		return func(work.Candidate) {
			mu.Lock()
			order = append(order, name)
			inFlight++
			overlapped = overlapped || inFlight > 1
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
		}
	}
	mapKind := &queuetest.RecordingAdvancer{
		Kind:          ref.KindMap,
		CandidateList: []work.Candidate{{Ref: ref.WorkRef{Kind: ref.KindMap, ContainerID: "map-a"}, Label: "repo/map-a", Verdict: work.Advance()}},
		OnAdvance:     advance("map"),
	}
	setKind := &queuetest.RecordingAdvancer{
		Kind:          ref.KindTaskSet,
		CandidateList: []work.Candidate{{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Label: "repo/set-a", Verdict: work.Advance()}},
		OnAdvance:     advance("task-set"),
	}

	var out bytes.Buffer
	// Wired Map-first: precedence, not list order, decides who dispatches first.
	tick(supervisorDepsOver(claimingTasksDeps(t), mapKind, setKind), &out, newRunOutputState())

	if overlapped {
		t.Fatal("two advances overlapped: dispatch must be serial, it mutates the shared checkout ledger")
	}
	if got := strings.Join(order, ","); got != "task-set,map" {
		t.Fatalf("dispatch order = %s, want task-set,map (kind precedence)", got)
	}
}

// TestDispatchEmitsStructuredEvents pins the reporting generalization: every
// dispatch decision — a kind's start, a kind's failure, and the supervisor's own
// occupancy refusal — comes back as one event carrying the kind, the ref, the
// outcome and the error, and all three render through the same call.
func TestDispatchEmitsStructuredEvents(t *testing.T) {
	claimed := &store.CheckoutClaim{Holder: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Reason: store.ClaimFailedGate}
	spawnErr := errors.New("queue: repo: spawn set-c: tmux refused pane")
	kind := &queuetest.RecordingAdvancer{
		Message: func(c work.Candidate) string { return "queue: repo: spawned drain for " + c.Ref.ContainerID },
		Err: func(c work.Candidate) error {
			if c.Ref.ContainerID == "set-c" {
				return spawnErr
			}
			return nil
		},
	}
	pass := kindPass{kind: ref.KindTaskSet, adv: kind}
	occupancy := &checkoutOccupancy{
		claim: func(path string) *store.CheckoutClaim {
			if path == occupiedCheckout {
				return claimed
			}
			return nil
		},
		taken: map[string]ref.WorkRef{},
	}

	candidate := func(id, checkout string) work.Candidate {
		return work.Candidate{
			Ref:      ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: id},
			Label:    "repo/" + id,
			Checkout: checkout,
			Verdict:  work.Advance(),
		}
	}

	started := dispatch(pass, candidate("set-b", ""), occupancy)
	blocked := dispatch(pass, candidate("set-d", occupiedCheckout), occupancy)
	failed := dispatch(pass, candidate("set-c", ""), occupancy)

	for _, event := range []work.AdvanceEvent{started, blocked, failed} {
		if event.Kind != ref.KindTaskSet {
			t.Fatalf("event kind = %q, want the kind that produced the candidate", event.Kind)
		}
		if event.Ref.ContainerID == "" || event.Label != "repo/"+event.Ref.ContainerID {
			t.Fatalf("event = %+v, want the candidate's ref and label", event)
		}
	}
	if started.Err != nil || started.Line() != "queue: repo: spawned drain for set-b" {
		t.Fatalf("start event = %+v, want the kind's own message", started)
	}
	if blocked.Err != nil || blocked.Line() != "queue: repo/set-d: skip; "+(drain.SpawnDeferral{Reason: drain.DeferCheckoutClaim, Claim: claimed}).Message() {
		t.Fatalf("occupancy event = %+v, want the supervisor's refusal", blocked)
	}
	if !errors.Is(failed.Err, spawnErr) || failed.Line() != spawnErr.Error() {
		t.Fatalf("failure event = %+v, want the kind's error rendered as it worded it", failed)
	}
	if failed.Outcome.Message != "" {
		t.Fatalf("failure event carried an outcome message %q, want none", failed.Outcome.Message)
	}
}

// TestRunOutputSeedStaysDrivenByTheKind pins the half of reporting that does not
// generalize: the Task-set run-output view diff is a Task-set snapshot type, so
// the supervisor seeds it from the owning kind's own hook and asks nothing of a
// kind that has no such snapshot — which is exactly why the hook is not on the
// seam.
func TestRunOutputSeedStaysDrivenByTheKind(t *testing.T) {
	reporter := &spawningAdvancer{
		RecordingAdvancer: &queuetest.RecordingAdvancer{
			Kind:          ref.KindTaskSet,
			CandidateList: []work.Candidate{{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Label: "repo/set-a", Verdict: work.Advance()}},
		},
		spawned: []drain.PickedUpSet{{Project: "pop", RepoLabel: "pop", SetID: "set-a"}},
	}
	mapKind := &queuetest.RecordingAdvancer{
		Kind:          ref.KindMap,
		CandidateList: []work.Candidate{{Ref: ref.WorkRef{Kind: ref.KindMap, ContainerID: "map-a"}, Label: "repo/map-a", Verdict: work.Advance()}},
	}

	var out bytes.Buffer
	runOut := newRunOutputState()
	tick(supervisorDepsOver(claimingTasksDeps(t), reporter, mapKind), &out, runOut)

	if runOut.prev == nil {
		t.Fatal("the post-spawn view was never recorded")
	}
	if len(runOut.prev.Running) != 1 || runOut.prev.Running[0].SetID != "set-a" {
		t.Fatalf("seeded running = %+v, want only the reporting kind's spawned set", runOut.prev.Running)
	}
}

// spawningAdvancer is a kind that also reports what it spawned, the shape the
// Task-set adapter has beside the seam.
type spawningAdvancer struct {
	*queuetest.RecordingAdvancer
	spawned []drain.PickedUpSet
}

func (k *spawningAdvancer) SpawnedSets() []drain.PickedUpSet { return k.spawned }

// meetingPoint blocks each arrival until every participant has arrived, so a test
// can tell concurrent phases from serial ones without depending on timing.
type meetingPoint struct {
	mu    sync.Mutex
	want  int
	count int
	open  chan struct{}
}

func newMeetingPoint(want int) *meetingPoint {
	return &meetingPoint{want: want, open: make(chan struct{})}
}

// arrive reports whether everyone showed up; false is the shape a serial caller
// produces, where the first arrival waits for a partner that cannot come.
func (m *meetingPoint) arrive() bool {
	m.mu.Lock()
	m.count++
	if m.count == m.want {
		close(m.open)
	}
	m.mu.Unlock()
	select {
	case <-m.open:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}
