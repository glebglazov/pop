package queuetest

import (
	"sync"

	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// RecordingAdvancer is a Work kind that also advances, recording the order the
// supervisor drives its phases in. The phase hooks let a test hold one kind
// inside a phase while another enters it, which is how the concurrent phases are
// told apart from a serial loop.
type RecordingAdvancer struct {
	Kind          work.KindID
	CandidateList []work.Candidate
	CandidatesErr error
	Message       func(work.Candidate) string
	Err           func(work.Candidate) error
	OnReconcile   func()
	OnCandidates  func()
	OnAdvance     func(work.Candidate)

	mu    sync.Mutex
	calls []string
}

// Calls is the ordered phase log the supervisor drove this kind through.
func (k *RecordingAdvancer) Calls() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.calls...)
}

func (k *RecordingAdvancer) record(call string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, call)
}

func (k *RecordingAdvancer) ID() work.KindID {
	if k.Kind == "" {
		return ref.KindTaskSet
	}
	return k.Kind
}
func (k *RecordingAdvancer) Load() ([]work.Container, error) { return nil, nil }
func (k *RecordingAdvancer) Less(a, b work.Container) bool   { return a.ID < b.ID }
func (k *RecordingAdvancer) StatusCell(work.Container) []work.StatusSegment {
	return nil
}
func (k *RecordingAdvancer) Actions(work.Container) []work.Action                { return nil }
func (k *RecordingAdvancer) StatusActions(work.Container) []work.Action          { return nil }
func (k *RecordingAdvancer) CopyActions(work.Container) []work.Action            { return nil }
func (k *RecordingAdvancer) ItemActions(work.Container, work.Item) []work.Action { return nil }
func (k *RecordingAdvancer) Perform(work.Container, *work.Item, work.Verb) (work.Outcome, error) {
	return work.Outcome{}, nil
}
func (k *RecordingAdvancer) Summary([]work.Container) []string { return nil }
func (k *RecordingAdvancer) Columns() []string                 { return nil }

func (k *RecordingAdvancer) Reconcile() error {
	k.record("reconcile")
	if k.OnReconcile != nil {
		k.OnReconcile()
	}
	return nil
}

func (k *RecordingAdvancer) Candidates() ([]work.Candidate, error) {
	k.record("candidates")
	if k.OnCandidates != nil {
		k.OnCandidates()
	}
	if k.CandidatesErr != nil {
		return nil, k.CandidatesErr
	}
	return k.CandidateList, nil
}

func (k *RecordingAdvancer) Advance(c work.Candidate) (work.Outcome, error) {
	k.record("advance " + c.Ref.String())
	if k.OnAdvance != nil {
		k.OnAdvance(c)
	}
	if k.Err != nil {
		if err := k.Err(c); err != nil {
			return work.Outcome{}, err
		}
	}
	msg := ""
	if k.Message != nil {
		msg = k.Message(c)
	}
	return work.Outcome{Kind: work.OutcomeMessage, Message: msg}, nil
}
