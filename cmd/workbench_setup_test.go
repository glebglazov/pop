package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

func TestWorkbenchSetupGroupsThroughApply(t *testing.T) {
	for _, create := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("create=%t/fail=%t", create, fail), func(t *testing.T) {
				dir := t.TempDir()
				shellPath := filepath.Join(dir, "human-shell")
				if err := os.WriteFile(shellPath, []byte("#!/bin/sh\n[ \"$1\" = -i ] || exit 97\nshift\nexec /bin/sh \"$@\"\n"), 0700); err != nil {
					t.Fatal(err)
				}
				// Both commands must start before either can finish. A sequential runner
				// fails the bounded rendezvous instead of hanging the test.
				command := func(name, sibling string, exit int) string {
					return fmt.Sprintf(`test -f first || exit 90; if read input; then exit 91; fi; touch %s-start; i=0; until test -f %s-start; do i=$((i+1)); test "$i" -lt 100 || exit 92; sleep 0.01; done; printf '%s-out\n'; printf '%s-err\n' >&2; touch %s-done; exit %d`, name, sibling, name, name, name, exit)
				}
				code := 0
				if fail {
					code = 7
				}
				left := command("left", "right", 0)
				right := command("right", "left", code)
				wb := config.Workbench{Name: "dev", Windows: []config.WorkbenchWindow{{Name: "work", Layout: &config.WorkbenchPaneSpec{Name: "shell", Command: "sh"}}}, BeforeApply: config.SetupEntries{
					{Command: "touch first"}, {Parallel: []string{left, right}},
					{Command: "test -f left-done && test -f right-done && touch last"},
				}}
				mod := &tmuxtest.Fake{Shell: shellPath}
				var output bytes.Buffer
				d := templateRuntimeDeps{Tmux: mod, UserHomeDir: func() (string, error) { return dir, nil }, RunBeforeApply: runBeforeApplyCommand, Out: &output, ErrOut: &output}
				var err error
				if create {
					err = createSessionFromWorkbench(d, wb, "session", dir)
				} else {
					err = applyWorkbench(d, wb, "session", dir)
				}
				wantOutput := "left-out\nleft-err\nright-out\nright-err\n"
				if fail {
					wantOutput = "right-out\nright-err\nleft-out\nleft-err\n"
					if err == nil || !strings.Contains(err.Error(), `parallel[1]`) || !strings.Contains(err.Error(), fmt.Sprintf("%q", right)) {
						t.Fatalf("failure = %v", err)
					}
					if len(mod.WBWindowIdentity) != 0 {
						t.Fatal("window realized after failed setup")
					}
					if _, err := os.Stat(filepath.Join(dir, "last")); !os.IsNotExist(err) {
						t.Fatal("entry after failed group ran")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if _, err := os.Stat(filepath.Join(dir, "last")); err != nil {
						t.Fatalf("barrier: %v", err)
					}
				}
				if !strings.HasSuffix(output.String(), wantOutput) {
					t.Fatalf("output = %q, want held output suffix %q", output.String(), wantOutput)
				}
				for _, command := range []string{left, right} {
					if !strings.Contains(output.String(), "Setup: "+command+" [running 0s]") {
						t.Fatalf("output does not show %q starting: %q", command, output.String())
					}
				}
				wantFinal := "finished"
				if fail {
					wantFinal = "failed"
				}
				if !strings.Contains(output.String(), "Setup: "+right+" ["+wantFinal+" ") {
					t.Fatalf("output does not show %q ending as %s: %q", right, wantFinal, output.String())
				}
				for _, name := range []string{"left-done", "right-done"} {
					if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
						t.Fatalf("sibling incomplete: %v", err)
					}
				}
			})
		}
	}
}

func TestSetupGroupWaitsBeforeOutputAndReportsDeclaredFailure(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	done := make(chan error, 1)
	var output bytes.Buffer
	d := templateRuntimeDeps{RunBeforeApply: func(_ tmuxmod.Tmux, command, dir string, stdin io.Reader, stdout, stderr io.Writer) error {
		if stdin != nil {
			return fmt.Errorf("unexpected stdin")
		}
		fmt.Fprint(stdout, "held:"+command)
		started <- command
		<-release
		if command != "one" {
			return fmt.Errorf("failed %s", command)
		}
		return nil
	}}
	go func() { done <- runSetupGroup(d, []string{"one", "two", "three"}, "", &output) }()
	defer close(release)
	for range 3 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("group did not start together")
		}
	}
	if strings.Contains(output.String(), "held:") {
		t.Fatalf("held output printed before barrier: %q", output.String())
	}
	// Release with a separate signal for each sibling; the deferred close also
	// unblocks them if an assertion above fails.
	for range 3 {
		release <- struct{}{}
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), `parallel[1] "two"`) {
			t.Fatalf("failure = %v", err)
		}
		if !strings.HasSuffix(output.String(), "held:twoheld:oneheld:three") {
			t.Fatalf("output = %q", output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("group did not finish")
	}
}

func TestSetupProgressRefreshesOneTerminalLine(t *testing.T) {
	started := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	var output bytes.Buffer
	progress := newSetupProgress(&output, []string{"npm install", "go generate"}, setupProgressDeps{
		now:      func() time.Time { return started },
		terminal: func(io.Writer) bool { return true },
	})
	progress.start()
	progress.commands[0].finished = started.Add(120 * time.Millisecond)
	progress.refresh(started.Add(260*time.Millisecond), false)
	progress.commands[1].finished = started.Add(340 * time.Millisecond)
	progress.commands[1].failed = true
	progress.refresh(started.Add(340*time.Millisecond), true)

	want := "\r\033[2KSetup: npm install [running 0s] | go generate [running 0s]" +
		"\r\033[2KSetup: npm install [finished 100ms] | go generate [running 300ms]" +
		"\r\033[2KSetup: npm install [finished 100ms] | go generate [failed 300ms]\n"
	if output.String() != want {
		t.Fatalf("terminal progress = %q, want %q", output.String(), want)
	}
}

type fakeSetupProgressTicker struct{ ticks chan time.Time }

func (t *fakeSetupProgressTicker) Chan() <-chan time.Time { return t.ticks }
func (t *fakeSetupProgressTicker) Stop()                  {}

type setupProgressWrites struct{ writes chan string }

func (w setupProgressWrites) Write(p []byte) (int, error) {
	w.writes <- string(p)
	return len(p), nil
}

func TestSetupGroupRefreshesWhileCommandsAreRunning(t *testing.T) {
	startedAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	ticker := &fakeSetupProgressTicker{ticks: make(chan time.Time)}
	writes := make(chan string, 16)
	out := setupProgressWrites{writes: writes}
	progress := newSetupProgress(out, []string{"one", "two"}, setupProgressDeps{
		now:       func() time.Time { return startedAt },
		newTicker: func(time.Duration) setupProgressTicker { return ticker },
		terminal:  func(io.Writer) bool { return true },
	})
	commandsStarted := make(chan struct{}, 2)
	release := make(chan struct{})
	d := templateRuntimeDeps{RunBeforeApply: func(_ tmuxmod.Tmux, command, _ string, _ io.Reader, stdout, _ io.Writer) error {
		commandsStarted <- struct{}{}
		<-release
		fmt.Fprint(stdout, "held:"+command)
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- runSetupGroupWithProgress(d, []string{"one", "two"}, "", progress) }()

	if got := <-writes; !strings.Contains(got, "one [running 0s] | two [running 0s]") {
		t.Fatalf("initial progress = %q", got)
	}
	<-commandsStarted
	<-commandsStarted
	ticker.ticks <- startedAt.Add(200 * time.Millisecond)
	if got := <-writes; !strings.Contains(got, "one [running 200ms] | two [running 200ms]") {
		t.Fatalf("in-flight progress = %q", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	close(writes)
	var remaining strings.Builder
	for write := range writes {
		remaining.WriteString(write)
	}
	if !strings.Contains(remaining.String(), "one [finished 0s] | two [finished 0s]\n") {
		t.Fatalf("final progress = %q", remaining.String())
	}
	if !strings.HasSuffix(remaining.String(), "held:oneheld:two") {
		t.Fatalf("held output did not follow progress: %q", remaining.String())
	}
}

func TestSetupProgressWithoutTerminalPrintsStartAndFinishLines(t *testing.T) {
	started := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	times := []time.Time{started, started.Add(120 * time.Millisecond), started.Add(260 * time.Millisecond)}
	var output bytes.Buffer
	progress := newSetupProgress(&output, []string{"npm install", "go generate"}, setupProgressDeps{
		now: func() time.Time {
			now := times[0]
			times = times[1:]
			return now
		},
		terminal: func(io.Writer) bool { return false },
	})
	progress.start()
	progress.finish(0, false)
	progress.finish(1, true)

	want := "Setup: npm install [running 0s]\n" +
		"Setup: go generate [running 0s]\n" +
		"Setup: npm install [finished 100ms]\n" +
		"Setup: go generate [failed 300ms]\n"
	if output.String() != want {
		t.Fatalf("plain progress = %q, want %q", output.String(), want)
	}
}

func TestSingleSetupCommandStaysQuiet(t *testing.T) {
	var output bytes.Buffer
	d := templateRuntimeDeps{
		Out: &output,
		RunBeforeApply: func(_ tmuxmod.Tmux, _ string, _ string, _ io.Reader, _, _ io.Writer) error {
			return nil
		},
	}
	if err := runWorkbenchSetup(d, config.SetupEntries{{Command: "git pull"}}, "/repo"); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("single Setup command output = %q, want quiet", output.String())
	}
}
