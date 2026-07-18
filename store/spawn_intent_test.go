package store

import (
	"testing"
	"time"
)

// aliveIntentsByToken builds a spawn-intent liveness predicate over the (pid,
// proc_start) pairs it should treat as alive. A pair that is absent reads dead —
// modelling both a dead PID and a reused PID (same pid, different token).
func aliveIntentsByToken(alive ...SpawnIntent) Liveness {
	type key struct {
		pid       int
		procStart string
	}
	live := map[key]bool{}
	for _, si := range alive {
		live[key{si.PID, si.ProcStart}] = true
	}
	return func(pid int, procStart string) bool { return live[key{pid, procStart}] }
}

func TestSpawnIntentRoundTripsAndFilterByFreshness(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	fresh := SpawnIntent{Repo: "/repo", SetID: "s1", RuntimePath: "/rt1", PID: 100, ProcStart: "t1", CreatedAt: now}
	stale := SpawnIntent{Repo: "/repo", SetID: "s2", RuntimePath: "/rt2", PID: 101, ProcStart: "t2", CreatedAt: now.Add(-10 * time.Minute)}
	otherRepo := SpawnIntent{Repo: "/other", SetID: "s3", RuntimePath: "/rt3", PID: 102, ProcStart: "t3", CreatedAt: now}
	for _, si := range []SpawnIntent{fresh, stale, otherRepo} {
		if err := s.PutSpawnIntent(si); err != nil {
			t.Fatalf("PutSpawnIntent: %v", err)
		}
	}

	got, err := s.SpawnIntentsForRepo("/repo", now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("SpawnIntentsForRepo: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d intents, want 1 (only the fresh one for /repo): %+v", len(got), got)
	}
	if got[0].SetID != "s1" || got[0].RuntimePath != "/rt1" || got[0].PID != 100 || got[0].ProcStart != "t1" {
		t.Fatalf("intent not round-tripped: %+v", got[0])
	}
}

func TestPutSpawnIntentUpsertsSameKey(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	if err := s.PutSpawnIntent(SpawnIntent{Repo: "/repo", SetID: "s1", RuntimePath: "/old", PID: 1, CreatedAt: now.Add(-time.Minute)}); err != nil {
		t.Fatalf("PutSpawnIntent: %v", err)
	}
	if err := s.PutSpawnIntent(SpawnIntent{Repo: "/repo", SetID: "s1", RuntimePath: "/new", PID: 2, ProcStart: "t2", CreatedAt: now}); err != nil {
		t.Fatalf("PutSpawnIntent (upsert): %v", err)
	}
	got, err := s.SpawnIntentsForRepo("/repo", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("SpawnIntentsForRepo: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d intents, want 1 (upsert must not duplicate): %+v", len(got), got)
	}
	if got[0].RuntimePath != "/new" || got[0].PID != 2 {
		t.Fatalf("upsert did not replace: %+v", got[0])
	}
}

func TestDeleteSpawnIntent(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	if err := s.PutSpawnIntent(SpawnIntent{Repo: "/repo", SetID: "s1", PID: 1, CreatedAt: now}); err != nil {
		t.Fatalf("PutSpawnIntent: %v", err)
	}
	if err := s.DeleteSpawnIntent("/repo", "s1"); err != nil {
		t.Fatalf("DeleteSpawnIntent: %v", err)
	}
	got, err := s.SpawnIntentsForRepo("/repo", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("SpawnIntentsForRepo: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("intent survived delete: %+v", got)
	}
}

func TestReconcileSpawnIntentsSweepsExpired(t *testing.T) {
	now := time.Now().UTC()
	// Owner is alive, but the intent is older than the cutoff: the spawn never
	// reached BeginDrain, so expiry must sweep it regardless of liveness.
	expired := SpawnIntent{Repo: "/repo", SetID: "s1", PID: 100, ProcStart: "t1", CreatedAt: now.Add(-10 * time.Minute)}
	s := openTestStore(t, aliveIntentsByToken(expired))
	if err := s.PutSpawnIntent(expired); err != nil {
		t.Fatalf("PutSpawnIntent: %v", err)
	}
	n, err := s.ReconcileSpawnIntents(now.Add(-2 * time.Minute))
	if err != nil {
		t.Fatalf("ReconcileSpawnIntents: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1 (expired)", n)
	}
	if got, _ := s.SpawnIntentsForRepo("/repo", now.Add(-time.Hour)); len(got) != 0 {
		t.Fatalf("expired intent survived sweep: %+v", got)
	}
}

func TestReconcileSpawnIntentsSweepsDeadOwner(t *testing.T) {
	s := openTestStore(t, aliveIntentsByToken()) // nothing alive
	now := time.Now().UTC()
	// Fresh (not expired) but its recording process is gone.
	fresh := SpawnIntent{Repo: "/repo", SetID: "s1", PID: 100, ProcStart: "t1", CreatedAt: now}
	if err := s.PutSpawnIntent(fresh); err != nil {
		t.Fatalf("PutSpawnIntent: %v", err)
	}
	n, err := s.ReconcileSpawnIntents(now.Add(-2 * time.Minute))
	if err != nil {
		t.Fatalf("ReconcileSpawnIntents: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1 (dead owner)", n)
	}
	if got, _ := s.SpawnIntentsForRepo("/repo", now.Add(-time.Hour)); len(got) != 0 {
		t.Fatalf("dead-owner intent survived sweep: %+v", got)
	}
}

func TestReconcileSpawnIntentsLeavesFreshLiveOwner(t *testing.T) {
	now := time.Now().UTC()
	live := SpawnIntent{Repo: "/repo", SetID: "s1", PID: 100, ProcStart: "t1", CreatedAt: now}
	s := openTestStore(t, aliveIntentsByToken(live))
	if err := s.PutSpawnIntent(live); err != nil {
		t.Fatalf("PutSpawnIntent: %v", err)
	}
	n, err := s.ReconcileSpawnIntents(now.Add(-2 * time.Minute))
	if err != nil {
		t.Fatalf("ReconcileSpawnIntents: %v", err)
	}
	if n != 0 {
		t.Fatalf("swept %d, want 0 (fresh + live owner)", n)
	}
	if got, _ := s.SpawnIntentsForRepo("/repo", now.Add(-time.Hour)); len(got) != 1 {
		t.Fatalf("fresh live-owner intent was wrongly swept: %+v", got)
	}
}

func TestReconcileSpawnIntentsSweepsReusedPID(t *testing.T) {
	// PID 100 is alive again but a different process (start token t2).
	reused := SpawnIntent{PID: 100, ProcStart: "t2"}
	s := openTestStore(t, aliveIntentsByToken(reused))
	now := time.Now().UTC()
	if err := s.PutSpawnIntent(SpawnIntent{Repo: "/repo", SetID: "s1", PID: 100, ProcStart: "t1", CreatedAt: now}); err != nil {
		t.Fatalf("PutSpawnIntent: %v", err)
	}
	n, err := s.ReconcileSpawnIntents(now.Add(-2 * time.Minute))
	if err != nil {
		t.Fatalf("ReconcileSpawnIntents: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1 (PID reused)", n)
	}
}

func TestReconcileSpawnIntentsSweepsOnlyExpiredWhenOwnersLive(t *testing.T) {
	now := time.Now().UTC()
	// Both owners read alive, so only the expired intent is swept — expiry is
	// independent of liveness.
	s := openTestStore(t, aliveIntentsByToken(
		SpawnIntent{PID: 1}, SpawnIntent{PID: 2}))
	if err := s.PutSpawnIntent(SpawnIntent{Repo: "/repo", SetID: "fresh", PID: 1, CreatedAt: now}); err != nil {
		t.Fatalf("PutSpawnIntent: %v", err)
	}
	if err := s.PutSpawnIntent(SpawnIntent{Repo: "/repo", SetID: "old", PID: 2, CreatedAt: now.Add(-10 * time.Minute)}); err != nil {
		t.Fatalf("PutSpawnIntent: %v", err)
	}
	n, err := s.ReconcileSpawnIntents(now.Add(-2 * time.Minute))
	if err != nil {
		t.Fatalf("ReconcileSpawnIntents: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1 (only the expired one)", n)
	}
	if got, _ := s.SpawnIntentsForRepo("/repo", now.Add(-time.Hour)); len(got) != 1 || got[0].SetID != "fresh" {
		t.Fatalf("nil-predicate sweep should keep the fresh intent: %+v", got)
	}
}
