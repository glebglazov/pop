package tasks

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// argvBytes is what execve counts: every argument plus its NUL.
func argvBytes(name string, args []string) int {
	n := len(name) + 1
	for _, a := range args {
		n += len(a) + 1
	}
	return n
}

// TestSpilledPromptKeepsArgvSmallForEveryPreset: a multi-megabyte prompt — the
// size a Verifier prompt for a large set reaches — leaves argv a few hundred
// bytes for every preset and for a custom agent command, because the prompt
// itself is in a file. The spill sits at the run seam, so no preset opts in.
func TestSpilledPromptKeepsArgvSmallForEveryPreset(t *testing.T) {
	huge := strings.Repeat("criteria and task bodies and spec\n", 100_000) // ~3.3 MB
	if len(huge) < 3<<20 {
		t.Fatalf("fixture prompt is %d bytes, want > 3 MiB", len(huge))
	}
	for _, preset := range ValidAgentPresets() {
		invocation, err := ResolveAgentInvocation(preset, "", huge, "/rt")
		if err != nil {
			t.Fatalf("%s: ResolveAgentInvocation: %v", preset, err)
		}
		if err := invocation.spillPrompt(); err != nil {
			t.Fatalf("%s: spillPrompt: %v", preset, err)
		}
		if size := argvBytes(invocation.Name, invocation.Args); size > 4096 {
			t.Fatalf("%s: argv = %d bytes, want a few hundred regardless of prompt size", preset, size)
		}
		body, err := os.ReadFile(invocation.promptFile)
		if err != nil {
			t.Fatalf("%s: read spill file: %v", preset, err)
		}
		if string(body) != huge {
			t.Fatalf("%s: spill file holds %d bytes, want the whole %d-byte prompt", preset, len(body), len(huge))
		}
		if !strings.Contains(strings.Join(invocation.Args, " "), invocation.promptFile) {
			t.Fatalf("%s: argv does not name the prompt file: %v", preset, invocation.Args)
		}
		invocation.cleanupPrompt()
	}

	custom, err := ResolveAgentInvocation("", "fake-agent --verbose", huge, "/rt")
	if err != nil {
		t.Fatalf("custom: ResolveAgentInvocation: %v", err)
	}
	if err := custom.spillPrompt(); err != nil {
		t.Fatalf("custom: spillPrompt: %v", err)
	}
	defer custom.cleanupPrompt()
	if size := argvBytes(custom.Name, custom.Args); size > 4096 {
		t.Fatalf("custom: argv = %d bytes, want a few hundred", size)
	}
}

// TestSpilledPromptExecvesWhereInlineWouldFail: the defect this fixes was
// `fork/exec: argument list too long`, so the proof is a real execve carrying the
// spilled argv — an inline prompt of the same size fails, the spilled one runs.
func TestSpilledPromptExecvesWhereInlineWouldFail(t *testing.T) {
	huge := strings.Repeat("x", 3<<20)
	invocation, err := ResolveAgentInvocation("claude", "", huge, "/rt")
	if err != nil {
		t.Fatalf("ResolveAgentInvocation: %v", err)
	}
	inline := exec.Command("/usr/bin/true", invocation.Args...)
	if err := inline.Run(); err == nil {
		t.Skip("this platform accepts a 3 MiB argv; the E2BIG ceiling cannot be shown here")
	}
	if err := invocation.spillPrompt(); err != nil {
		t.Fatalf("spillPrompt: %v", err)
	}
	defer invocation.cleanupPrompt()
	if err := exec.Command("/usr/bin/true", invocation.Args...).Run(); err != nil {
		t.Fatalf("spilled argv still failed to exec: %v", err)
	}
}

// TestRunAgentAttemptRemovesSpillFile: the file lives for one attempt. It is gone
// after a clean run, after a timeout kill, and after an interrupt — and the agent
// could read it while the attempt was live.
func TestRunAgentAttemptRemovesSpillFile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		runner  CommandRunner
	}{
		{name: "clean run", timeout: 5 * time.Second, runner: promptReadingRunner{}},
		{name: "timeout", timeout: 20 * time.Millisecond, runner: hangingRunner{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invocation, err := ResolveAgentInvocation("claude", "", "the whole prompt", "/rt")
			if err != nil {
				t.Fatalf("ResolveAgentInvocation: %v", err)
			}
			d := DefaultDeps()
			d.Runner = tc.runner
			raw, outcome, err := runAgentAttempt(d, t.TempDir(), io.Discard, tc.timeout, invocation)
			if err != nil {
				t.Fatalf("runAgentAttempt: %v", err)
			}
			if r, ok := tc.runner.(promptReadingRunner); ok {
				_ = r
				if !strings.Contains(raw, "the whole prompt") {
					t.Fatalf("agent could not read the spilled prompt, got %q", raw)
				}
			} else if outcome == nil || !outcome.timedOut {
				t.Fatalf("outcome = %+v, want a timed-out attempt", outcome)
			}
			if invocation.promptFile != "" {
				t.Fatalf("prompt file %q still recorded after the attempt", invocation.promptFile)
			}
			for _, arg := range invocation.Args {
				if path, ok := spillPathFromInstruction(arg); ok {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("spill file %s survived the attempt (stat err = %v)", path, err)
					}
				}
			}
		})
	}
}

// spillPathFromInstruction recovers the file an argv instruction names.
func spillPathFromInstruction(arg string) (string, bool) {
	const prefix = "Read the file "
	if !strings.HasPrefix(arg, prefix) {
		return "", false
	}
	path, _, found := strings.Cut(strings.TrimPrefix(arg, prefix), " in full:")
	return path, found
}

// promptReadingRunner is an agent that does what the instruction says: it reads
// the spill file and echoes it, which is only possible while the file exists.
type promptReadingRunner struct{}

func (promptReadingRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (promptReadingRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	var body bytes.Buffer
	for _, arg := range args {
		if path, ok := spillPathFromInstruction(arg); ok {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			body.Write(data)
		}
	}
	_, _ = stdout.Write(body.Bytes())
	proc := &ManagedProcess{done: make(chan waitResult, 1)}
	proc.done <- waitResult{}
	return proc, nil
}

func (promptReadingRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

// hangingRunner never finishes on its own, so the attempt ends on its timeout.
type hangingRunner struct{}

func (hangingRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (hangingRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return RealCommandRunner{}.Start(ctx, dir, stdout, stderr, "sleep", "30")
}

func (hangingRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}
