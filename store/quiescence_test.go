package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// holdAlive builds a gate-hold liveness predicate over the (pid, procStart)
// pairs it should treat as alive, mirroring aliveByToken for drains.
func holdAliveByToken(alive ...[2]string) func(int, string) bool {
	type key struct {
		pid       string
		procStart string
	}
	live := map[key]bool{}
	for _, a := range alive {
		live[key{a[0], a[1]}] = true
	}
	return func(pid int, procStart string) bool {
		return live[key{fmt.Sprint(pid), procStart}]
	}
}

func acceptVerdict(setID string) VerifyVerdict {
	return VerifyVerdict{
		Repo: "r", SetID: setID, WorkSHA: "sha1", Verdict: "PASS",
		Scope: 1, HumanAuthored: true, Note: "human ok", ComputedAt: time.Now().UTC(),
	}
}

func TestMutateIfCheckoutQuiescentWritesWhenIdle(t *testing.T) {
	s := openTestStore(t)
	occ, err := s.MutateIfCheckoutQuiescent("/rt", func(ctx context.Context, ex Execer) error {
		return PutVerifyVerdictExec(ctx, ex, acceptVerdict("s"))
	})
	if err != nil {
		t.Fatalf("MutateIfCheckoutQuiescent: %v", err)
	}
	if occ != nil {
		t.Fatalf("expected no occupant on idle checkout, got %+v", occ)
	}
	v, err := s.GetVerifyVerdict("r", "s", "sha1")
	if err != nil || v == nil {
		t.Fatalf("verdict not committed: v=%+v err=%v", v, err)
	}
	if !v.HumanAuthored || v.Verdict != "PASS" {
		t.Fatalf("wrong verdict committed: %+v", v)
	}
}

func TestMutateIfCheckoutQuiescentRefusedByLiveDrain(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	if _, err := s.StartDrain(Drain{Repo: "r", SetID: "busyset", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	ran := false
	occ, err := s.MutateIfCheckoutQuiescent("/rt", func(ctx context.Context, ex Execer) error {
		ran = true
		return PutVerifyVerdictExec(ctx, ex, acceptVerdict("busyset"))
	})
	if !errors.Is(err, ErrCheckoutBusy) {
		t.Fatalf("err = %v, want ErrCheckoutBusy", err)
	}
	if ran {
		t.Fatalf("mutation must not run while checkout is busy")
	}
	if occ == nil || occ.Kind != OccupantDrain || occ.SetID != "busyset" || occ.PID != 100 {
		t.Fatalf("occupant = %+v, want live drain busyset/100", occ)
	}
	if v, _ := s.GetVerifyVerdict("r", "busyset", "sha1"); v != nil {
		t.Fatalf("no verdict must be written on refusal, got %+v", v)
	}
}

func TestMutateIfCheckoutQuiescentRefusedByLiveGateHold(t *testing.T) {
	s := openTestStore(t, holdAliveByToken([2]string{"200", "h1"}))
	if err := s.PutCheckoutGateHold(CheckoutGateHold{RuntimePath: "/rt", SetID: "gated", PID: 200, ProcStart: "h1"}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}
	occ, err := s.MutateIfCheckoutQuiescent("/rt", func(ctx context.Context, ex Execer) error {
		return PutVerifyVerdictExec(ctx, ex, acceptVerdict("gated"))
	})
	if !errors.Is(err, ErrCheckoutBusy) {
		t.Fatalf("err = %v, want ErrCheckoutBusy", err)
	}
	if occ == nil || occ.Kind != OccupantGateHold || occ.SetID != "gated" || occ.PID != 200 {
		t.Fatalf("occupant = %+v, want gate hold gated/200", occ)
	}
}

func TestMutateIfCheckoutQuiescentIgnoresDeadDrain(t *testing.T) {
	// Owner PID 100/t1 is not in the alive set → dead → does not block.
	s := openTestStore(t, aliveByToken())
	if _, err := s.StartDrain(Drain{Repo: "r", SetID: "s", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	occ, err := s.MutateIfCheckoutQuiescent("/rt", func(ctx context.Context, ex Execer) error {
		return PutVerifyVerdictExec(ctx, ex, acceptVerdict("s"))
	})
	if err != nil || occ != nil {
		t.Fatalf("dead drain must not block: occ=%+v err=%v", occ, err)
	}
	if v, _ := s.GetVerifyVerdict("r", "s", "sha1"); v == nil {
		t.Fatalf("verdict should be committed past a dead drain")
	}
}

func TestMutateIfCheckoutQuiescentDetectsReusedDrainPID(t *testing.T) {
	// PID 100 is alive but now a different process (t2) → not this drain → dead.
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t2"}))
	if _, err := s.StartDrain(Drain{Repo: "r", SetID: "s", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	occ, err := s.MutateIfCheckoutQuiescent("/rt", func(ctx context.Context, ex Execer) error {
		return PutVerifyVerdictExec(ctx, ex, acceptVerdict("s"))
	})
	if err != nil || occ != nil {
		t.Fatalf("reused-PID drain must not block: occ=%+v err=%v", occ, err)
	}
}

func TestMutateIfCheckoutQuiescentIgnoresDeadGateHold(t *testing.T) {
	// Hold owner is dead (empty alive set) → does not block.
	s := openTestStore(t, holdAliveByToken())
	if err := s.PutCheckoutGateHold(CheckoutGateHold{RuntimePath: "/rt", SetID: "gated", PID: 200, ProcStart: "h1"}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}
	occ, err := s.MutateIfCheckoutQuiescent("/rt", func(ctx context.Context, ex Execer) error {
		return PutVerifyVerdictExec(ctx, ex, acceptVerdict("gated"))
	})
	if err != nil || occ != nil {
		t.Fatalf("dead gate hold must not block: occ=%+v err=%v", occ, err)
	}
}

func TestMutateIfCheckoutQuiescentRollsBackOnMutateError(t *testing.T) {
	s := openTestStore(t)
	boom := errors.New("boom")
	occ, err := s.MutateIfCheckoutQuiescent("/rt", func(ctx context.Context, ex Execer) error {
		// Write, then fail: the transaction must roll back so nothing persists.
		if e := PutVerifyVerdictExec(ctx, ex, acceptVerdict("s")); e != nil {
			return e
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if occ != nil {
		t.Fatalf("no occupant on mutate error, got %+v", occ)
	}
	if v, _ := s.GetVerifyVerdict("r", "s", "sha1"); v != nil {
		t.Fatalf("mutate error must roll back the write, got %+v", v)
	}
}

// TestMutateIfCheckoutQuiescentBlocksConcurrentStartDrain proves the check and
// the write are atomic against a concurrent BeginDrain: while the gate holds its
// BEGIN IMMEDIATE transaction, a StartDrain issued from a second connection on
// the same database cannot commit a running row. So no drain can appear between
// the quiescence check and the mutation — the window ADR-0104 closes.
func TestMutateIfCheckoutQuiescentBlocksConcurrentStartDrain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pop.db")
	s, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	s2, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	defer s2.Close()

	inWindow := make(chan struct{})
	drainDone := make(chan error, 1)
	go func() {
		<-inWindow
		_, e := s2.StartDrain(Drain{Repo: "r", SetID: "racer", RuntimePath: "/rt", PID: 222, ProcStart: "t2", StartedAt: time.Now()})
		drainDone <- e
	}()

	occ, err := s.MutateIfCheckoutQuiescent("/rt", func(ctx context.Context, ex Execer) error {
		close(inWindow)
		// The competing StartDrain is now racing for the write lock we hold; it
		// must not commit while we are inside the transaction.
		select {
		case e := <-drainDone:
			t.Errorf("StartDrain committed inside the quiescence window (err=%v)", e)
		case <-time.After(300 * time.Millisecond):
		}
		return PutVerifyVerdictExec(ctx, ex, acceptVerdict("safe"))
	})
	if err != nil || occ != nil {
		t.Fatalf("gated mutation failed: occ=%+v err=%v", occ, err)
	}
	// Our human PASS committed; it was never at risk of an interleaved drain.
	if v, _ := s.GetVerifyVerdict("r", "safe", "sha1"); v == nil {
		t.Fatalf("gated verdict not committed")
	}
	// Drain the goroutine so the test does not leak it; its outcome (success once
	// we released, or a snapshot conflict) is irrelevant — it was serialized after
	// us either way.
	select {
	case <-drainDone:
	case <-time.After(6 * time.Second):
		t.Fatalf("competing StartDrain never returned")
	}
}
