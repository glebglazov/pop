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

// TestRewriteBindingRuntimePathPrefixRepointsOnlyMatchingRows: the storage-layout
// move repoints every binding under the old root and leaves every other recorded
// checkout — an adopted one, a sibling directory sharing a name prefix — alone.
func TestRewriteBindingRuntimePathPrefixRepointsOnlyMatchingRows(t *testing.T) {
	s := openTestStore(t)

	rows := []Binding{
		{ScopedKey: "repo\x00set-a", RuntimePath: "/data/pop/queue/worktrees/repo-abc/set-a", Branch: "pop/set-a", Provisioned: true},
		{ScopedKey: "repo\x00set-b", RuntimePath: "/data/pop/queue/worktrees/repo-abc/set-b", Branch: "pop/set-b", Provisioned: true},
		{ScopedKey: "repo\x00adopted", RuntimePath: "/home/me/checkouts/feature", Branch: "feature"},
		// A sibling of the root whose name starts with it: the prefix carries a
		// separator so this must not match.
		{ScopedKey: "repo\x00sibling", RuntimePath: "/data/pop/queue/worktrees-old/repo-abc/set-c", Branch: "pop/set-c", Provisioned: true},
	}
	for _, b := range rows {
		if err := s.PutBinding(b); err != nil {
			t.Fatalf("seed %s: %v", b.ScopedKey, err)
		}
	}

	n, err := s.RewriteBindingRuntimePathPrefix("/data/pop/queue/worktrees/", "/data/pop/work/worktrees/")
	if err != nil {
		t.Fatalf("RewriteBindingRuntimePathPrefix: %v", err)
	}
	if n != 2 {
		t.Fatalf("rewrote %d rows, want 2", n)
	}

	want := map[string]string{
		"repo\x00set-a":   "/data/pop/work/worktrees/repo-abc/set-a",
		"repo\x00set-b":   "/data/pop/work/worktrees/repo-abc/set-b",
		"repo\x00adopted": "/home/me/checkouts/feature",
		"repo\x00sibling": "/data/pop/queue/worktrees-old/repo-abc/set-c",
	}
	all, err := s.AllBindings()
	if err != nil {
		t.Fatalf("AllBindings: %v", err)
	}
	for key, path := range want {
		got, ok := all[key]
		if !ok {
			t.Fatalf("binding %q vanished", key)
		}
		if got.RuntimePath != path {
			t.Fatalf("binding %q runtime path = %q, want %q", key, got.RuntimePath, path)
		}
	}
	// The rewrite touches runtime_path and nothing else.
	if b := all["repo\x00set-a"]; b.Branch != "pop/set-a" || !b.Provisioned {
		t.Fatalf("rewrite changed more than the path: %+v", b)
	}

	// Re-running finds nothing left to do.
	again, err := s.RewriteBindingRuntimePathPrefix("/data/pop/queue/worktrees/", "/data/pop/work/worktrees/")
	if err != nil {
		t.Fatalf("second rewrite: %v", err)
	}
	if again != 0 {
		t.Fatalf("second rewrite touched %d rows, want 0", again)
	}
}

// TestRewriteBindingRuntimePathPrefixRequiresBothPrefixes: an empty prefix would
// match (or produce) every path, so it is refused rather than guessed at.
func TestRewriteBindingRuntimePathPrefixRequiresBothPrefixes(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.RewriteBindingRuntimePathPrefix("", "/data/pop/work/worktrees/"); err == nil {
		t.Fatal("empty old prefix must be refused")
	}
	if _, err := s.RewriteBindingRuntimePathPrefix("/data/pop/queue/worktrees/", ""); err == nil {
		t.Fatal("empty new prefix must be refused")
	}
}
