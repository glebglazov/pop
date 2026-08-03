package deps

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// recordingGit answers every question with the call count it was asked at, so a
// memoized answer is distinguishable from a fresh one.
type recordingGit struct {
	mu    sync.Mutex
	calls []string
	fail  bool
}

func (g *recordingGit) Command(args ...string) (string, error) {
	return g.record("", args)
}

func (g *recordingGit) CommandInDir(dir string, args ...string) (string, error) {
	return g.record(dir, args)
}

func (g *recordingGit) record(dir string, args []string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, dir+" :: "+strings.Join(args, " "))
	if g.fail {
		return "", errors.New("boom")
	}
	return dir + "#" + string(rune('a'+len(g.calls)-1)), nil
}

func (g *recordingGit) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.calls)
}

func TestMemoGitForksOncePerQuestion(t *testing.T) {
	inner := &recordingGit{}
	memo := NewMemoGit(inner)

	first, err := memo.CommandInDir("/repo", "rev-parse", "--git-common-dir")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		got, err := memo.CommandInDir("/repo", "rev-parse", "--git-common-dir")
		if err != nil || got != first {
			t.Fatalf("repeat %d = %q (err %v), want %q", i, got, err, first)
		}
	}
	// A different question about the same checkout, and the same question about
	// another checkout, are both their own fork.
	if _, err := memo.CommandInDir("/repo", "rev-parse", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, err := memo.CommandInDir("/other", "rev-parse", "--git-common-dir"); err != nil {
		t.Fatal(err)
	}
	if got := inner.count(); got != 3 {
		t.Fatalf("inner git forked %d times, want 3", got)
	}
	// A path spelled unclean is the same checkout.
	if _, err := memo.CommandInDir("/repo/sub/..", "rev-parse", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if got := inner.count(); got != 3 {
		t.Fatalf("inner git forked %d times after an unclean spelling, want 3", got)
	}
}

func TestMemoGitPassesThroughVolatileCommands(t *testing.T) {
	inner := &recordingGit{}
	memo := NewMemoGit(inner)

	for _, args := range [][]string{
		{"status", "--porcelain"},
		{"worktree", "add", "/wt", "HEAD"},
		{"commit", "-m", "x"},
	} {
		for i := 0; i < 2; i++ {
			if _, err := memo.CommandInDir("/repo", args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := inner.count(); got != 6 {
		t.Fatalf("inner git forked %d times, want 6 (nothing memoized)", got)
	}
	// A cwd-relative invocation names no checkout, so it is never memoized.
	for i := 0; i < 2; i++ {
		if _, err := memo.Command("rev-parse", "HEAD"); err != nil {
			t.Fatal(err)
		}
	}
	if got := inner.count(); got != 8 {
		t.Fatalf("inner git forked %d times, want 8", got)
	}
}

// TestMemoGitLifetimeIsTheMemo pins the scope: nothing about a checkout is
// remembered past the memo that asked, so the next load's memo re-reads a moved
// HEAD.
func TestMemoGitLifetimeIsTheMemo(t *testing.T) {
	inner := &recordingGit{}

	first, err := NewMemoGit(inner).CommandInDir("/repo", "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewMemoGit(inner).CommandInDir("/repo", "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("second memo replayed %q instead of re-reading", first)
	}
	if got := inner.count(); got != 2 {
		t.Fatalf("inner git forked %d times across two memos, want 2", got)
	}
}

// TestMemoGitRemembersFailures pins that a failed read is answered once too: a
// load with an unreadable checkout must not pay a fork per caller for the same
// failure.
func TestMemoGitRemembersFailures(t *testing.T) {
	inner := &recordingGit{fail: true}
	memo := NewMemoGit(inner)

	for i := 0; i < 3; i++ {
		if _, err := memo.CommandInDir("/repo", "rev-parse", "HEAD"); err == nil {
			t.Fatalf("call %d: want an error", i)
		}
	}
	if got := inner.count(); got != 1 {
		t.Fatalf("inner git forked %d times, want 1", got)
	}
}

func TestMemoGitIsConcurrencySafe(t *testing.T) {
	inner := &recordingGit{}
	memo := NewMemoGit(inner)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := memo.CommandInDir("/repo", "rev-parse", "HEAD"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := inner.count(); got > 32 {
		t.Fatalf("inner git forked %d times, want at most one per racing caller", got)
	}
}
