package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Argument construction for each lifecycle primitive is asserted here, once
// per verb, against the recording runner.

func TestHasSessionBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if !tm.HasSession("work") {
		t.Fatal("HasSession = false, want true on zero-exit")
	}
	want := [][]string{{"has-session", "-t=work"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestHasSessionFalseOnError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("no such session")}}
	if tm.HasSession("gone") {
		t.Fatal("HasSession = true, want false on non-zero exit")
	}
}

func TestNewSessionBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.NewSession("work", "/proj"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"new-session", "-ds", "work", "-c", "/proj"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestSwitchClientBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.SwitchClient("%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"switch-client", "-t", "%5"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestAttachSessionUsesAttachRunner(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.AttachSession("work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// attach-session must go through the stdio-wired attach seam, not output.
	if len(r.calls) != 0 {
		t.Fatalf("output calls = %v, want none", r.calls)
	}
	want := [][]string{{"attach-session", "-t", "work"}}
	if !reflect.DeepEqual(r.attachCalls, want) {
		t.Fatalf("attach args = %v, want %v", r.attachCalls, want)
	}
}

func TestKillSessionBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.KillSession("work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"kill-session", "-t", "work"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestKillSessionPropagatesError(t *testing.T) {
	tm := &realTmux{run: &recordingRunner{err: fmt.Errorf("boom")}}
	if err := tm.KillSession("work"); err == nil {
		t.Fatal("expected error")
	}
}

// TestInConfiguredServer covers ADR-0199 decision 2: InTmux is a
// socket-identity comparison, with the unset-socket case preserving the
// pre-socket-key "$TMUX != \"\"" behaviour.
func TestInConfiguredServer(t *testing.T) {
	t.Run("empty TMUX is outside", func(t *testing.T) {
		if inConfiguredServer("", "") {
			t.Fatal("empty $TMUX must be outside")
		}
		if inConfiguredServer("", "pop") {
			t.Fatal("empty $TMUX must be outside even with a configured socket")
		}
	})

	t.Run("unset socket with TMUX is inside", func(t *testing.T) {
		if !inConfiguredServer("/tmp/tmux-501/default,1,0", "") {
			t.Fatal("unset socket must treat any $TMUX as inside")
		}
	})

	t.Run("foreign socket is outside", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("TMUX_TMPDIR", root)
		uid := os.Getuid()
		defaultSock := filepath.Join(root, fmt.Sprintf("tmux-%d", uid), "default")
		if err := os.MkdirAll(filepath.Dir(defaultSock), 0o755); err != nil {
			t.Fatal(err)
		}
		if inConfiguredServer(defaultSock+",1,0", "pop") {
			t.Fatal("inside default while configured for pop must be outside")
		}
	})

	t.Run("matching socket is inside", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("TMUX_TMPDIR", root)
		uid := os.Getuid()
		popSock := filepath.Join(root, fmt.Sprintf("tmux-%d", uid), "pop")
		if err := os.MkdirAll(filepath.Dir(popSock), 0o755); err != nil {
			t.Fatal(err)
		}
		if !inConfiguredServer(popSock+",1,0", "pop") {
			t.Fatal("matching socket must be inside")
		}
	})
}

// TestInConfiguredServerSymlinkResolved proves the macOS /tmp vs /private/tmp
// case: constructing the configured path yields the unresolved side while
// $TMUX reports the resolved side, so a naive string compare fails toward
// "always outside".
func TestInConfiguredServerSymlinkResolved(t *testing.T) {
	root := t.TempDir()
	privateTmp := filepath.Join(root, "private", "tmp")
	if err := os.MkdirAll(privateTmp, 0o755); err != nil {
		t.Fatal(err)
	}
	tmpLink := filepath.Join(root, "tmp")
	if err := os.Symlink(privateTmp, tmpLink); err != nil {
		t.Fatal(err)
	}

	uid := os.Getuid()
	sockDirName := fmt.Sprintf("tmux-%d", uid)
	sockName := "default"
	if err := os.MkdirAll(filepath.Join(tmpLink, sockDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedSock := filepath.Join(privateTmp, sockDirName, sockName)
	if err := os.WriteFile(resolvedSock, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TMUX_TMPDIR", tmpLink)
	configured := configuredSocketPath(sockName)
	envPath := resolvedSock
	if configured == envPath {
		t.Fatalf("precondition failed: configured %q already equals env path — naive compare would pass", configured)
	}
	tmuxEnv := envPath + ",1234,0"
	if !inConfiguredServer(tmuxEnv, sockName) {
		t.Fatalf("resolved paths must match: configured=%q env=%q resolvedConfigured=%q resolvedEnv=%q",
			configured, envPath, resolvePath(configured), resolvePath(envPath))
	}
}

func TestRealTmuxInTmuxUsesSocket(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMUX_TMPDIR", root)
	uid := os.Getuid()
	popSock := filepath.Join(root, fmt.Sprintf("tmux-%d", uid), "pop")
	if err := os.MkdirAll(filepath.Dir(popSock), 0o755); err != nil {
		t.Fatal(err)
	}

	tm := New("pop").(*realTmux)

	t.Setenv("TMUX", "")
	if tm.InTmux() {
		t.Fatal("empty $TMUX must be outside")
	}

	t.Setenv("TMUX", popSock+",1,0")
	if !tm.InTmux() {
		t.Fatal("matching configured socket must be inside")
	}

	t.Setenv("TMUX", filepath.Join(root, fmt.Sprintf("tmux-%d", uid), "default")+",1,0")
	if tm.InTmux() {
		t.Fatal("foreign socket must be outside")
	}

	unset := New("").(*realTmux)
	t.Setenv("TMUX", "/anywhere,1,0")
	if !unset.InTmux() {
		t.Fatal("unset socket must treat any $TMUX as inside")
	}
}
