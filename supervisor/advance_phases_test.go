package supervisor

import (
	"bytes"
	"errors"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// TestSupervisorReconcilesBeforeCandidatesThenDispatches pins the phase order the
// seam defines — reconcile, then a pure candidate read, then dispatch — and that
// a refusal verdict reaches dispatch instead of being filtered out before it.
func TestSupervisorReconcilesBeforeCandidatesThenDispatches(t *testing.T) {
	kind := &queuetest.RecordingAdvancer{
		CandidateList: []work.Candidate{
			{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Label: "repo/set-a", Verdict: work.Advance()},
			{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-b"}, Label: "repo/set-b", Verdict: work.Refuse("set parked after repeated abnormal drain exits")},
		},
		Message: func(c work.Candidate) string { return "work: " + c.Label + " handled" },
	}

	var out bytes.Buffer
	tick(supervisorTestDeps(t, kind), &out, newRunOutputState())

	want := []string{
		"reconcile",
		"candidates",
		"advance task-set:set-a",
		"advance task-set:set-b",
	}
	if strings.Join(kind.Calls(), ",") != strings.Join(want, ",") {
		t.Fatalf("supervisor drove the kind as %v, want %v", kind.Calls(), want)
	}
	for _, line := range []string{"work: repo/set-a handled", "work: repo/set-b handled"} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("supervisor output missing %q:\n%s", line, out.String())
		}
	}
}

// TestSupervisorReportsAdvanceFailure pins that a kind's dispatch error is
// reported as the kind worded it and never halts the rest of the pass.
func TestSupervisorReportsAdvanceFailure(t *testing.T) {
	kind := &queuetest.RecordingAdvancer{
		CandidateList: []work.Candidate{
			{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Label: "repo/set-a", Verdict: work.Advance()},
			{Ref: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-b"}, Label: "repo/set-b", Verdict: work.Advance()},
		},
		Err: func(c work.Candidate) error {
			if c.Ref.ContainerID == "set-a" {
				return errors.New("work: repo: spawn set-a: tmux refused pane")
			}
			return nil
		},
		Message: func(c work.Candidate) string { return "work: " + c.Label + " handled" },
	}

	var out bytes.Buffer
	tick(supervisorTestDeps(t, kind), &out, newRunOutputState())

	if !strings.Contains(out.String(), "work: repo: spawn set-a: tmux refused pane") {
		t.Fatalf("supervisor output missing the kind's failure line:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "work: repo/set-b handled") {
		t.Fatalf("one candidate's failure must not stop the pass:\n%s", out.String())
	}
}

// TestSupervisorStopsOnCandidateError pins that a scan failure is reported once
// and dispatches nothing — the pre-seam behaviour, now expressed as the kind's
// own error crossing the seam.
func TestSupervisorStopsOnCandidateError(t *testing.T) {
	kind := &queuetest.RecordingAdvancer{CandidatesErr: errors.New("work: scan: store unreadable")}

	var out bytes.Buffer
	tick(supervisorTestDeps(t, kind), &out, newRunOutputState())

	if !strings.Contains(out.String(), "work: scan: store unreadable") {
		t.Fatalf("supervisor output missing the scan error:\n%s", out.String())
	}
	for _, call := range kind.Calls() {
		if strings.HasPrefix(call, "advance") {
			t.Fatalf("candidate failure must dispatch nothing, got %v", kind.Calls())
		}
	}
}

// supervisorTestDeps wires a supervisor over one synthetic kind and a config with
// no projects, so the tick exercises the seam and nothing else.
func supervisorTestDeps(t *testing.T, kind work.Kind) *drain.Deps {
	t.Helper()
	cfg := &config.Config{}
	return &drain.Deps{
		Tasks:      queuetest.TasksDeps(t, true),
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		Kinds:      func(*config.Config) []work.Kind { return []work.Kind{kind} },
	}
}
