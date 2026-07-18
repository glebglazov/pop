package store

import (
	"sync"
	"testing"
)

// TestPutBindingIfAbsentInsertsWhenEmpty: a first writer into an empty key
// inserts its row and reports it back.
func TestPutBindingIfAbsentInsertsWhenEmpty(t *testing.T) {
	s := openTestStore(t)

	want := Binding{ScopedKey: "repo\x00set", RuntimePath: "/wt/a", Branch: "feature", Provisioned: false}
	inserted, got, err := s.PutBindingIfAbsent(want)
	if err != nil {
		t.Fatalf("PutBindingIfAbsent: %v", err)
	}
	if !inserted {
		t.Fatalf("first insert into empty key must report inserted=true")
	}
	if got != want {
		t.Fatalf("returned row = %+v, want %+v", got, want)
	}
	stored, ok, err := s.LookupBinding(want.ScopedKey)
	if err != nil || !ok {
		t.Fatalf("LookupBinding after insert: ok=%v err=%v", ok, err)
	}
	if stored != want {
		t.Fatalf("stored row = %+v, want %+v", stored, want)
	}
}

// TestPutBindingIfAbsentRefusesOverwrite: a second writer into an occupied key
// never overwrites — it reports inserted=false and returns the existing row.
func TestPutBindingIfAbsentRefusesOverwrite(t *testing.T) {
	s := openTestStore(t)

	first := Binding{ScopedKey: "repo\x00set", RuntimePath: "/wt/first", Provisioned: true}
	if _, _, err := s.PutBindingIfAbsent(first); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	second := Binding{ScopedKey: "repo\x00set", RuntimePath: "/wt/second", Provisioned: false}
	inserted, existing, err := s.PutBindingIfAbsent(second)
	if err != nil {
		t.Fatalf("PutBindingIfAbsent: %v", err)
	}
	if inserted {
		t.Fatalf("insert into occupied key must report inserted=false")
	}
	if existing != first {
		t.Fatalf("loser must see existing row %+v, got %+v", first, existing)
	}
	stored, _, err := s.LookupBinding(first.ScopedKey)
	if err != nil {
		t.Fatalf("LookupBinding: %v", err)
	}
	if stored != first {
		t.Fatalf("row must be untouched: got %+v, want %+v", stored, first)
	}
}

// TestPutBindingIfAbsentConcurrentWritersOneWins proves the check-then-insert is
// atomic under a concurrent-writer scenario: two writers race the same key with
// different rows, exactly one wins the insert, and the loser sees the winner's
// existing row — no clobber (ADR-0118). Because PutBindingIfAbsent holds the
// connection across the SELECT and INSERT inside one BEGIN IMMEDIATE
// transaction, the two writers cannot interleave into a lost update.
func TestPutBindingIfAbsentConcurrentWritersOneWins(t *testing.T) {
	s := openTestStore(t)

	key := "repo\x00set"
	writers := []Binding{
		{ScopedKey: key, RuntimePath: "/wt/A", Branch: "a", Provisioned: true},
		{ScopedKey: key, RuntimePath: "/wt/B", Branch: "b", Provisioned: false},
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
		results [2]struct {
			inserted bool
			existing Binding
			err      error
		}
	)
	start := make(chan struct{})
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			inserted, existing, err := s.PutBindingIfAbsent(writers[i])
			results[i].inserted = inserted
			results[i].existing = existing
			results[i].err = err
			if err == nil && inserted {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range results {
		if results[i].err != nil {
			t.Fatalf("writer %d errored: %v", i, results[i].err)
		}
	}
	if winners != 1 {
		t.Fatalf("exactly one writer must win the insert, got %d", winners)
	}

	// The stored row must be exactly one of the two writers' rows, untouched.
	stored, ok, err := s.LookupBinding(key)
	if err != nil || !ok {
		t.Fatalf("LookupBinding after race: ok=%v err=%v", ok, err)
	}
	if stored != writers[0] && stored != writers[1] {
		t.Fatalf("stored row %+v matches neither writer", stored)
	}

	// The loser must have observed that same winning row — it saw an existing
	// binding, never clobbered it.
	for i := range results {
		if results[i].inserted {
			continue
		}
		if results[i].existing != stored {
			t.Fatalf("loser saw %+v, want the winning row %+v", results[i].existing, stored)
		}
	}
}
