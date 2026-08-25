package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// installClaudeProbeAgent puts a claude on PATH that refuses every invocation
// while its window is shut and answers cleanly once openPath exists. The counter
// is how these tests see how many times pop asked — the probe's whole cost.
//
// ADR-0145: a PATH stub, so callers stay serial deliberately.
func installClaudeProbeAgent(t *testing.T, root string) (counterPath, openPath string) {
	t.Helper()
	dir := filepath.Join(root, ".probe-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	counterPath = filepath.Join(dir, "claude.count")
	openPath = filepath.Join(dir, "window-open")
	script := "#!/bin/sh\n" +
		"COUNT=0\n" +
		"if [ -f " + counterPath + " ]; then COUNT=$(cat " + counterPath + "); fi\n" +
		"echo $((COUNT + 1)) > " + counterPath + "\n" +
		"if [ -f " + openPath + " ]; then\n" +
		"  printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"READY\"}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"error_during_execution\",\"result\":\"You'\"'\"'ve hit your weekly limit · resets Mon 12:00am\"}'\n"
	writeFile(t, filepath.Join(dir, "claude"), script)
	if err := os.Chmod(filepath.Join(dir, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return counterPath, openPath
}

func probesAsked(t *testing.T, counterPath string) int {
	t.Helper()
	data, err := os.ReadFile(counterPath)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("probe counter %q: %v", data, err)
	}
	return n
}

func openWindow(t *testing.T, openPath string) {
	t.Helper()
	writeFile(t, openPath, "open\n")
}

// probeFixture is a park on a guessed cooldown: an isolated store, a claude that
// refuses until its window is opened, and the waiter the park registered.
type probeFixture struct {
	d           *Deps
	s           *store.Store
	root        string
	counterPath string
	openPath    string
}

func newProbeFixture(t *testing.T) *probeFixture {
	t.Helper()
	d := newTestDeps(t)
	root := t.TempDir()
	counterPath, openPath := installClaudeProbeAgent(t, root)
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return &probeFixture{d: d, s: s, root: root, counterPath: counterPath, openPath: openPath}
}

// park records a refusal against claude and registers the waiter that sits
// behind it, exactly as a quota-paused drain does.
func (f *probeFixture) park(t *testing.T, setID string, class AgentQuotaWindowClass, at time.Time) *RecoveryWaiter {
	t.Helper()
	until, err := recordAgentQuotaCooldown(f.d, AgentQuotaCooldownRequest{Preset: "claude", Class: class}, at, 0)
	if err != nil {
		t.Fatalf("record cooldown: %v", err)
	}
	runtimePath := filepath.Join(f.root, setID)
	if err := os.MkdirAll(runtimePath, 0o755); err != nil {
		t.Fatal(err)
	}
	w := &RecoveryWaiter{SetID: setID, Preset: "claude", ResetAt: until, RuntimePath: runtimePath, RegisteredAt: at}
	if err := f.s.PutRecoveryWaiter(store.RecoveryWaiter{
		SetID: w.SetID, Preset: w.Preset, ResetAt: w.ResetAt, RuntimePath: w.RuntimePath, RegisteredAt: w.RegisteredAt,
	}); err != nil {
		t.Fatalf("register waiter: %v", err)
	}
	return w
}

func (f *probeFixture) cooldown(t *testing.T) *store.AgentCooldown {
	t.Helper()
	row, err := f.s.GetAgentCooldown("claude")
	if err != nil {
		t.Fatalf("read cooldown: %v", err)
	}
	return row
}

// The window class sets the cadence: dense enough for a five-hour window that a
// reopening costs minutes, sparse enough for a weekly one that a week does not
// cost a thousand refused invocations.
func TestQuotaProbeIntervalFollowsTheWindowClass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		class AgentQuotaWindowClass
		want  time.Duration
	}{
		{QuotaWindowFiveHour, 10 * time.Minute},
		{QuotaWindowWeekly, 2 * time.Hour},
		{QuotaWindowOpus, 2 * time.Hour},
		{QuotaWindowUnknown, 10 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(string(tc.class)+"/unknown", func(t *testing.T) {
			if got := quotaProbeInterval(tc.class, 0); got != tc.want {
				t.Fatalf("interval for %q = %s, want %s", tc.class, got, tc.want)
			}
		})
	}
	// A named window's cadence is the window's, however long the park has
	// already been refused: only the assumed span escalates.
	if got := quotaProbeInterval(QuotaWindowFiveHour, 40*time.Hour); got != 10*time.Minute {
		t.Fatalf("a five-hour window's interval drifted to %s", got)
	}
}

// A refusal naming no window assumes the shortest one, and walks toward the
// bound once that assumption has been disproved by the park outliving it.
func TestUnclassedQuotaProbeWidensOnceTheShortestWindowElapses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		refusedFor time.Duration
		want       time.Duration
	}{
		{0, 10 * time.Minute},
		{4 * time.Hour, 10 * time.Minute},
		{5 * time.Hour, 10 * time.Minute},
		{10 * time.Hour, 20 * time.Minute},
		{30 * time.Hour, time.Hour},
		{7 * 24 * time.Hour, 2 * time.Hour},
	}
	for _, tc := range cases {
		if got := quotaProbeInterval(QuotaWindowUnknown, tc.refusedFor); got != tc.want {
			t.Errorf("interval after %s refused = %s, want %s", tc.refusedFor, got, tc.want)
		}
	}
}

// A guess is asked about; an instant a provider stated is waited to. The row
// says which by carrying a first ask, or none at all.
func TestOnlyAGuessedCooldownSchedulesAProbe(t *testing.T) {
	f := newProbeFixture(t)
	now := time.Date(2026, 8, 25, 19, 58, 0, 0, time.UTC)

	f.park(t, "guessed", QuotaWindowFiveHour, now)
	guessed := f.cooldown(t)
	if want := now.Add(10 * time.Minute); !guessed.NextProbeAt.Equal(want) {
		t.Fatalf("guessed next_probe_at = %s, want %s", guessed.NextProbeAt, want)
	}

	stated := now.Add(2 * time.Hour)
	if _, err := recordAgentQuotaCooldown(f.d, AgentQuotaCooldownRequest{
		Preset: "claude", Stated: stated, Class: QuotaWindowFiveHour,
	}, stated.Add(-time.Hour), 0); err != nil {
		t.Fatalf("record stated cooldown: %v", err)
	}
	if row := f.cooldown(t); !row.NextProbeAt.IsZero() {
		t.Fatalf("a stated cooldown scheduled an ask at %s", row.NextProbeAt)
	}

	// And the park behind a stated instant never asks: the session it builds is
	// inert, so the agent is not invoked at all.
	w := &RecoveryWaiter{SetID: "stated", Preset: "claude", ResetAt: stated, RuntimePath: f.root, RegisteredAt: now}
	session := newQuotaProbeSession(f.d, f.s, w, nil)
	for _, at := range []time.Time{stated.Add(-time.Hour), stated.Add(-time.Minute)} {
		if open, err := session.step(at); err != nil || open {
			t.Fatalf("step on a stated cooldown = (%v, %v), want (false, nil)", open, err)
		}
	}
	if n := probesAsked(t, f.counterPath); n != 0 {
		t.Fatalf("asked %d times about an instant the provider stated", n)
	}
}

// Refused, the ask advances and nothing else moves — least of all the ceiling,
// whose re-dating from a later refusal is the compounding this replaces.
func TestRefusedQuotaProbeSchedulesTheNextAndLeavesTheCeiling(t *testing.T) {
	f := newProbeFixture(t)
	now := time.Date(2026, 8, 25, 19, 58, 0, 0, time.UTC)
	w := f.park(t, "set-a", QuotaWindowFiveHour, now)
	ceiling := f.cooldown(t).ExhaustedUntil

	session := newQuotaProbeSession(f.d, f.s, w, nil)
	due := now.Add(10 * time.Minute)
	open, err := session.step(due)
	if err != nil {
		t.Fatalf("step: %v", err)
	}
	if open {
		t.Fatal("a refused ask should not open the window")
	}
	if n := probesAsked(t, f.counterPath); n != 1 {
		t.Fatalf("asked %d times, want 1", n)
	}
	row := f.cooldown(t)
	if !row.ExhaustedUntil.Equal(ceiling) {
		t.Fatalf("ceiling moved to %s, want %s", row.ExhaustedUntil, ceiling)
	}
	if want := due.Add(10 * time.Minute); !row.NextProbeAt.Equal(want) {
		t.Fatalf("next ask = %s, want %s", row.NextProbeAt, want)
	}
	if !row.ProbeLeaseUntil.IsZero() {
		t.Fatalf("the ask stayed claimed until %s after being answered", row.ProbeLeaseUntil)
	}
	if !row.StatedUntil.IsZero() {
		t.Fatalf("a refused probe invented a stated instant: %s", row.StatedUntil)
	}
}

// Answered yes, the cooldown is dropped and every park behind it — including
// those in other checkouts, which never ran a probe of their own — resumes
// through its ordinary recovery turn.
func TestAllowedQuotaProbeClearsTheCooldownForEveryPark(t *testing.T) {
	f := newProbeFixture(t)
	now := time.Date(2026, 8, 25, 19, 58, 0, 0, time.UTC)
	a := f.park(t, "set-a", QuotaWindowFiveHour, now)
	b := &RecoveryWaiter{SetID: "set-b", Preset: "claude", ResetAt: a.ResetAt, RuntimePath: filepath.Join(f.root, "set-b"), RegisteredAt: now}
	if err := f.s.PutRecoveryWaiter(store.RecoveryWaiter{
		SetID: b.SetID, Preset: b.Preset, ResetAt: b.ResetAt, RuntimePath: b.RuntimePath, RegisteredAt: b.RegisteredAt,
	}); err != nil {
		t.Fatal(err)
	}
	sessionA := newQuotaProbeSession(f.d, f.s, a, nil)
	sessionB := newQuotaProbeSession(f.d, f.s, b, nil)

	openWindow(t, f.openPath)
	due := now.Add(10 * time.Minute)
	if open, err := sessionA.step(due); err != nil || !open {
		t.Fatalf("step on an open window = (%v, %v), want (true, nil)", open, err)
	}
	if row := f.cooldown(t); row != nil {
		t.Fatalf("cooldown survived an allowed ask: %#v", row)
	}
	// B asked nothing and still resumes: the answer was about the preset, not
	// about the checkout that happened to ask.
	if open, err := sessionB.step(due); err != nil || !open {
		t.Fatalf("the other checkout's park = (%v, %v), want (true, nil)", open, err)
	}
	if n := probesAsked(t, f.counterPath); n != 1 {
		t.Fatalf("asked %d times for two parks, want 1", n)
	}
	waiters, err := f.s.AllRecoveryWaiters()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range waiters {
		if w.ResetAt.After(due) {
			t.Errorf("waiter %s still parked until %s", w.SetID, w.ResetAt)
		}
	}
}

// The cooldown is machine-global while parks are per-checkout, so the claim is
// what keeps three parked worktrees from each asking the same question.
func TestQuotaProbeAsksOncePerIntervalAcrossCheckouts(t *testing.T) {
	f := newProbeFixture(t)
	now := time.Date(2026, 8, 25, 19, 58, 0, 0, time.UTC)
	first := f.park(t, "set-a", QuotaWindowFiveHour, now)
	var sessions []*quotaProbeSession
	sessions = append(sessions, newQuotaProbeSession(f.d, f.s, first, nil))
	for _, id := range []string{"set-b", "set-c"} {
		w := &RecoveryWaiter{SetID: id, Preset: "claude", ResetAt: first.ResetAt, RuntimePath: filepath.Join(f.root, id), RegisteredAt: now}
		sessions = append(sessions, newQuotaProbeSession(f.d, f.s, w, nil))
	}

	due := now.Add(10 * time.Minute)
	for i, session := range sessions {
		if open, err := session.step(due); err != nil || open {
			t.Fatalf("session %d = (%v, %v), want (false, nil)", i, open, err)
		}
	}
	if n := probesAsked(t, f.counterPath); n != 1 {
		t.Fatalf("three parked checkouts asked %d times in one interval, want 1", n)
	}
	// The next interval is one ask again, whichever checkout wins it.
	for _, session := range sessions {
		if _, err := session.step(due.Add(10 * time.Minute)); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	if n := probesAsked(t, f.counterPath); n != 2 {
		t.Fatalf("asked %d times over two intervals, want 2", n)
	}
}

// The ask is cheap by construction: the store-pure attempt path leaves no Drain
// row, no Captured run and no spent Task retry cap behind it.
func TestQuotaProbeWritesNoDrainRecordNorCapturedRun(t *testing.T) {
	f := newProbeFixture(t)
	now := time.Date(2026, 8, 25, 19, 58, 0, 0, time.UTC)
	w := f.park(t, "set-a", QuotaWindowFiveHour, now)
	session := newQuotaProbeSession(f.d, f.s, w, nil)
	if _, err := session.step(now.Add(10 * time.Minute)); err != nil {
		t.Fatalf("step: %v", err)
	}
	if n := probesAsked(t, f.counterPath); n != 1 {
		t.Fatalf("asked %d times, want 1", n)
	}

	drains, err := f.s.AllDrains()
	if err != nil {
		t.Fatal(err)
	}
	if len(drains) != 0 {
		t.Fatalf("the ask recorded %d drains: %#v", len(drains), drains)
	}
	caps, err := f.s.AllSpentRetryCaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 0 {
		t.Fatalf("the ask spent a retry cap: %#v", caps)
	}
	if entries, err := os.ReadDir(filepath.Join(w.RuntimePath, streamsDirName)); err == nil && len(entries) > 0 {
		t.Fatalf("the ask captured %d runs", len(entries))
	}
}

// ADR-0233's incident, replayed: claude refuses at 19:58 with no window named,
// and the window it exhausted reopens at 21:00. The old blind hour answered the
// early retry with another hour from the later moment — 19:58 → 20:58 → 21:58
// against a window that had been open since 21:00. Now the park asks, and the
// first ask after 21:00 ends it.
func TestQuotaProbeIncidentReplayResumesWhenTheWindowReopens(t *testing.T) {
	f := newProbeFixture(t)
	refusedAt := time.Date(2026, 8, 25, 19, 58, 0, 0, time.UTC)
	reopensAt := time.Date(2026, 8, 25, 21, 0, 0, 0, time.UTC)
	w := f.park(t, "set-a", QuotaWindowUnknown, refusedAt)
	ceiling := f.cooldown(t).ExhaustedUntil
	if want := refusedAt.Add(5 * time.Hour); !ceiling.Equal(want) {
		t.Fatalf("ceiling = %s, want %s", ceiling, want)
	}

	session := newQuotaProbeSession(f.d, f.s, w, nil)
	var resumedAt time.Time
	for at := refusedAt; !at.After(ceiling); at = at.Add(time.Minute) {
		if !at.Before(reopensAt) {
			openWindow(t, f.openPath)
		}
		open, err := session.step(at)
		if err != nil {
			t.Fatalf("step at %s: %v", at, err)
		}
		if row := f.cooldown(t); row != nil && !row.ExhaustedUntil.Equal(ceiling) {
			t.Fatalf("ceiling moved to %s at %s", row.ExhaustedUntil, at)
		}
		if open {
			resumedAt = at
			break
		}
	}

	if resumedAt.IsZero() {
		t.Fatal("the park sat out the whole ceiling with the window open since 21:00")
	}
	if resumedAt.Before(reopensAt) {
		t.Fatalf("resumed at %s, before the window reopened", resumedAt)
	}
	if overshoot := resumedAt.Sub(reopensAt); overshoot > minQuotaProbeInterval {
		t.Fatalf("resumed %s after the window reopened, want at most one ask apart (%s)", overshoot, minQuotaProbeInterval)
	}
	if row := f.cooldown(t); row != nil {
		t.Fatalf("cooldown survived the reopening: %#v", row)
	}
	// Six refused asks between 20:08 and 20:58, then the one that resumed it.
	if n := probesAsked(t, f.counterPath); n != 7 {
		t.Fatalf("asked %d times over an hour of refusal, want 7", n)
	}
}

// The park itself, not just the schedule: a waiting drain asks on its recovery
// poll and takes its recovery turn the moment the preset says yes, without the
// ceiling elapsing.
func TestParkedDrainResumesOnAnAllowedQuotaProbe(t *testing.T) {
	f := newProbeFixture(t)
	// The refusal is twenty minutes old, so an ask is due now and the ceiling is
	// still hours away — the wait can only end by asking.
	refusedAt := time.Now().UTC().Add(-20 * time.Minute)
	w := f.park(t, "set-a", QuotaWindowFiveHour, refusedAt)
	openWindow(t, f.openPath)

	var buf bytes.Buffer
	if err := WaitForRecovery(f.d, w, outputFor(&buf)); err != nil {
		t.Fatalf("WaitForRecovery: %v", err)
	}
	if row := f.cooldown(t); row != nil {
		t.Fatalf("cooldown survived the park: %#v", row)
	}
	if n := probesAsked(t, f.counterPath); n != 1 {
		t.Fatalf("asked %d times, want 1", n)
	}
	// The turn is held by the resumed set, which is what an ordinary recovery
	// turn looks like from here.
	if err := ReleaseRecoveryTurn(f.d, w.RuntimePath); err != nil {
		t.Fatalf("release turn: %v", err)
	}
}

// A guessed ceiling is a backstop, not a reset time, and the park's countdown
// says which it is holding: stating it as a confident instant is the reporting
// failure that sent the incident's human to a raw SQL delete.
func TestGuessedParkCountdownStatesNoReset(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for _, guessed := range []bool{false, true} {
		var buf bytes.Buffer
		p := &recoveryPrinter{out: outputFor(&buf), heartbeat: recoveryHeartbeat}
		p.countdown(now, "claude", now.Add(2*time.Hour), guessed)
		line := buf.String()
		if guessed && strings.Contains(line, "resets at") {
			t.Fatalf("a guessed ceiling was printed as a reset: %q", line)
		}
		if !guessed && !strings.Contains(line, "resets at") {
			t.Fatalf("a stated reset lost its wording: %q", line)
		}
		if guessed && !strings.Contains(line, "stated no reset") {
			t.Fatalf("a guessed park did not say the reset was never stated: %q", line)
		}
	}
}
