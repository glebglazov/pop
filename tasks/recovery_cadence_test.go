package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// countLines counts non-empty status lines written to the buffer.
func countLines(buf *bytes.Buffer) int {
	n := 0
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// TestRecoveryPrinterCountdownPrintsEachCall pins that the pre-reset countdown
// emits exactly one line per call (i.e. once per poll tick).
func TestRecoveryPrinterCountdownPrintsEachCall(t *testing.T) {
	var buf bytes.Buffer
	p := &recoveryPrinter{out: outputFor(&buf), heartbeat: recoveryHeartbeat}

	base := time.Now().UTC()
	resetAt := base.Add(time.Hour)
	for i := 0; i < 5; i++ {
		p.countdown(base.Add(time.Duration(i)*30*time.Second), "claude", resetAt)
	}

	if got := countLines(&buf); got != 5 {
		t.Fatalf("countdown lines = %d, want 5 (one per call)", got)
	}
}

// TestRecoveryPrinterBlockedReasonChange pins that the post-reset block line
// prints only when the reason changes and otherwise stays silent within the
// heartbeat window.
func TestRecoveryPrinterBlockedReasonChange(t *testing.T) {
	var buf bytes.Buffer
	p := &recoveryPrinter{out: outputFor(&buf), heartbeat: recoveryHeartbeat}

	base := time.Now().UTC()
	held := &store.RecoveryBlock{Kind: store.RecoveryBlockTurnHeld, SetID: "set-a"}

	// First observation prints.
	p.blocked(base, held)
	// Same reason within heartbeat: silent for several ticks.
	for i := 1; i <= 20; i++ { // 20 ticks of 2s = 40s, under the 60s heartbeat
		p.blocked(base.Add(time.Duration(i)*2*time.Second), held)
	}
	if got := countLines(&buf); got != 1 {
		t.Fatalf("unchanged block within heartbeat printed %d lines, want 1", got)
	}

	// Different kind (a claim block): prints again.
	drain := &store.RecoveryBlock{Kind: store.RecoveryBlockClaimed, SetID: "set-a",
		Claim: &store.CheckoutClaim{Kind: store.ClaimRunningDrain, SetID: "set-a"}}
	p.blocked(base.Add(41*time.Second), drain)
	if got := countLines(&buf); got != 2 {
		t.Fatalf("reason-kind change printed total %d lines, want 2", got)
	}

	// Same kind, different blocking set: prints again.
	drainOther := &store.RecoveryBlock{Kind: store.RecoveryBlockClaimed, SetID: "set-b",
		Claim: &store.CheckoutClaim{Kind: store.ClaimRunningDrain, SetID: "set-b"}}
	p.blocked(base.Add(42*time.Second), drainOther)
	if got := countLines(&buf); got != 3 {
		t.Fatalf("blocking-set change printed total %d lines, want 3", got)
	}

	// Same set, different claim kind (drain → failed gate): prints again.
	gateClaim := &store.RecoveryBlock{Kind: store.RecoveryBlockClaimed, SetID: "set-b",
		Claim: &store.CheckoutClaim{Kind: store.ClaimFailedGate, SetID: "set-b"}}
	p.blocked(base.Add(43*time.Second), gateClaim)
	if got := countLines(&buf); got != 4 {
		t.Fatalf("claim-kind change printed total %d lines, want 4", got)
	}
}

// TestRecoveryPrinterBlockedHeartbeat pins that an unchanged block reason
// reprints once the heartbeat interval elapses, so a long-held gate still
// shows life without printing every tick.
func TestRecoveryPrinterBlockedHeartbeat(t *testing.T) {
	var buf bytes.Buffer
	p := &recoveryPrinter{out: outputFor(&buf), heartbeat: recoveryHeartbeat}

	base := time.Now().UTC()
	gate := &store.RecoveryBlock{Kind: store.RecoveryBlockClaimed, SetID: "set-a",
		Claim: &store.CheckoutClaim{Kind: store.ClaimFailedGate, SetID: "set-a"}}

	// Drive 150 seconds of 2s fast-tick-equivalent calls with an unchanged
	// reason. Expect an initial print plus one per elapsed 60s heartbeat.
	for i := 0; i <= 75; i++ { // 0..150s
		p.blocked(base.Add(time.Duration(i)*2*time.Second), gate)
	}

	// Prints at t=0, then first reprint at the first call with elapsed >= 60s
	// (t=60s), then next at elapsed >= 60s from there (t=120s): 3 total.
	if got := countLines(&buf); got != 3 {
		t.Fatalf("heartbeat reprints = %d, want 3 (initial + 60s + 120s)", got)
	}
}

// TestRecoveryPrinterBlockedNilNoop pins that a nil block prints nothing.
func TestRecoveryPrinterBlockedNilNoop(t *testing.T) {
	var buf bytes.Buffer
	p := &recoveryPrinter{out: outputFor(&buf), heartbeat: recoveryHeartbeat}
	p.blocked(time.Now().UTC(), nil)
	if got := countLines(&buf); got != 0 {
		t.Fatalf("nil block printed %d lines, want 0", got)
	}
}
