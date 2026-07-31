package tasks

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

// The two tests below drive RealCommandRunner.RunAttended directly over a real
// `true` binary: they exercise the tty/process-group foreground handover that
// only exists on the real path, so they stay in realShimSmokeSet (ADR-0144)
// rather than routing through the in-process fake.

// TestRunAttendedNonTerminalStdinPlainExec checks that a non-tty stdin skips
// foreground process-group handover and execs plainly without error.
func TestRunAttendedNonTerminalStdinPlainExec(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	code, err := RealCommandRunner{}.RunAttended(context.Background(), ".", r, os.Stdout, os.Stderr, "true")
	if err != nil {
		t.Fatalf("RunAttended: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

// TestRunAttendedTTYNotStdinFd checks that foreground handover uses the tty fd
// wired to the child, not pop's fd 0, so a caller with redirected stdin can
// still spawn an attended child on a separately-opened terminal.
func TestRunAttendedTTYNotStdinFd(t *testing.T) {
	if _, err := os.Stat("/dev/tty"); err != nil {
		t.Skip("no /dev/tty")
	}
	tty, err := os.Open("/dev/tty")
	if err != nil {
		t.Skipf("cannot open /dev/tty: %v", err)
	}
	defer tty.Close()

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()

	oldStdin := os.Stdin
	os.Stdin = devNull
	defer func() { os.Stdin = oldStdin }()

	if tty.Fd() == 0 {
		t.Skip("tty unexpectedly got fd 0")
	}

	code, err := RealCommandRunner{}.RunAttended(context.Background(), ".", tty, os.Stdout, os.Stderr, "true")
	if err != nil {
		t.Fatalf("RunAttended with tty not on fd 0: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

// TestStartWithEnvLayersInvocationEnvOverParentEnvironment drives the real
// runner over a real `sh` because merging into a spawned process's environment
// is exactly the OS behavior under test (see realShimSmokeSet).
func TestStartWithEnvLayersInvocationEnvOverParentEnvironment(t *testing.T) {
	t.Setenv("POP_ENV_CARRIER_PROBE", "parent-value")
	var out strings.Builder
	proc, err := RealCommandRunner{}.StartWithEnv(
		context.Background(), ".",
		[]string{"KIMI_CODE_NO_AUTO_UPDATE=1", "POP_ENV_CARRIER_PROBE=invocation-value"},
		&out, &out,
		"sh", "-c", `printf '%s %s %s' "$KIMI_CODE_NO_AUTO_UPDATE" "$POP_ENV_CARRIER_PROBE" "${PATH:+path-inherited}"`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if code, err := proc.Wait(); err != nil || code != 0 {
		t.Fatalf("exit code = %d, err = %v", code, err)
	}
	if got := out.String(); got != "1 invocation-value path-inherited" {
		t.Fatalf("child environment = %q, want the invocation entries to win over an inherited value while the rest of the environment survives", got)
	}
}

// envCapturingRunner records the env an invocation asked for. Its Start path
// fails the test: an invocation carrying env must never reach it.
type envCapturingRunner struct {
	t   *testing.T
	env []string
}

func (r *envCapturingRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (r *envCapturingRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	r.t.Fatalf("invocation with env took the env-blind Start path: %s %v", name, args)
	return nil, nil
}

func (r *envCapturingRunner) StartWithEnv(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	r.env = env
	proc := &ManagedProcess{done: make(chan waitResult, 1)}
	proc.done <- waitResult{}
	return proc, nil
}

// envBlindRunner implements only CommandRunner, standing in for a runner that
// cannot carry an invocation's environment.
type envBlindRunner struct{}

func (envBlindRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (envBlindRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return nil, nil
}

func TestStartAgentInvocationRoutesEnvToTheRunner(t *testing.T) {
	invocation, err := ResolveAgentInvocation("kimi", "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	runner := &envCapturingRunner{t: t}
	if _, err := startAgentInvocation(context.Background(), runner, "/tmp/runtime", io.Discard, invocation); err != nil {
		t.Fatal(err)
	}
	if strings.Join(runner.env, " ") != "KIMI_CODE_NO_AUTO_UPDATE=1" {
		t.Fatalf("runner env = %#v, want KIMI_CODE_NO_AUTO_UPDATE=1", runner.env)
	}
}

func TestStartAgentInvocationFailsLoudlyWhenEnvCannotBeCarried(t *testing.T) {
	invocation, err := ResolveAgentInvocation("kimi", "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	_, err = startAgentInvocation(context.Background(), envBlindRunner{}, "/tmp/runtime", io.Discard, invocation)
	if err == nil {
		t.Fatal("expected an error rather than a silently dropped environment")
	}
	if !strings.Contains(err.Error(), "KIMI_CODE_NO_AUTO_UPDATE=1") {
		t.Fatalf("error = %v, want it to name the dropped entries", err)
	}
}
