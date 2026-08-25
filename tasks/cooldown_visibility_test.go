package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// A live cooldown reports where its instant came from, so no surface has to
// guess whether the number is a provider's reset or pop's own backstop
// (ADR-0235).
func TestLiveCooldownsCarryTheirOrigin(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	now := time.Date(2026, 8, 24, 19, 58, 0, 0, time.UTC)
	stated := now.Add(90 * time.Minute)

	if _, err := recordAgentQuotaCooldown(d, undatedRefusal("claude", QuotaWindowFiveHour), now, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := recordAgentQuotaCooldown(d, AgentQuotaCooldownRequest{Preset: "codex", Stated: stated}, now, 0); err != nil {
		t.Fatal(err)
	}

	rows, err := LiveAgentQuotaCooldownsWith(d, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("live cooldowns = %d, want 2: %+v", len(rows), rows)
	}
	// Preset order, so a listing is stable between reads of one machine.
	if rows[0].Preset != "claude" || rows[1].Preset != "codex" {
		t.Fatalf("live cooldowns out of preset order: %+v", rows)
	}
	guess := rows[0]
	if !guess.Guessed || guess.Origin() != "guessed" {
		t.Fatalf("a refusal that stated nothing reads as %q", guess.Origin())
	}
	if guess.Class != QuotaWindowFiveHour || guess.Class.Label() != "session limit" {
		t.Fatalf("guess names window %q (%q), want the session limit it was dated from", guess.Class, guess.Class.Label())
	}
	if !guess.Until.Equal(now.Add(5*time.Hour)) || guess.NextProbeAt.IsZero() {
		t.Fatalf("guess ceiling %s / next probe %s, want the class ceiling and a scheduled ask", guess.Until, guess.NextProbeAt)
	}
	read := rows[1]
	if read.Guessed || read.Origin() != "stated" {
		t.Fatalf("a provider's own instant reads as %q", read.Origin())
	}
	if !read.Until.Equal(stated) {
		t.Fatalf("stated cooldown until %s, want %s", read.Until, stated)
	}
}

// Clearing is the verb that replaces the incident's raw SQL delete: the named
// preset's row is gone, every park waiting on it is eligible now, and the presets
// beside it are untouched. A preset with nothing recorded says so rather than
// reporting a clear it did not make.
func TestClearAgentQuotaCooldownDropsOnlyTheNamedPreset(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	now := time.Date(2026, 8, 24, 19, 58, 0, 0, time.UTC)
	for _, preset := range []string{"claude", "codex"} {
		if _, err := recordAgentQuotaCooldown(d, undatedRefusal(preset, QuotaWindowWeekly), now, 0); err != nil {
			t.Fatal(err)
		}
	}
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatal(err)
	}
	ceiling := now.Add(7 * 24 * time.Hour)
	if err := s.PutRecoveryWaiter(store.RecoveryWaiter{
		SetID: "set-a", Preset: "claude", ResetAt: ceiling, RuntimePath: t.TempDir(), RegisteredAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	cleared, err := ClearAgentQuotaCooldownWith(d, "claude", now)
	if err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("clearing a live cooldown reported nothing to clear")
	}
	active, err := ActiveAgentCooldownsWith(d, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, still := active["claude"]; still {
		t.Fatal("cleared preset is still cooling")
	}
	if _, ok := active["codex"]; !ok {
		t.Fatal("clearing one preset dropped another's cooldown")
	}
	waiters, err := s.AllRecoveryWaiters()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range waiters {
		if w.SetID == "set-a" && w.ResetAt.After(now) {
			t.Fatalf("park on the cleared preset still waits until %s", w.ResetAt)
		}
	}

	if cleared, err := ClearAgentQuotaCooldownWith(d, "kimi", now); err != nil || cleared {
		t.Fatalf("clearing an uncooled preset = (%v, %v), want (false, nil)", cleared, err)
	}
}

// The attended launch-time skip is the one place a human meets a cooldown before
// any drain runs (ADR-0195 decision 6). A guess there has to name itself: read as
// a provider's reset, it looks like a wait worth respecting rather than a ceiling
// worth clearing.
func TestAttendedSkipNamesAGuessAsAGuess(t *testing.T) {
	until := time.Date(2026, 8, 25, 0, 58, 0, 0, time.UTC)

	guessed := formatAttendedSkipCooling(AgentQuotaCooldownView{
		Preset:  "claude",
		Until:   until,
		Guessed: true,
		Class:   QuotaWindowFiveHour,
	})
	for _, want := range []string{"skipped claude", "guessed cooldown", "session limit", "backstop"} {
		if !strings.Contains(guessed, want) {
			t.Fatalf("guessed skip %q missing %q", guessed, want)
		}
	}

	stated := formatAttendedSkipCooling(AgentQuotaCooldownView{Preset: "claude", Until: until})
	if !strings.Contains(stated, "cooling until") {
		t.Fatalf("stated skip %q does not read as a reset", stated)
	}
	if strings.Contains(stated, "guessed") || strings.Contains(stated, "backstop") {
		t.Fatalf("stated skip %q reads as a guess", stated)
	}
}
