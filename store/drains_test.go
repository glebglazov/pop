package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "pop.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenIsWALAndMigrated(t *testing.T) {
	s := openTestStore(t)

	var jm string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&jm); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if jm != "wal" {
		t.Fatalf("journal_mode = %q, want wal", jm)
	}
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("user_version = %d, want %d", version, len(migrations))
	}
	// drains table exists.
	if _, err := s.db.Exec(`SELECT count(*) FROM drains`); err != nil {
		t.Fatalf("drains table missing: %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pop.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	d, err := s.StartDrain(Drain{Repo: "r", SetID: "s", RuntimePath: "/rt", PID: 1, StartedAt: time.Now()}, allAlive(false))
	if err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	_ = s.Close()

	// Reopening runs migrate again with no outstanding steps and preserves data.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got, err := s2.LiveDrainByRuntimePath("/rt", allAlive(true))
	if err != nil {
		t.Fatalf("LiveDrainByRuntimePath: %v", err)
	}
	if got == nil || got.ID != d.ID {
		t.Fatalf("running drain not preserved across reopen: %+v", got)
	}
}

func allAlive(v bool) func(int) bool { return func(int) bool { return v } }

func TestStartDrainInsertsRunning(t *testing.T) {
	s := openTestStore(t)
	start := time.Now().UTC().Truncate(time.Second)
	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt", PID: 4242, StartedAt: start}, allAlive(false))
	if err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("unexpected drain %+v", d)
	}
	live, err := s.LiveDrainByRuntimePath("/rt", allAlive(true))
	if err != nil {
		t.Fatalf("LiveDrainByRuntimePath: %v", err)
	}
	if live == nil {
		t.Fatal("expected a live running drain")
	}
	if live.PID != 4242 || live.SetID != "set" || !live.StartedAt.Equal(start) {
		t.Fatalf("running drain fields wrong: %+v", live)
	}
}

func TestStartDrainRefusesConcurrentSameSet(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt-a", PID: 1, StartedAt: time.Now()}, allAlive(false)); err != nil {
		t.Fatalf("first StartDrain: %v", err)
	}
	// Same (repo, set) in a different checkout, first PID alive → refused.
	conflict, err := s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt-b", PID: 2, StartedAt: time.Now()}, allAlive(true))
	if !errors.Is(err, ErrDrainInProgress) {
		t.Fatalf("err = %v, want ErrDrainInProgress", err)
	}
	if conflict.PID != 1 {
		t.Fatalf("conflict drain = %+v, want the live drain (PID 1)", conflict)
	}
}

func TestStartDrainRefusesConcurrentSameCheckout(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-a", RuntimePath: "/rt", PID: 1, StartedAt: time.Now()}, allAlive(false)); err != nil {
		t.Fatalf("first StartDrain: %v", err)
	}
	// Different set but same checkout, first PID alive → refused.
	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, StartedAt: time.Now()}, allAlive(true)); !errors.Is(err, ErrDrainInProgress) {
		t.Fatalf("err = %v, want ErrDrainInProgress", err)
	}
}

func TestStartDrainStaleRunningDoesNotBlock(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt", PID: 1, StartedAt: time.Now()}, allAlive(false)); err != nil {
		t.Fatalf("first StartDrain: %v", err)
	}
	// First PID dead → stale running row does not block a new start.
	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt", PID: 2, StartedAt: time.Now()}, allAlive(false)); err != nil {
		t.Fatalf("second StartDrain over stale row: %v", err)
	}
}

func TestFinishDrainTransitionsTerminal(t *testing.T) {
	s := openTestStore(t)
	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt", PID: 1, StartedAt: time.Now()}, allAlive(false))
	if err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	reset := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if err := s.FinishDrain(d.ID, StateQuotaPaused, "claude", true, reset, time.Now().UTC()); err != nil {
		t.Fatalf("FinishDrain: %v", err)
	}
	if live, _ := s.LiveDrainByRuntimePath("/rt", allAlive(true)); live != nil {
		t.Fatalf("expected no live drain after finish, got %+v", live)
	}
	term, err := s.LatestTerminalByRuntimePath("/rt")
	if err != nil {
		t.Fatalf("LatestTerminalByRuntimePath: %v", err)
	}
	if term == nil {
		t.Fatal("expected a terminal drain")
	}
	if term.State != StateQuotaPaused || term.ExhaustedPreset != "claude" || !term.ExhaustedPinned {
		t.Fatalf("terminal fields wrong: %+v", term)
	}
	if !term.ExhaustedResetAt.Equal(reset) {
		t.Fatalf("reset = %v, want %v", term.ExhaustedResetAt, reset)
	}
	// A finished set no longer blocks a fresh drain.
	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt", PID: 2, StartedAt: time.Now()}, allAlive(true)); err != nil {
		t.Fatalf("StartDrain after terminal: %v", err)
	}
}

func TestFinishedTerminalOmitsQuotaFields(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt", PID: 1, StartedAt: time.Now()}, allAlive(false))
	if err := s.FinishDrain(d.ID, StateFinished, "", false, time.Time{}, time.Now().UTC()); err != nil {
		t.Fatalf("FinishDrain: %v", err)
	}
	term, _ := s.LatestTerminalByRuntimePath("/rt")
	if term.State != StateFinished {
		t.Fatalf("state = %q, want finished", term.State)
	}
	if term.ExhaustedPreset != "" || term.ExhaustedPinned || !term.ExhaustedResetAt.IsZero() {
		t.Fatalf("finished terminal carries quota fields: %+v", term)
	}
}

// TestStartDrainConcurrentSeparateConnectionsAdmitsOne models competing
// processes: each opens its own connection to the same database and races to
// claim the same (repo, set). The BEGIN IMMEDIATE transaction must admit exactly
// one and refuse the rest.
func TestStartDrainConcurrentSeparateConnectionsAdmitsOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pop.db")
	// Create the schema once up front so racing opens skip migration writes.
	if s, err := Open(path); err != nil {
		t.Fatal(err)
	} else {
		_ = s.Close()
	}

	const racers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var admitted, refused int
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(pid int) {
			defer wg.Done()
			s, err := Open(path)
			if err != nil {
				t.Errorf("open: %v", err)
				return
			}
			defer s.Close()
			_, err = s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt", PID: pid, StartedAt: time.Now()}, allAlive(true))
			mu.Lock()
			switch {
			case err == nil:
				admitted++
			case errors.Is(err, ErrDrainInProgress):
				refused++
			default:
				t.Errorf("unexpected err: %v", err)
			}
			mu.Unlock()
		}(1000 + i)
	}
	wg.Wait()
	if admitted != 1 || refused != racers-1 {
		t.Fatalf("admitted = %d, refused = %d, want 1 and %d", admitted, refused, racers-1)
	}
}

func TestCancelDrainRemovesRow(t *testing.T) {
	s := openTestStore(t)
	d, _ := s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt", PID: 1, StartedAt: time.Now()}, allAlive(false))
	if err := s.CancelDrain(d.ID); err != nil {
		t.Fatalf("CancelDrain: %v", err)
	}
	if live, _ := s.LiveDrainByRuntimePath("/rt", allAlive(true)); live != nil {
		t.Fatalf("expected no live drain after cancel, got %+v", live)
	}
	if term, _ := s.LatestTerminalByRuntimePath("/rt"); term != nil {
		t.Fatalf("cancel must leave no terminal, got %+v", term)
	}
	// A cancelled set is free to start again.
	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set", RuntimePath: "/rt", PID: 2, StartedAt: time.Now()}, allAlive(true)); err != nil {
		t.Fatalf("StartDrain after cancel: %v", err)
	}
}
