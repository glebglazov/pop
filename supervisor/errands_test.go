package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/errand"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks/drain"
)

type daemonNotice chan string

func (n daemonNotice) Write(p []byte) (int, error) {
	select {
	case n <- string(p):
	default:
	}
	return len(p), nil
}

func TestRunningDaemonWakesForRemovalAndKeepsJournal(t *testing.T) {
	td := queuetest.DataDeps(t)
	repo := t.TempDir()
	queuetest.InitGitRepo(t, repo)
	checkout := filepath.Join(t.TempDir(), "feature")
	if output, err := exec.Command("git", "-C", repo, "worktree", "add", "-b", "feature", checkout).CombinedOutput(); err != nil {
		t.Fatalf("worktree: %v: %s", err, output)
	}
	d := &drain.Deps{
		Tasks: td, Project: project.DefaultDeps(),
		Tmux:       queuetest.NewRecordingTmux(false, "0"),
		LoadConfig: func(string) (*config.Config, error) { return &config.Config{}, nil },
	}
	signals := make(chan os.Signal, 1)
	finished := make(chan error, 1)
	notices := make(daemonNotice, 100)
	go func() { finished <- Run(d, time.Hour, notices, signals) }()
	t.Cleanup(func() {
		signals <- os.Interrupt
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("daemon did not stop")
		}
	})
	waitNotice := func(match string) {
		t.Helper()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case line := <-notices:
				if strings.Contains(line, match) {
					return
				}
			case <-timer.C:
				t.Fatalf("daemon did not report %q", match)
			}
		}
	}
	waitNotice("errands running; work running")
	if live := ReadLiveness(td); !live.Errands || !live.Work {
		t.Fatalf("full daemon liveness = %+v, want both halves", live)
	}
	// A one-hour Work interval must not delay a human-requested Errand.
	if err := errand.QueueCheckoutRemoval(td, store.CheckoutRemoval{Path: checkout, WorkingPath: repo}); err != nil {
		t.Fatal(err)
	}
	waitNotice(fmt.Sprintf("errand: removed checkout %s", checkout))
	if _, err := os.Stat(checkout); !os.IsNotExist(err) {
		t.Fatalf("checkout remains: %v", err)
	}
	events, err := BuildLog(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "checkout_removed" || events[0].RuntimePath != checkout {
		t.Fatalf("journal: %+v", events)
	}
	s, _, err := td.Store(false)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListErrands()
	if err != nil || len(rows) != 0 {
		t.Fatalf("completed row: %+v, %v", rows, err)
	}
}

func TestErrandHalfRunsRemovalWithoutStartingWorkHalf(t *testing.T) {
	td := queuetest.DataDeps(t)
	repo := t.TempDir()
	queuetest.InitGitRepo(t, repo)
	checkout := filepath.Join(t.TempDir(), "feature")
	if output, err := exec.Command("git", "-C", repo, "worktree", "add", "-b", "feature", checkout).CombinedOutput(); err != nil {
		t.Fatalf("worktree: %v: %s", err, output)
	}
	signals := make(chan os.Signal, 1)
	finished := make(chan error, 1)
	notices := make(daemonNotice, 100)
	go func() { finished <- RunErrands(td, project.DefaultDeps(), notices, signals) }()
	waitFor := func(match string) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case line := <-notices:
				if strings.Contains(line, match) {
					return
				}
			case <-deadline:
				t.Fatalf("Errand half did not report %q", match)
			}
		}
	}
	waitFor("errands running; work stopped")
	if live := ReadLiveness(td); !live.Errands || live.Work {
		t.Fatalf("liveness = %+v, want only Errands", live)
	}
	if err := errand.QueueCheckoutRemoval(td, store.CheckoutRemoval{Path: checkout, WorkingPath: repo}); err != nil {
		t.Fatal(err)
	}
	waitFor("errand: removed checkout")
	if _, err := os.Stat(checkout); !os.IsNotExist(err) {
		t.Fatalf("checkout remains: %v", err)
	}
	signals <- os.Interrupt
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
