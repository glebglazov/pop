package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// TestClassifyPresence covers ADR-0199 decision 3's three states.
func TestClassifyPresence(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMUX_TMPDIR", root)
	uid := os.Getuid()
	sockDir := filepath.Join(root, fmt.Sprintf("tmux-%d", uid))
	if err := os.MkdirAll(sockDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := classifyPresence("", "pop"); got != presenceOutside {
		t.Fatalf("empty $TMUX = %v, want outside", got)
	}
	if got := classifyPresence("", ""); got != presenceOutside {
		t.Fatalf("empty $TMUX with unset socket = %v, want outside", got)
	}

	popSock := filepath.Join(sockDir, "pop")
	if got := classifyPresence(popSock+",1,0", "pop"); got != presenceInside {
		t.Fatalf("matching socket = %v, want inside", got)
	}
	if got := classifyPresence("/anywhere,1,0", ""); got != presenceInside {
		t.Fatalf("unset socket with $TMUX = %v, want inside", got)
	}

	defaultSock := filepath.Join(sockDir, "default")
	if got := classifyPresence(defaultSock+",1,0", "pop"); got != presenceForeign {
		t.Fatalf("foreign socket = %v, want foreign", got)
	}
}

// TestForeignServerErrorNamesBothSocketsAndFixes proves the nest-refusal
// message names the configured socket, the caller's socket, and both ways out.
func TestForeignServerErrorNamesBothSocketsAndFixes(t *testing.T) {
	err := foreignServerError("/tmp/tmux-501/default,1,0", "pop")
	if err == nil {
		t.Fatal("expected foreign refusal")
	}
	msg := err.Error()
	for _, want := range []string{"pop", "default", "tmux.socket", "Detach"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal %q missing %q", msg, want)
		}
	}
	if err := foreignServerError("", "pop"); err != nil {
		t.Errorf("outside must not refuse: %v", err)
	}
	if err := foreignServerError("/tmp/tmux-501/pop,1,0", "pop"); err != nil {
		t.Errorf("matching socket must not refuse: %v", err)
	}
	if err := foreignServerError("/anywhere,1,0", ""); err != nil {
		t.Errorf("unset socket must not refuse: %v", err)
	}
}

// TestSwitchTargetForeignRefusesWithoutTouchingTMUX proves ADR-0199 decision
// 3: a foreign server is refused, $TMUX is left alone, and neither switch nor
// attach runs.
func TestSwitchTargetForeignRefusesWithoutTouchingTMUX(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMUX_TMPDIR", root)
	uid := os.Getuid()
	sockDir := filepath.Join(root, fmt.Sprintf("tmux-%d", uid))
	if err := os.MkdirAll(sockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(sockDir, "default") + ",1,0"
	t.Setenv("TMUX", foreign)

	rec := &recordingRunner{}
	tm := &realTmux{run: rec, socket: "pop"}
	err := SwitchTarget(tm, "work")
	if err == nil {
		t.Fatal("expected foreign refusal")
	}
	msg := err.Error()
	if !strings.Contains(msg, "pop") || !strings.Contains(msg, "default") {
		t.Errorf("refusal must name both sockets, got %q", msg)
	}
	if !strings.Contains(msg, "tmux.socket") || !strings.Contains(msg, "Detach") {
		t.Errorf("refusal must name both fixes, got %q", msg)
	}
	if got := os.Getenv("TMUX"); got != foreign {
		t.Errorf("$TMUX rewritten to %q, want left as %q", got, foreign)
	}
	if len(rec.calls) != 0 || len(rec.attachCalls) != 0 {
		t.Errorf("tmux must not be invoked on refusal, got calls=%v attach=%v", rec.calls, rec.attachCalls)
	}
}

// TestSwitchTargetInsideAndOutsideUnchanged pins decision 3's non-foreign
// paths: configured-socket → switch-client, no-tmux → attach-session.
func TestSwitchTargetInsideAndOutsideUnchanged(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMUX_TMPDIR", root)
	uid := os.Getuid()
	sockDir := filepath.Join(root, fmt.Sprintf("tmux-%d", uid))
	if err := os.MkdirAll(sockDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("inside configured switches", func(t *testing.T) {
		t.Setenv("TMUX", filepath.Join(sockDir, "pop")+",1,0")
		rec := &recordingRunner{}
		tm := &realTmux{run: rec, socket: "pop"}
		if err := SwitchTarget(tm, "work"); err != nil {
			t.Fatal(err)
		}
		if len(rec.calls) != 1 || rec.calls[0][0] != "switch-client" {
			t.Fatalf("calls = %v, want switch-client", rec.calls)
		}
		if len(rec.attachCalls) != 0 {
			t.Fatalf("attachCalls = %v, want none", rec.attachCalls)
		}
	})

	t.Run("outside attaches", func(t *testing.T) {
		t.Setenv("TMUX", "")
		rec := &recordingRunner{}
		tm := &realTmux{run: rec, socket: "pop"}
		if err := SwitchTarget(tm, "work"); err != nil {
			t.Fatal(err)
		}
		if len(rec.attachCalls) != 1 || rec.attachCalls[0][0] != "attach-session" {
			t.Fatalf("attachCalls = %v, want attach-session", rec.attachCalls)
		}
		if len(rec.calls) != 0 {
			t.Fatalf("calls = %v, want none", rec.calls)
		}
	})
}
