package tasks

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/glebglazov/pop/store"
)

// CheckoutGateHold records that a drain is parked at a human-wait gate on a
// runtime checkout. While active it blocks recovery turn acquisition on that
// path (ADR-0100). PID and ProcStart identify the registering process so a hold
// whose owner dies (a crash while a human sat at the gate) is swept by the
// reconcile pass rather than blocking the checkout forever.
type CheckoutGateHold struct {
	SetID        string
	RuntimePath  string
	PID          int
	ProcStart    string
	RegisteredAt time.Time
}

// RegisterCheckoutGateHold records a gate hold for one runtime path. The
// runtime_path is UNIQUE: a second registration for the same checkout replaces
// the first. The hold captures the registering process's PID and start token so
// the reconcile pass can sweep it if that process dies while parked at the gate.
func RegisterCheckoutGateHold(d *Deps, setID, runtimePath string) error {
	if setID == "" || runtimePath == "" {
		return nil
	}
	s, err := openDrainStore(d)
	if err != nil {
		return exitErr(ExitOperational, "register checkout gate hold: %v", err)
	}
	pid := os.Getpid()
	procStart, _ := procStartToken(d, pid)
	if err := s.PutCheckoutGateHold(store.CheckoutGateHold{
		SetID:        setID,
		RuntimePath:  runtimePath,
		PID:          pid,
		ProcStart:    procStart,
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		return exitErr(ExitOperational, "register checkout gate hold: %v", err)
	}
	return nil
}

// ReleaseCheckoutGateHold removes the gate hold for one runtime path. A missing
// row is not an error.
func ReleaseCheckoutGateHold(d *Deps, runtimePath string) error {
	if runtimePath == "" {
		return nil
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return s.DeleteCheckoutGateHold(runtimePath)
}

// GetCheckoutGateHold returns the active gate hold for one runtime path, or nil
// when no hold is registered.
func GetCheckoutGateHold(d *Deps, runtimePath string) (*CheckoutGateHold, error) {
	if runtimePath == "" {
		return nil, nil
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	h, err := s.GetCheckoutGateHold(runtimePath)
	if err != nil {
		return nil, err
	}
	if h == nil {
		return nil, nil
	}
	return &CheckoutGateHold{
		SetID:        h.SetID,
		RuntimePath:  h.RuntimePath,
		PID:          h.PID,
		ProcStart:    h.ProcStart,
		RegisteredAt: h.RegisteredAt,
	}, nil
}

// ParkAndWaitForQuotaRecovery parks the drain with quota_paused, registers a
// recovery waiter, polls until the preset's cooldown elapses and a recovery turn
// is acquired, then re-acquires the drain via ensureDrain. Returns
// (registrationFailed, err): when registrationFailed is true the caller should
// fall back to the legacy quota-paused exit; err is non-nil on wait/interrupt
// failures or when ensureDrain refuses.
func ParkAndWaitForQuotaRecovery(
	d *Deps,
	drain **DrainHandle,
	setID, preset string,
	resetAt time.Time,
	runtimePath string,
	priority int,
	out io.Writer,
	ensureDrain func() error,
) (registrationFailed bool, err error) {
	if resetAt.IsZero() {
		cooldowns, readErr := readAgentCooldowns(d)
		if readErr == nil {
			if entry, ok := cooldowns[preset]; ok {
				resetAt = entry.ExhaustedUntil
			}
		}
	}

	if *drain != nil {
		_ = (*drain).Finish(store.StateQuotaPaused, preset, false, resetAt)
		*drain = nil
	}

	waiter, regErr := RegisterRecoveryWaiter(d, RecoveryWaiter{
		SetID:        setID,
		Preset:       preset,
		ResetAt:      resetAt,
		RuntimePath:  runtimePath,
		Priority:     priority,
		RegisteredAt: time.Now().UTC(),
	})
	if regErr != nil {
		return true, nil
	}

	if waitErr := WaitForRecovery(d, waiter, outputFor(out)); waitErr != nil {
		return false, waitErr
	}

	if err := ensureDrain(); err != nil {
		_ = ReleaseRecoveryTurn(d, runtimePath)
		return false, err
	}
	_ = DeregisterRecoveryWaiter(d, setID)
	_ = ReleaseRecoveryTurn(d, runtimePath)
	return false, nil
}

// agent's quota is exhausted during a task attempt, instead of exiting with
// ExitQuotaPaused, the drain parks, registers a waiter, and polls until the
// preset's cooldown elapses and a recovery turn is acquired (ADR-0100).
type RecoveryWaiter struct {
	SetID        string
	Preset       string
	ResetAt      time.Time
	RuntimePath  string
	Priority     int
	RegisteredAt time.Time
}

// RegisterRecoveryWaiter records a quota-recovery wait in the global store. The
// set_id is UNIQUE: a second registration for the same set replaces the first,
// so a crash-restart does not duplicate the row. Returns the registered waiter.
func RegisterRecoveryWaiter(d *Deps, w RecoveryWaiter) (*RecoveryWaiter, error) {
	if w.SetID == "" || w.Preset == "" || w.ResetAt.IsZero() || w.RuntimePath == "" {
		return nil, exitErr(ExitOperational, "invalid recovery waiter: missing required fields")
	}
	s, err := openDrainStore(d)
	if err != nil {
		return nil, err
	}

	if w.RegisteredAt.IsZero() {
		w.RegisteredAt = time.Now().UTC()
	}
	if err := s.PutRecoveryWaiter(store.RecoveryWaiter{
		SetID:        w.SetID,
		Preset:       w.Preset,
		ResetAt:      w.ResetAt,
		RuntimePath:  w.RuntimePath,
		Priority:     w.Priority,
		RegisteredAt: w.RegisteredAt,
	}); err != nil {
		return nil, exitErr(ExitOperational, "register recovery waiter: %v", err)
	}
	return &w, nil
}

// DeregisterRecoveryWaiter removes the recovery waiter for one task set. A
// missing row is not an error. Called on SIGINT during the wait loop or after
// successful recovery.
func DeregisterRecoveryWaiter(d *Deps, setID string) error {
	if setID == "" {
		return nil
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return s.DeleteRecoveryWaiter(setID)
}

// GetRecoveryWaiter returns the recovery waiter for one task set, or nil when
// no waiter is registered.
func GetRecoveryWaiter(d *Deps, setID string) (*RecoveryWaiter, error) {
	if setID == "" {
		return nil, nil
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	w, err := s.GetRecoveryWaiter(setID)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, nil
	}
	return &RecoveryWaiter{
		SetID:        w.SetID,
		Preset:       w.Preset,
		ResetAt:      w.ResetAt,
		RuntimePath:  w.RuntimePath,
		Priority:     w.Priority,
		RegisteredAt: w.RegisteredAt,
	}, nil
}

// countRecoveryWaitersOnPath returns how many recovery waiters are registered on
// one runtime checkout, counting every waiter regardless of whether its cooldown
// has elapsed — all of them are queued behind whatever holds that checkout. A nil
// deps, empty path, or store read error yields 0 so a gate menu degrades to
// showing no count line rather than failing.
func countRecoveryWaitersOnPath(d *Deps, runtimePath string) int {
	if d == nil || runtimePath == "" {
		return 0
	}
	waiters, err := AllRecoveryWaiters(d)
	if err != nil {
		return 0
	}
	n := 0
	for _, w := range waiters {
		if w.RuntimePath == runtimePath {
			n++
		}
	}
	return n
}

// renderBlockedWaiterCount prints a dim line above a hold-registering gate menu
// naming how many quota-recovery waiters are queued behind this checkout, so the
// human sitting at the gate sees they are holding those drains up (ADR-0100).
// Nothing prints when no waiter is registered on the path.
func renderBlockedWaiterCount(display *output, d *Deps, runtimePath string) {
	if display == nil {
		return
	}
	n := countRecoveryWaitersOnPath(d, runtimePath)
	if n <= 0 {
		return
	}
	noun := "waiters"
	if n == 1 {
		noun = "waiter"
	}
	display.line(ansiDim, "⏳ %d quota %s blocked on this checkout", n, noun)
}

// AllRecoveryWaiters returns every registered recovery waiter keyed by set ID.
// A missing store or read error yields an empty map so queue dispatch degrades
// to spawnable rather than blocking on a transient store problem.
func AllRecoveryWaiters(d *Deps) (map[string]RecoveryWaiter, error) {
	if d == nil {
		return map[string]RecoveryWaiter{}, nil
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string]RecoveryWaiter{}, nil
	}
	list, err := s.AllRecoveryWaiters()
	if err != nil {
		return nil, err
	}
	out := make(map[string]RecoveryWaiter, len(list))
	for _, w := range list {
		out[w.SetID] = RecoveryWaiter{
			SetID:        w.SetID,
			Preset:       w.Preset,
			ResetAt:      w.ResetAt,
			RuntimePath:  w.RuntimePath,
			Priority:     w.Priority,
			RegisteredAt: w.RegisteredAt,
		}
	}
	return out, nil
}

// WaitForRecovery polls until the preset's cooldown elapses and a recovery turn
// is acquired. It prints a dim status line with reset/cooldown timing during the
// poll loop. SIGINT during the wait deregisters the waiter and returns
// ExitInterrupted. The caller must have already parked the drain (released the
// runtime lock) before calling this.
//
// Returns nil when recovery is ready (cooldown elapsed and turn acquired).
// Returns an ExitError with ExitInterrupted on SIGINT.
func WaitForRecovery(d *Deps, w *RecoveryWaiter, out *output) error {
	if w == nil {
		return exitErr(ExitOperational, "nil recovery waiter")
	}

	// Open the store once and reuse it for all checks to avoid connection contention.
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil {
		return exitErr(ExitOperational, "open store for recovery wait: %v", err)
	}
	if !ok {
		return exitErr(ExitOperational, "store not available for recovery wait")
	}

	// Install signal handler for SIGINT during the wait loop.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Poll interval: check every 30 seconds, or every 5 seconds if reset is
	// imminent (within 5 minutes).
	pollInterval := func() time.Duration {
		if time.Until(w.ResetAt) < 5*time.Minute {
			return 5 * time.Second
		}
		return 30 * time.Second
	}

	ticker := time.NewTicker(pollInterval())
	defer ticker.Stop()

	// Fast check interval for detecting external deregistration. This is a
	// silent store read only: it never prints and never drives the status line
	// (ADR-0100 recovery-wait cadence).
	fastCheckInterval := 2 * time.Second
	fastTicker := time.NewTicker(fastCheckInterval)
	defer fastTicker.Stop()

	printer := &recoveryPrinter{out: out, heartbeat: recoveryHeartbeat}

	// poll runs one acquisition-or-countdown pass. Called once per regular poll
	// tick (never on the fast check), so the status line appears at most once
	// per poll interval. Returns true when recovery is ready.
	poll := func() (bool, error) {
		now := time.Now().UTC()
		resetAt := w.ResetAt.UTC()
		if !now.Before(resetAt) {
			// Cooldown elapsed. Try to acquire a recovery turn.
			acquired, block, err := acquireRecoveryTurnWithStore(s, w)
			if err != nil {
				return false, err
			}
			if acquired {
				return true, nil
			}
			// Turn not acquired: the cooldown is over but something on the
			// checkout still blocks resumption. Name the blocker rather than
			// pretending we are still waiting on quota. Print only on a reason
			// change or the periodic heartbeat.
			printer.blocked(now, block)
		} else {
			// Pre-cooldown: still waiting for the preset's quota to reset.
			printer.countdown(now, w.Preset, resetAt)
		}
		return false, nil
	}

	// Print the opening status line immediately so the wait shows life at once;
	// subsequent countdown/block lines follow the poll cadence.
	if done, err := poll(); err != nil || done {
		return err
	}
	ticker.Reset(pollInterval())

	for {
		select {
		case <-fastTicker.C:
			// Fast check: silent store read to see if the waiter still exists.
			// If it was deregistered externally (e.g., by a test or another
			// process), exit the wait loop immediately. No status line here.
			existing, err := s.GetRecoveryWaiter(w.SetID)
			if err != nil {
				return exitErr(ExitOperational, "check recovery waiter: %v", err)
			}
			if existing == nil {
				if out != nil {
					out.line(ansiYellow, "Recovery waiter deregistered externally")
				}
				return exitErr(ExitInterrupted, "recovery waiter deregistered")
			}
			// Continue waiting.
		case <-ticker.C:
			// Regular poll: acquire-or-print, then adjust interval as the reset
			// approaches.
			if done, err := poll(); err != nil || done {
				return err
			}
			ticker.Reset(pollInterval())
		case sig := <-sigCh:
			_ = sig
			// SIGINT: deregister and exit interrupted.
			if out != nil {
				out.line(ansiYellow, "Interrupted: deregistering recovery waiter")
			}
			_ = s.DeleteRecoveryWaiter(w.SetID)
			return exitErr(ExitInterrupted, "interrupted during quota recovery wait")
		case <-ctx.Done():
			return exitErr(ExitInterrupted, "context cancelled during quota recovery wait")
		}
	}
}

// recoveryHeartbeat is how often the post-cooldown block line reprints while
// the blocking reason is unchanged, so a long-held gate still shows life.
const recoveryHeartbeat = 60 * time.Second

// recoveryPrinter decides when the recovery wait loop emits a status line,
// decoupling printing cadence from the poll/fast-check ticker cadence (ADR-0100).
// The pre-reset countdown prints on every poll tick; the post-reset block line
// prints only when the reason changes (different kind or blocking set) or when
// the heartbeat interval elapses.
type recoveryPrinter struct {
	out       *output
	heartbeat time.Duration

	haveBlock   bool
	lastKind    store.RecoveryBlockKind
	lastSetID   string
	lastBlockAt time.Time
}

// countdown prints the pre-reset waiting line. It is called once per poll tick,
// so the countdown appears at most once per poll interval.
func (p *recoveryPrinter) countdown(now time.Time, preset string, resetAt time.Time) {
	if p == nil || p.out == nil {
		return
	}
	waitDur := resetAt.Sub(now)
	if waitDur < 0 {
		waitDur = 0
	}
	p.out.line(ansiDim, "⏳ Waiting for quota recovery: %s resets at %s (in %s)",
		preset,
		resetAt.Local().Format("15:04:05 MST"),
		formatDuration(waitDur))
}

// blocked prints the post-cooldown block line, but only on a reason change or
// once the heartbeat interval has elapsed since the last print. A nil block is
// a no-op (nothing blocks, so nothing to say).
func (p *recoveryPrinter) blocked(now time.Time, block *store.RecoveryBlock) {
	if p == nil || p.out == nil || block == nil {
		return
	}
	changed := !p.haveBlock || block.Kind != p.lastKind || block.SetID != p.lastSetID
	if !changed && now.Sub(p.lastBlockAt) < p.heartbeat {
		return
	}
	p.out.line(ansiDim, "⏸ Quota recovered; waiting for checkout — %s",
		recoveryBlockMessage(block))
	p.haveBlock = true
	p.lastKind = block.Kind
	p.lastSetID = block.SetID
	p.lastBlockAt = now
}

// acquireRecoveryTurn attempts to acquire a recovery turn for the waiter.
// Returns true when the turn is acquired and the caller can proceed to resume
// the task. Returns false when the turn is not yet available; the *store.RecoveryBlock
// names the blocker when the cooldown has elapsed but the turn was denied.
func acquireRecoveryTurn(d *Deps, w *RecoveryWaiter) (bool, *store.RecoveryBlock, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil {
		return false, nil, err
	}
	if !ok {
		return false, nil, nil
	}
	return acquireRecoveryTurnWithStore(s, w)
}

// acquireRecoveryTurnWithStore is like acquireRecoveryTurn but takes an already-open
// store to avoid connection contention when called in a tight loop.
func acquireRecoveryTurnWithStore(s *store.Store, w *RecoveryWaiter) (bool, *store.RecoveryBlock, error) {
	return s.TryAcquireRecoveryTurn(store.RecoveryWaiter{
		SetID:        w.SetID,
		Preset:       w.Preset,
		ResetAt:      w.ResetAt,
		RuntimePath:  w.RuntimePath,
		Priority:     w.Priority,
		RegisteredAt: w.RegisteredAt,
	}, time.Now().UTC())
}

// recoveryBlockMessage renders a post-cooldown recovery block as a human phrase
// naming the actual blocker, with distinct wording per kind.
func recoveryBlockMessage(b *store.RecoveryBlock) string {
	switch b.Kind {
	case store.RecoveryBlockGateHold:
		return fmt.Sprintf("gate hold by set %s", b.SetID)
	case store.RecoveryBlockLiveDrain:
		return fmt.Sprintf("drain running for set %s", b.SetID)
	case store.RecoveryBlockTurnHeld:
		return fmt.Sprintf("recovery turn held by set %s", b.SetID)
	case store.RecoveryBlockBehindWaiter:
		return fmt.Sprintf("behind waiter %s", b.SetID)
	default:
		return fmt.Sprintf("blocked by set %s", b.SetID)
	}
}

// ReleaseRecoveryTurn drops the checkout-scoped recovery turn for one runtime
// path. A missing row is not an error.
func ReleaseRecoveryTurn(d *Deps, runtimePath string) error {
	if runtimePath == "" {
		return nil
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return s.ReleaseRecoveryTurn(runtimePath)
}

// parseTime parses a time string in RFC3339Nano format, returning the zero
// value when the input is empty or unparseable.
// formatDuration formats a duration as a human-readable string, e.g. "2h 15m"
// or "45s".
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
