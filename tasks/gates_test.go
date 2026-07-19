package tasks

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// newHITLGateRun builds an implementRun wired to the sole-Human-blocked fixture
// (a real store-backed checkout holding a live Drain) with just the fields the
// HITL gate choreography reads, so r.hitlGate can be driven directly. confirmIn /
// yes decide whether the menu will actually prompt.
func newHITLGateRun(t *testing.T, confirmIn io.Reader, yes bool) (*implementRun, *Deps, string, *Manifest, *Task) {
	t.Helper()
	env, agent := setupSoleHumanBlockedFixture(t)
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}
	statePath := DefaultStatePath()
	refresh, err := RefreshWith(d, env.tasksDir, statePath)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	m := refresh.Manifests["solo"]
	hitl := BlockingHITLTask(m)
	if hitl == nil {
		t.Fatal("fixture must present a blocking HITL task")
	}

	handle, err := BeginDrain(d, runtimePath, "solo", io.Discard)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}

	run := &implementRun{
		d:    d,
		plan: &runPlan{},
		opts: RunTaskSetOptions{
			ResolveInput: ResolveInput{CWD: env.root},
			AgentCmd:     agent,
			ConfirmIn:    confirmIn,
			Yes:          yes,
		},
		resolved:    &ResolvedPaths{DefinitionPath: env.tasksDir, ProjectPath: env.root},
		runtimePath: runtimePath,
		statePath:   statePath,
		taskSetID:   "solo",
		confirmOut:  io.Discard,
		out:         &bytes.Buffer{},
		timeout:     time.Minute,
		drain:       handle,
	}
	t.Cleanup(func() {
		if run.drain != nil {
			finalizeDrain(run.drain, false, false, false, "", false, time.Time{}, nil)
		}
	})
	return run, d, runtimePath, m, hitl
}

// TestHITLGateShowsBlockedWaiterCount pins the ADR-0100 blocked-waiter line: a
// hold-registering gate menu prints how many quota-recovery waiters are queued
// behind the same checkout, and prints nothing when none are registered.
func TestHITLGateShowsBlockedWaiterCount(t *testing.T) {
	const blockedLine = "blocked on this checkout"

	t.Run("waiters present", func(t *testing.T) {
		run, d, runtimePath, m, hitl := newHITLGateRun(t, &checkingPromptReader{
			t: t, check: func(*testing.T) {}, response: "0\n",
		}, false)
		if _, err := RegisterRecoveryWaiter(d, RecoveryWaiter{
			SetID:       "waiter-set",
			Preset:      "sonnet",
			ResetAt:     time.Now().Add(time.Hour),
			RuntimePath: runtimePath,
		}); err != nil {
			t.Fatalf("RegisterRecoveryWaiter: %v", err)
		}

		if _, err := run.hitlGate(m, hitl); err != nil {
			t.Fatalf("hitlGate: %v", err)
		}
		got := run.out.(*bytes.Buffer).String()
		if !strings.Contains(got, "1 quota waiter "+blockedLine) {
			t.Fatalf("gate menu missing blocked-waiter count line; output:\n%s", got)
		}
	})

	t.Run("no waiters", func(t *testing.T) {
		run, _, _, m, hitl := newHITLGateRun(t, &checkingPromptReader{
			t: t, check: func(*testing.T) {}, response: "0\n",
		}, false)

		if _, err := run.hitlGate(m, hitl); err != nil {
			t.Fatalf("hitlGate: %v", err)
		}
		got := run.out.(*bytes.Buffer).String()
		if strings.Contains(got, blockedLine) {
			t.Fatalf("gate menu printed a blocked-waiter line with zero waiters; output:\n%s", got)
		}
	})
}

// TestImplementRunHITLGateHoldPairsAroundPromptingMenu pins the gate-hold
// register/release pairing the terminal-status HITL choreography (ADR-0067/0100)
// depends on: a prompting HITL gate parks the Drain and holds a checkout gate
// hold for the whole menu, and releases it when the menu ends. The hold is
// observed *during* the menu via a prompt reader whose Read callback fires while
// the handler waits on a selection.
func TestImplementRunHITLGateHoldPairsAroundPromptingMenu(t *testing.T) {
	var (
		run         *implementRun
		d           *Deps
		runtimePath string
		holdSeen    bool
	)
	check := func(t *testing.T) {
		t.Helper()
		hold, err := GetCheckoutGateHold(d, runtimePath)
		if err != nil {
			t.Fatalf("GetCheckoutGateHold during menu: %v", err)
		}
		if hold == nil || hold.SetID != "solo" {
			t.Fatalf("gate hold missing/mismatched during menu: %#v", hold)
		}
		// Parking dropped the live Drain so the menu runs lock-free.
		if run.drain != nil {
			t.Fatal("a prompting HITL gate must park the Drain before the menu")
		}
		holdSeen = true
	}
	reader := &checkingPromptReader{t: t, check: check, response: "0\n"}

	run, d, runtimePath, m, hitl := newHITLGateRun(t, reader, false)

	handled, err := run.hitlGate(m, hitl)
	if err != nil {
		t.Fatalf("hitlGate: %v", err)
	}
	if handled {
		t.Fatal("Exit (menu 0) must return handled=false")
	}
	if !holdSeen {
		t.Fatal("the menu never prompted; the hold-present assertion did not run")
	}
	if run.drain != nil {
		t.Fatal("the Drain must remain parked after a prompting gate")
	}
	hold, err := GetCheckoutGateHold(d, runtimePath)
	if err != nil {
		t.Fatalf("GetCheckoutGateHold after menu: %v", err)
	}
	if hold != nil {
		t.Fatalf("gate hold leaked after the menu ended: %#v", hold)
	}
}

// TestImplementRunHITLGateSkipsParkWhenNotPrompting pins the other half of the
// pairing: under --yes the menu will not prompt, so hitlGate must neither park
// the Drain nor register a gate hold — the held Drain stays live and the normal
// finalize records the terminal (ADR-0067).
func TestImplementRunHITLGateSkipsParkWhenNotPrompting(t *testing.T) {
	run, d, runtimePath, m, hitl := newHITLGateRun(t, nil, true)
	held := run.drain

	handled, err := run.hitlGate(m, hitl)
	if err != nil {
		t.Fatalf("hitlGate: %v", err)
	}
	if handled {
		t.Fatal("a non-prompting gate must return handled=false")
	}
	if run.drain != held {
		t.Fatal("a non-prompting gate must not park the Drain")
	}
	hold, err := GetCheckoutGateHold(d, runtimePath)
	if err != nil {
		t.Fatalf("GetCheckoutGateHold: %v", err)
	}
	if hold != nil {
		t.Fatalf("a non-prompting gate must register no hold: %#v", hold)
	}
}
