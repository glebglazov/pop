package tasks

import (
	"fmt"
	"path/filepath"
	"strings"
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
			req := AgentQuotaCooldownRequest{Preset: preset, Stated: concurrentCooldownUntil(i)}
			if _, err := recordAgentQuotaCooldown(d, req, time.Now(), 0); err != nil {
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
		want := concurrentCooldownUntil(i)
		if got := store[preset].ExhaustedUntil; !got.Equal(want) {
			t.Fatalf("%s until = %s, want %s", preset, got, want)
		}
	}
}

// concurrentCooldownUntil spaces the writers' stated resets a minute apart, all
// ahead of now: a reset already behind now is no statement and would be recorded
// as a guess instead.
func concurrentCooldownUntil(i int) time.Time {
	return time.Now().UTC().Add(time.Hour).Truncate(time.Second).Add(time.Duration(i) * time.Minute)
}

// updateAgentCooldown writes one preset's row unconditionally, the way the fold
// of the retired agent-cooldowns.json file does. It seeds a cooldown for tests
// that are about how a cooling preset is *read*; a test about how a refusal is
// recorded must go through recordAgentQuotaCooldown, whose whole subject is
// which writes are refused.
func updateAgentCooldown(d *Deps, preset string, until time.Time) error {
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	return s.PutAgentCooldown(strings.TrimSpace(preset), until.UTC())
}

func TestAgentQuotaCooldownRowPolicy(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	unclassed := 30 * time.Minute

	tests := []struct {
		name       string
		req        AgentQuotaCooldownRequest
		want       time.Time
		wantStated bool
	}{
		{
			name:       "stated reset recorded as derived, in both columns",
			req:        AgentQuotaCooldownRequest{Stated: now.Add(time.Hour), Class: QuotaWindowFiveHour},
			want:       now.Add(time.Hour),
			wantStated: true,
		},
		{
			name: "no reset is dated from the class ceiling",
			req:  AgentQuotaCooldownRequest{Class: QuotaWindowFiveHour},
			want: now.Add(5 * time.Hour),
		},
		{
			name: "a weekly refusal is dated from the week",
			req:  AgentQuotaCooldownRequest{Class: QuotaWindowWeekly},
			want: now.Add(7 * 24 * time.Hour),
		},
		{
			name: "a reset already behind now is no statement",
			req:  AgentQuotaCooldownRequest{Stated: now.Add(-time.Second), Class: QuotaWindowOpus},
			want: now.Add(7 * 24 * time.Hour),
		},
		{
			name: "a reset past the horizon is no statement",
			req:  AgentQuotaCooldownRequest{Stated: now.Add(maxAgentQuotaResetHorizon + time.Second), Class: QuotaWindowFiveHour},
			want: now.Add(5 * time.Hour),
		},
		{
			name: "an unclassed refusal uses the configured ceiling",
			req:  AgentQuotaCooldownRequest{},
			want: now.Add(unclassed),
		},
		{
			name: "a caller's own ceiling answers where no class does",
			req:  AgentQuotaCooldownRequest{Ceiling: spendCapCooldown},
			want: now.Add(spendCapCooldown),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.Preset = "claude"
			row := agentQuotaCooldownRow(tc.req, now, unclassed)
			if !row.ExhaustedUntil.Equal(tc.want) {
				t.Fatalf("cooldown = %s, want %s", row.ExhaustedUntil, tc.want)
			}
			if got := !row.StatedUntil.IsZero(); got != tc.wantStated {
				t.Fatalf("stated = %v (%s), want %v: the absent statement is what marks a guess", got, row.StatedUntil, tc.wantStated)
			}
			if tc.wantStated && !row.StatedUntil.Equal(row.ExhaustedUntil) {
				t.Fatalf("stated %s and enforced %s disagree for a read reset", row.StatedUntil, row.ExhaustedUntil)
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

	until, err := recordAgentQuotaCooldown(d, quotaCooldownRequest(v), claudeCaptureRefusedAt, defaultUnclassedQuotaCeiling)
	if err != nil {
		t.Fatal(err)
	}
	if !until.Equal(v.ResetAt.UTC()) {
		t.Fatalf("cooldown row at %s, want the instant the wait sleeps on, %s", until, v.ResetAt.UTC())
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
