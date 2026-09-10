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
				want := "left-out\nleft-err\nright-out\nright-err\n"
				if fail {
					want = "right-out\nright-err\nleft-out\nleft-err\n"
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
				if output.String() != want {
					t.Fatalf("output = %q, want %q", output.String(), want)
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
		fmt.Fprint(stdout, command)
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
	if output.Len() != 0 {
		t.Fatalf("output before barrier: %q", output.String())
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
		if output.String() != "twoonethree" {
			t.Fatalf("output = %q", output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("group did not finish")
	}
}
