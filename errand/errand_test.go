package errand_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/errand"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
)

func TestRemovalFailureRetryAndJournal(t *testing.T) {
	td := queuetest.DataDeps(t)
	repo := t.TempDir()
	queuetest.InitGitRepo(t, repo)
	checkout := filepath.Join(t.TempDir(), "feature")
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-b", "feature", checkout)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree: %v: %s", err, out)
	}
	subject := store.CheckoutRemoval{Path: checkout, WorkingPath: repo, Branch: "feature", Force: true}
	if err := errand.QueueCheckoutRemoval(td, subject); err != nil {
		t.Fatal(err)
	}
	s, _, err := td.Store(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutHistoryEntry(checkout, td.Now()); err != nil {
		t.Fatal(err)
	}
	real := deps.NewRealFileSystem()
	attempts := 0
	pd := project.DefaultDeps()
	pd.Holders = nil
	pd.FS = &deps.MockFileSystem{
		ReadDirFunc: real.ReadDir, StatFunc: real.Stat,
		RemoveAllFunc: func(path string) error {
			attempts++
			return os.ErrPermission
		},
	}
	if err := errand.Tick(td, pd, io.Discard); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListErrands()
	if err != nil || len(rows) != 1 || rows[0].State != store.ErrandFailed {
		t.Fatalf("failure: %+v, %v", rows, err)
	}
	body, err := os.ReadFile(rows[0].OutputPath)
	if err != nil || !strings.Contains(string(body), checkout) {
		t.Fatalf("report: %s, %v", body, err)
	}
	firstAttempts := attempts
	if err := errand.Tick(td, pd, io.Discard); err != nil {
		t.Fatal(err)
	}
	if attempts != firstAttempts {
		t.Fatal("failed removal retried automatically")
	}
	if err := errand.QueueCheckoutRemoval(td, subject); err != nil {
		t.Fatal(err)
	}
	if err := errand.QueueCheckoutRemoval(td, subject); err != nil {
		t.Fatal(err)
	}
	rows, err = s.ListErrands()
	if err != nil || len(rows) != 1 || rows[0].State != store.ErrandQueued {
		t.Fatalf("retry: %+v, %v", rows, err)
	}
	// The daemon must walk the subject as it exists now, including late writes.
	if err := os.WriteFile(filepath.Join(checkout, "late-write"), []byte("late"), 0o644); err != nil {
		t.Fatal(err)
	}
	pd.FS = real
	if err := errand.Tick(td, pd, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(checkout); !os.IsNotExist(err) {
		t.Fatalf("checkout remains: %v", err)
	}
	rows, err = s.ListErrands()
	if err != nil || len(rows) != 0 {
		t.Fatalf("completed row remains: %+v, %v", rows, err)
	}
	events, err := s.ListErrandCompletions()
	if err != nil || len(events) != 1 || events[0].Path != checkout {
		t.Fatalf("journal: %+v, %v", events, err)
	}
	hist, err := s.AllHistoryEntries()
	if err != nil || len(hist) != 0 {
		t.Fatalf("History: %+v, %v", hist, err)
	}
	for _, args := range [][]string{{"worktree", "list", "--porcelain"}, {"branch", "--list", "feature"}} {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil || strings.Contains(string(out), "feature") {
			t.Fatalf("Git residue: %s, %v", out, err)
		}
	}
}

func TestInterruptedRemovalIsNotRetried(t *testing.T) {
	td := queuetest.DataDeps(t)
	checkout := t.TempDir()
	subject := store.CheckoutRemoval{Path: checkout, WorkingPath: t.TempDir()}
	if err := errand.QueueCheckoutRemoval(td, subject); err != nil {
		t.Fatal(err)
	}
	s, _, err := td.Store(false)
	if err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(t.TempDir(), "output.md")
	if err := os.WriteFile(report, []byte("partial output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if started, err := s.StartErrand(checkout, report); err != nil || !started {
		t.Fatalf("start: %v, %v", started, err)
	}
	if err := td.CloseStore(); err != nil {
		t.Fatal(err)
	}
	if err := errand.Recover(td); err != nil {
		t.Fatal(err)
	}
	if err := errand.Tick(td, project.DefaultDeps(), io.Discard); err != nil {
		t.Fatal(err)
	}
	s, _, err = td.Store(false)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListErrands()
	if err != nil || len(rows) != 1 || rows[0].State != store.ErrandFailed {
		t.Fatalf("recovery: %+v, %v", rows, err)
	}
	body, err := os.ReadFile(rows[0].OutputPath)
	if err != nil || !strings.Contains(string(body), "Interrupted") {
		t.Fatalf("report: %s, %v", body, err)
	}
	if _, err := os.Stat(checkout); err != nil {
		t.Fatalf("interrupted removal ran again: %v", err)
	}
}
