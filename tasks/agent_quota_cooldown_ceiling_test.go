package tasks

import (
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

// undatedRefusal is a refusal that named a window but no reset instant — the
// shape that used to earn a blind hour measured from pop's disappointment.
func undatedRefusal(preset string, class AgentQuotaWindowClass) AgentQuotaCooldownRequest {
	return AgentQuotaCooldownRequest{Preset: preset, Class: class}
}

func recordedCooldown(t *testing.T, d *Deps, preset string) (row struct {
	Until, Stated time.Time
	Class         string
}) {
	t.Helper()
	rows, err := readAgentCooldowns(d)
	if err != nil {
		t.Fatalf("readAgentCooldowns: %v", err)
	}
	got, ok := rows[preset]
	if !ok {
		t.Fatalf("no cooldown row for %s", preset)
	}
	row.Until, row.Stated, row.Class = got.ExhaustedUntil, got.StatedUntil, got.Class
	return row
}

// A refusal pop had to guess at records no stated instant, and its expiry is the
// ceiling of the window class the refusal named — the latest that window can
// still be running (ADR-0235).
func TestGuessedCooldownDatesFromItsClassCeiling(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	now := time.Now().UTC()

	untilRow, err := recordAgentQuotaCooldown(d, undatedRefusal("claude", QuotaWindowFiveHour), now, config.DefaultWorkDaemonQuotaRetryAfter)
	if err != nil {
		t.Fatal(err)
	}
	until := untilRow.Until
	if want := now.Add(5 * time.Hour); !until.Equal(want) {
		t.Fatalf("cooldown until %s, want the five-hour ceiling %s", until, want)
	}
	row := recordedCooldown(t, d, "claude")
	if !row.Stated.IsZero() {
		t.Fatalf("stated instant %s recorded for a refusal that stated nothing", row.Stated)
	}
	if row.Class != string(QuotaWindowFiveHour) {
		t.Fatalf("class = %q, want %q: the ceiling has to say what it was dated from", row.Class, QuotaWindowFiveHour)
	}
}

// A guess never displaces a stated instant that has yet to pass. Reading beats
// guessing, and this alone would have prevented the incident's third write.
func TestGuessNeverOverwritesALiveStatedInstant(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	now := time.Now().UTC().Truncate(time.Second)
	stated := now.Add(2 * time.Hour)

	if _, err := recordAgentQuotaCooldown(d, AgentQuotaCooldownRequest{Preset: "claude", Stated: stated, Class: QuotaWindowFiveHour}, now, config.DefaultWorkDaemonQuotaRetryAfter); err != nil {
		t.Fatal(err)
	}
	untilRow, err := recordAgentQuotaCooldown(d, undatedRefusal("claude", QuotaWindowWeekly), now.Add(time.Minute), config.DefaultWorkDaemonQuotaRetryAfter)
	if err != nil {
		t.Fatal(err)
	}
	until := untilRow.Until
	if !until.Equal(stated) {
		t.Fatalf("cooldown until %s, want the provider's own %s left standing", until, stated)
	}
	row := recordedCooldown(t, d, "claude")
	if !row.Stated.Equal(stated) {
		t.Fatalf("stated instant = %s, want %s: a guess erased what the provider said", row.Stated, stated)
	}
}

// Refused at the instant a provider named, pop stops believing the statement and
// falls into the guessed path rather than inventing a third policy.
func TestRefusalAtAStatedInstantBecomesAGuess(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	now := time.Now().UTC().Truncate(time.Second)
	stated := now.Add(time.Hour)

	if _, err := recordAgentQuotaCooldown(d, AgentQuotaCooldownRequest{Preset: "claude", Stated: stated, Class: QuotaWindowFiveHour}, now, config.DefaultWorkDaemonQuotaRetryAfter); err != nil {
		t.Fatal(err)
	}
	// The wake happens at the stated instant, and the agent refuses again.
	untilRow, err := recordAgentQuotaCooldown(d, undatedRefusal("claude", QuotaWindowFiveHour), stated, config.DefaultWorkDaemonQuotaRetryAfter)
	if err != nil {
		t.Fatal(err)
	}
	until := untilRow.Until
	if want := stated.Add(5 * time.Hour); !until.Equal(want) {
		t.Fatalf("cooldown until %s, want the class ceiling %s from the disproved statement", until, want)
	}
	if row := recordedCooldown(t, d, "claude"); !row.Stated.IsZero() {
		t.Fatalf("stated instant %s survived a refusal at that very instant", row.Stated)
	}
}

// A refusal naming no window falls back to the configured ceiling, which is the
// shortest window a subscription has rather than the hour that was shorter than
// all of them.
func TestUnclassedRefusalUsesTheConfiguredCeiling(t *testing.T) {
	t.Parallel()
	if config.DefaultWorkDaemonQuotaRetryAfter == time.Hour {
		t.Fatal("the unclassed ceiling still defaults to one blind hour")
	}
	d := newTestDeps(t)
	now := time.Now().UTC()

	untilRow, err := recordAgentQuotaCooldown(d, undatedRefusal("kimi", QuotaWindowUnknown), now, config.DefaultWorkDaemonQuotaRetryAfter)
	if err != nil {
		t.Fatal(err)
	}
	until := untilRow.Until
	if want := now.Add(config.DefaultWorkDaemonQuotaRetryAfter); !until.Equal(want) {
		t.Fatalf("cooldown until %s, want the configured unclassed ceiling %s", until, want)
	}
}

// The ADR-0233 incident, replayed: a refusal at 19:58 against a five-hour window
// that reopened at 21:00, a wake that came too early, and a second refusal on the
// edge of it. Each refusal used to write a fresh hour from its own later moment —
// 19:58 → 20:58 → 21:58 — so an undershoot of two minutes bought an overshoot of
// fifty-eight and nothing ever pulled the deadline back. The ceiling is dated at
// the first refusal, and no later one may move it (ADR-0235).
func TestGuessedCooldownNeverOutgrowsItsFirstCeiling(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	refusedAt := time.Date(2026, 8, 24, 19, 58, 0, 0, time.UTC)

	firstRow, err := recordAgentQuotaCooldown(d, undatedRefusal("claude", QuotaWindowFiveHour), refusedAt, config.DefaultWorkDaemonQuotaRetryAfter)
	if err != nil {
		t.Fatal(err)
	}
	first := firstRow.Until
	ceiling := refusedAt.Add(5 * time.Hour)
	if !first.Equal(ceiling) {
		t.Fatalf("first cooldown until %s, want %s", first, ceiling)
	}

	for _, later := range []time.Time{
		refusedAt.Add(time.Hour),       // the early wake that earned the incident's second hour
		refusedAt.Add(2 * time.Hour),   // and its third
		ceiling.Add(-time.Millisecond), // a refusal on the very edge of the ceiling
	} {
		untilRow, err := recordAgentQuotaCooldown(d, undatedRefusal("claude", QuotaWindowFiveHour), later, config.DefaultWorkDaemonQuotaRetryAfter)
		if err != nil {
			t.Fatal(err)
		}
		until := untilRow.Until
		if !until.Equal(ceiling) {
			t.Fatalf("refusal at %s moved the cooldown to %s, want the first ceiling %s", later, until, ceiling)
		}
		if row := recordedCooldown(t, d, "claude"); !row.Until.Equal(ceiling) {
			t.Fatalf("row at %s after a refusal at %s, want the first ceiling %s", row.Until, later, ceiling)
		}
	}

	// Past the ceiling the row is spent, and the next refusal is a first refusal
	// again — dated from itself, not stacked on what came before.
	after := ceiling.Add(time.Minute)
	untilRow, err := recordAgentQuotaCooldown(d, undatedRefusal("claude", QuotaWindowFiveHour), after, config.DefaultWorkDaemonQuotaRetryAfter)
	if err != nil {
		t.Fatal(err)
	}
	until := untilRow.Until
	if want := after.Add(5 * time.Hour); !until.Equal(want) {
		t.Fatalf("cooldown after the ceiling elapsed = %s, want %s", until, want)
	}
}
