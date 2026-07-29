package tasks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProbeAgentAuthentication(t *testing.T) {
	t.Run("no probe reports cannot determine", func(t *testing.T) {
		for _, preset := range []string{"pi", "opencode"} {
			got := ProbeAgentAuthentication(&Deps{}, ".", preset)
			if got.Status != AgentAuthCannotDetermine {
				t.Fatalf("%s status = %v, want cannot determine", preset, got.Status)
			}
			if !strings.Contains(got.Detail, "cannot determine") {
				t.Fatalf("%s detail = %q, want cannot determine wording", preset, got.Detail)
			}
		}
	})

	t.Run("authenticated", func(t *testing.T) {
		runner := &probeCountingRunner{output: `{"loggedIn":true}`}
		got := ProbeAgentAuthentication(&Deps{Runner: runner}, ".", "claude")
		if got.Status != AgentAuthAuthenticated || got.Detail != "authenticated" {
			t.Fatalf("got = %+v, want authenticated", got)
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		raw := `{"isAuthenticated":false}`
		runner := &probeCountingRunner{output: raw}
		got := ProbeAgentAuthentication(&Deps{Runner: runner}, ".", "cursor")
		if got.Status != AgentAuthUnauthenticated || got.Detail != raw {
			t.Fatalf("got = %+v, want unauthenticated with provider line", got)
		}
	})

	t.Run("unknown not unauthenticated", func(t *testing.T) {
		runner := &probeCountingRunner{output: "not json"}
		got := ProbeAgentAuthentication(&Deps{Runner: runner}, ".", "cursor")
		if got.Status != AgentAuthUnknown {
			t.Fatalf("got = %+v, want unknown", got)
		}
		if got.Status == AgentAuthUnauthenticated {
			t.Fatal("unparseable probe must not report unauthenticated")
		}
	})
}

func TestAvailabilityProbeCapabilityByPreset(t *testing.T) {
	wantProbe := map[string]bool{
		"claude":   true,
		"cursor":   true,
		"codex":    true,
		"pi":       false,
		"opencode": false,
	}
	for preset, want := range wantProbe {
		t.Run(preset, func(t *testing.T) {
			adapter, err := ResolveAgentAdapter(preset)
			if err != nil {
				t.Fatal(err)
			}
			got := adapter.AvailabilityProbeCapability().Available()
			if got != want {
				t.Fatalf("probe available = %v, want %v", got, want)
			}
		})
	}
}

func TestInterpretCursorAvailabilityProbe(t *testing.T) {
	t.Run("authenticated", func(t *testing.T) {
		if u := interpretCursorAvailabilityProbe(0, `{"isAuthenticated":true}`); u != nil {
			t.Fatalf("unexpected unavailability: %#v", u)
		}
	})
	t.Run("explicit negative", func(t *testing.T) {
		raw := `{"isAuthenticated":false}`
		u := interpretCursorAvailabilityProbe(0, raw)
		if u == nil || u.Kind != UnavailabilityAuthFailure {
			t.Fatalf("unavailability = %#v", u)
		}
		if u.Reason != raw {
			t.Fatalf("reason = %q, want %q", u.Reason, raw)
		}
	})
	t.Run("non-zero exit is unknown", func(t *testing.T) {
		if u := interpretCursorAvailabilityProbe(1, `{"isAuthenticated":false}`); u != nil {
			t.Fatalf("unexpected unavailability: %#v", u)
		}
	})
	t.Run("unparseable is unknown", func(t *testing.T) {
		if u := interpretCursorAvailabilityProbe(0, "not json"); u != nil {
			t.Fatalf("unexpected unavailability: %#v", u)
		}
	})
}

func TestInterpretClaudeAvailabilityProbe(t *testing.T) {
	t.Run("authenticated", func(t *testing.T) {
		if u := interpretClaudeAvailabilityProbe(0, `{"loggedIn":true}`); u != nil {
			t.Fatalf("unexpected unavailability: %#v", u)
		}
	})
	t.Run("explicit negative", func(t *testing.T) {
		raw := `{"loggedIn":false}`
		u := interpretClaudeAvailabilityProbe(0, raw)
		if u == nil || u.Kind != UnavailabilityAuthFailure {
			t.Fatalf("unavailability = %#v", u)
		}
	})
}

func TestInterpretCodexAvailabilityProbeNeverNegative(t *testing.T) {
	if u := interpretCodexAvailabilityProbe(1, "not logged in"); u != nil {
		t.Fatalf("non-zero exit must be unknown, got %#v", u)
	}
	if u := interpretCodexAvailabilityProbe(0, "Logged in"); u != nil {
		t.Fatalf("exit zero must proceed, got %#v", u)
	}
}

func TestAgentAvailabilityProbeMemoRunsOncePerPreset(t *testing.T) {
	runner := &probeCountingRunner{}
	d := &Deps{Runner: runner}
	memo := newAgentAvailabilityProbeMemo()

	first := memo.checkUnavailability(d, ".", "cursor")
	if first != nil {
		t.Fatalf("first probe = %#v, want nil (authenticated)", first)
	}
	second := memo.checkUnavailability(d, ".", "cursor")
	if second != nil {
		t.Fatalf("second probe = %#v, want memoised proceed", second)
	}
	if runner.calls != 1 {
		t.Fatalf("probe calls = %d, want 1", runner.calls)
	}
}

func TestAgentAvailabilityProbeMemoOneWaySkip(t *testing.T) {
	runner := &probeCountingRunner{output: `{"isAuthenticated":false}`}
	d := &Deps{Runner: runner}
	memo := newAgentAvailabilityProbeMemo()

	first := memo.checkUnavailability(d, ".", "cursor")
	if first == nil || first.Kind != UnavailabilityAuthFailure {
		t.Fatalf("first probe = %#v, want auth failure", first)
	}
	runner.output = `{"isAuthenticated":true}`
	second := memo.checkUnavailability(d, ".", "cursor")
	if second == nil || second.Kind != UnavailabilityAuthFailure {
		t.Fatalf("second probe = %#v, want memoised skip", second)
	}
	if runner.calls != 1 {
		t.Fatalf("probe calls = %d, want 1", runner.calls)
	}
}

type probeCountingRunner struct {
	calls  int
	output string
}

func (r *probeCountingRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	r.calls++
	if r.output == "" {
		r.output = `{"isAuthenticated":true}`
	}
	_, _ = io.WriteString(stdout, r.output)
	return 0, nil
}

func (r *probeCountingRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return nil, io.EOF
}

func TestAgentAvailabilityProbeTimeoutProceeds(t *testing.T) {
	prev := agentAvailabilityProbeTimeout
	agentAvailabilityProbeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { agentAvailabilityProbeTimeout = prev })

	runner := &slowProbeRunner{}
	d := &Deps{Runner: runner}
	memo := newAgentAvailabilityProbeMemo()

	if u := memo.checkUnavailability(d, ".", "cursor"); u != nil {
		t.Fatalf("timeout probe = %#v, want proceed", u)
	}
	if !runner.started {
		t.Fatal("probe was not started")
	}
}

type slowProbeRunner struct {
	started bool
}

func (r *slowProbeRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	r.started = true
	<-ctx.Done()
	return 1, ctx.Err()
}

func (r *slowProbeRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return nil, io.EOF
}

func TestRunTaskSetAgentFallbackSkipsCursorOnProbeAuthFailure(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	attemptCount := filepath.Join(env.root, ".agent-bin", "cursor-agent.attempts")
	installAgentShim(t, env.root, "cursor-agent", fmt.Sprintf(`#!/bin/sh
if [ "$1" = status ]; then
  printf '{"isAuthenticated":false}\n'
  exit 0
fi
n=0
test -f %[1]q && n=$(cat %[1]q)
n=$((n + 1))
printf '%%s\n' "$n" > %[1]q
printf 'should not run\n'
exit 1
`, attemptCount))
	installAgentShim(t, env.root, "claude", `#!/bin/sh
TASK=$(printf '%s' "$*" | sed -n 's|^.*You are implementing the task at: ||p' | head -1 | awk '{print $1}')
if [ -n "$TASK" ] && [ -f "$TASK" ]; then sed -i '' 's/- \[ \]/- [x]/g' "$TASK" 2>/dev/null || sed -i 's/- \[ \]/- [x]/g' "$TASK"; fi
printf 'SUMMARY_START\nclaude done\nSUMMARY_END\nTASK_COMPLETE\n'
`)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"cursor", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 3

	start := time.Now()
	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("fallback took %s, want no retry delay between agents", elapsed)
	}
	out := buf.String()
	if strings.Contains(out, "Attempt 1/3 · cursor") {
		t.Fatalf("cursor should be skipped by probe before spawn:\n%s", out)
	}
	if !strings.Contains(out, "Attempt 1/3 · claude") {
		t.Fatalf("fallback attempt not rendered:\n%s", out)
	}
	if !strings.Contains(out, "Agent cursor unauthenticated; trying next") {
		t.Fatalf("missing cursor probe fallback line:\n%s", out)
	}
	if _, err := os.Stat(attemptCount); !os.IsNotExist(err) {
		t.Fatalf("cursor-agent headless was spawned despite probe skip: stat err = %v", err)
	}

	runs, err := listSetRuns(env.deps(), env.execFixture().demoDir())
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	for _, run := range runs {
		if run.meta.Agent == "cursor" {
			t.Fatalf("cursor captured run should not exist: %#v", run.meta)
		}
	}

	assertTaskDone(t, env.execFixture(), "01-a")
}

func TestRunTaskSetAgentFallbackProceedsOnUnknownProbe(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	authLine := "Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable."
	installAgentShim(t, env.root, "cursor-agent", fmt.Sprintf(`#!/bin/sh
if [ "$1" = status ]; then
  printf 'unparseable status output\n'
  exit 1
fi
printf '%%s\n' %q
exit 1
`, authLine))
	installAgentShim(t, env.root, "claude", `#!/bin/sh
TASK=$(printf '%s' "$*" | sed -n 's|^.*You are implementing the task at: ||p' | head -1 | awk '{print $1}')
if [ -n "$TASK" ] && [ -f "$TASK" ]; then sed -i '' 's/- \[ \]/- [x]/g' "$TASK" 2>/dev/null || sed -i 's/- \[ \]/- [x]/g' "$TASK"; fi
printf 'SUMMARY_START\nclaude done\nSUMMARY_END\nTASK_COMPLETE\n'
`)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"cursor", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 3

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 1 {
		t.Fatalf("result = %#v", result)
	}
	out := buf.String()
	if !strings.Contains(out, "Attempt 1/3 · cursor") {
		t.Fatalf("unknown probe must still invoke cursor:\n%s", out)
	}
	if !strings.Contains(out, "Agent cursor unauthenticated; trying next") {
		t.Fatalf("passive auth detection should still run:\n%s", out)
	}
}
