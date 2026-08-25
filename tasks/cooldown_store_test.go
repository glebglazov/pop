package tasks

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func TestAgentCooldownPathUsesXDGData(t *testing.T) {
	t.Parallel()
	d := &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return "/xdg/data"
			}
			return ""
		},
	}}
	got := AgentCooldownPathWith(d)
	want := filepath.Join("/xdg/data", "pop", agentCooldownFileName)
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestAgentCooldownConcurrentUpdates(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)

	const writers = 10
	var wg sync.WaitGroup
	errCh := make(chan error, writers)
	for i := 0; i < writers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			preset := fmt.Sprintf("agent-%02d", i)
			until := time.Date(2026, 6, 20, 12, i, 0, 0, time.UTC)
			if err := updateAgentCooldown(d, preset, until); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	store, err := readAgentCooldowns(d)
	if err != nil {
		t.Fatalf("readAgentCooldowns: %v", err)
	}
	if len(store) != writers {
		t.Fatalf("store entries = %d, want %d: %#v", len(store), writers, store)
	}
	for i := 0; i < writers; i++ {
		preset := fmt.Sprintf("agent-%02d", i)
		want := time.Date(2026, 6, 20, 12, i, 0, 0, time.UTC)
		if got := store[preset].ExhaustedUntil; !got.Equal(want) {
			t.Fatalf("%s until = %s, want %s", preset, got, want)
		}
	}
}

func TestAgentQuotaCooldownUntilPolicyInTasks(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	fallback := 30 * time.Minute

	tests := []struct {
		name    string
		resetAt time.Time
		want    time.Time
	}{
		{name: "zero fallback", resetAt: time.Time{}, want: now.Add(fallback)},
		{name: "past fallback", resetAt: now.Add(-time.Second), want: now.Add(fallback)},
		{name: "too far fallback", resetAt: now.Add(8*24*time.Hour + time.Second), want: now.Add(fallback)},
		{name: "sane reset recorded as derived", resetAt: now.Add(time.Hour), want: now.Add(time.Hour)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentQuotaCooldownUntil(tc.resetAt, now, fallback); !got.Equal(tc.want) {
				t.Fatalf("cooldown = %s, want %s", got, tc.want)
			}
		})
	}
}

// A quota pause with a provider-stated reset parks once. The recovery wait
// sleeps on the verdict's instant and the cooldown row records that same
// instant, so the drain that wakes finds the preset runnable. Before ADR-0235
// the row sat one Quota assurance offset further out than the wait: the wake
// re-read it, found the preset still cooling, synthesised a fresh quota pause
// and parked again — one wasted park-resume-park cycle and a junk drain record
// per quota pause.
func TestStatedQuotaResetParksOnceNotTwice(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)

	result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, claudeSessionLimitCapture(t))
	if result.ProceedVerdict == nil {
		t.Fatal("no verdict from the captured session-limit run")
	}
	v := resolveProceedResetAt(stampDetectedVerdict(*result.ProceedVerdict, "claude", "opus"), claudeCaptureRefusedAt)

	until := agentQuotaCooldownUntil(v.ResetAt, claudeCaptureRefusedAt, defaultAgentQuotaRetryAfter)
	if !until.Equal(v.ResetAt.UTC()) {
		t.Fatalf("cooldown row at %s, want the instant the wait sleeps on, %s", until, v.ResetAt.UTC())
	}
	if err := updateAgentCooldown(d, v.Preset, until); err != nil {
		t.Fatal(err)
	}

	// WaitForRecovery resumes at the first instant that is not before ResetAt.
	wake := v.ResetAt.UTC()
	active, err := ActiveAgentCooldownsWith(d, wake)
	if err != nil {
		t.Fatal(err)
	}
	if stillCooling, cooling := active[v.Preset]; cooling {
		t.Fatalf("preset still cooling until %s when the park woke at %s: the pause parks twice", stillCooling, wake)
	}
}
