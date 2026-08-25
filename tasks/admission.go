package tasks

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/glebglazov/pop/internal/tty"
	"github.com/glebglazov/pop/store"
)

// AdmissionPolicy is what a caller does when the checkout or the set it wants is
// held: refuse with the claim's own sentence, or join the Admission queue and
// wait for a grant (ADR-0239). It is resolved per invocation rather than fixed
// per call site, because the same code path serves a human at a terminal and a
// machine that wants an exit code.
type AdmissionPolicy int

const (
	// AdmissionRefuse exits non-zero naming the claim. It is the machine's half
	// of admission: a script or a daemon that blocks forever is worse than one
	// that reports busy.
	AdmissionRefuse AdmissionPolicy = iota
	// AdmissionWait registers in the Admission queue and blocks until a window
	// opens, printing who holds the checkout and where to reach them. Unbounded
	// by design: a timeout hands the re-run back to the human at the least
	// predictable moment, which is the failure waiting exists to remove.
	AdmissionWait
)

// AdmissionWaitChoice is the `--wait` / `--no-wait` tri-state as a command
// receives it: the flags override in both directions, and the unset default asks
// whether a human is actually there.
type AdmissionWaitChoice int

const (
	// AdmissionWaitAuto waits when the invocation is attended and refuses when it
	// is not.
	AdmissionWaitAuto AdmissionWaitChoice = iota
	// AdmissionWaitAlways waits even unattended (`--wait`): a drain sent into a
	// pane to sit there until the tree frees up.
	AdmissionWaitAlways
	// AdmissionWaitNever refuses even at a terminal (`--no-wait`): today's
	// behaviour, kept for the human who would rather see the error.
	AdmissionWaitNever
)

// Policy resolves the choice against the invocation's confirmation input. The
// default turns on an interactive terminal specifically, not on
// prompt-ability: a piped stdin, an explicit NonInteractiveReader and a test's
// buffer are all machines, and a machine that blocks forever is the outcome
// ADR-0239 rules out.
func (c AdmissionWaitChoice) Policy(in io.Reader) AdmissionPolicy {
	switch c {
	case AdmissionWaitAlways:
		return AdmissionWait
	case AdmissionWaitNever:
		return AdmissionRefuse
	}
	if in == nil {
		in = os.Stdin
	}
	if _, nonInteractive := in.(NonInteractiveReader); nonInteractive {
		return AdmissionRefuse
	}
	if tty.IsTerminal(in) {
		return AdmissionWait
	}
	return AdmissionRefuse
}

// admissionHeartbeat is how often the wait line reprints while the blocking
// reason is unchanged, so a long wait still shows life without spinning.
const admissionHeartbeat = 60 * time.Second

// defaultAdmissionPollInterval is how often a waiting command re-asks for its
// grant. It is tighter than the recovery-wait cadence because what it waits on
// is a human finishing at a keyboard, not a quota window hours away.
const defaultAdmissionPollInterval = 5 * time.Second

// admissionPollInterval returns the grant-retry cadence, honoring an injected
// override and otherwise using the production default. Tests inject a small
// value so the wait loop advances without wall-clock waits.
func (d *Deps) admissionPollInterval() time.Duration {
	if d != nil && d.AdmissionPollInterval > 0 {
		return d.AdmissionPollInterval
	}
	return defaultAdmissionPollInterval
}

// awaitAdmission registers a place in the checkout's Admission queue and polls
// for an Admission grant until one arrives, printing the actionable wait line
// while it waits. The bool reports whether it ever saw a block — the caller
// re-derives its target's status when it did, because the world moved while it
// waited.
//
// The registration happens before the first grant attempt, never after a
// hopeful unqueued try: an unqueued attempt is not in the line and would jump
// every command already in it, which is exactly the guarantee the queue exists
// to make. The waiter is deregistered on every exit path — grant (where the
// grant transaction already consumed it), interrupt, or error — so a wait that
// ends never stalls the queue behind it.
func awaitAdmission(d *Deps, s *store.Store, row store.Drain, out io.Writer) (store.Drain, bool, error) {
	if out == nil {
		out = io.Discard
	}
	display := outputFor(out)

	waiter, err := s.RegisterAdmissionWaiter(store.AdmissionWaiter{
		RuntimePath:  row.RuntimePath,
		Repo:         row.Repo,
		SetID:        row.SetID,
		PID:          row.PID,
		ProcStart:    row.ProcStart,
		RegisteredAt: time.Now().UTC(),
	})
	if err != nil {
		return store.Drain{}, false, exitErr(ExitOperational, "join the admission queue: %v", err)
	}
	defer func() { _ = s.DeleteAdmissionWaiter(waiter.ID) }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(d.admissionPollInterval())
	defer ticker.Stop()

	printer := &admissionPrinter{out: display, heartbeat: admissionHeartbeat}
	waited := false
	for {
		row.StartedAt = time.Now().UTC()
		drain, block, err := s.TryAdmitDrain(row, waiter.ID)
		if err != nil {
			return store.Drain{}, waited, exitErr(ExitOperational, "record drain start: %v", err)
		}
		if block == nil {
			if waited {
				display.line(ansiGreen, "▶ Admitted to %s", row.RuntimePath)
			}
			return drain, waited, nil
		}
		waited = true
		printer.blocked(time.Now().UTC(), admissionBlockKey(block), func() string {
			return admissionBlockLine(d, s, block)
		})

		select {
		case <-sigCh:
			display.line(ansiYellow, "Interrupted: leaving the admission queue")
			return store.Drain{}, waited, exitErr(ExitInterrupted,
				"interrupted while waiting for admission to %s", row.RuntimePath)
		case <-ticker.C:
		}
	}
}

// admissionPrinter decides when the wait loop emits its line, decoupling the
// printing cadence from the poll cadence: the line reprints when the blocking
// reason changes — a different holder, a different claim reason, a move up the
// queue — and otherwise once per heartbeat, so a wait behind an hour-long gate
// neither spins nor goes silent.
type admissionPrinter struct {
	out       *output
	heartbeat time.Duration

	printed bool
	lastKey string
	lastAt  time.Time
}

// blocked decides whether this poll says anything, from the block's key alone,
// and only then asks render for the words. The split is not cosmetic: rendering
// resolves the holder's controlling terminal, which costs a process, and the
// loop polls far more often than it prints.
func (p *admissionPrinter) blocked(now time.Time, key string, render func() string) {
	if p == nil || p.out == nil || key == "" {
		return
	}
	if p.printed && key == p.lastKey && now.Sub(p.lastAt) < p.heartbeat {
		return
	}
	if line := render(); line != "" {
		p.out.line(ansiDim, "%s", line)
	}
	p.printed = true
	p.lastKey = key
	p.lastAt = now
}

// admissionBlockKey identifies what is being waited on, so the printer can tell
// "still the same thing" from "something changed" without rendering a line.
func admissionBlockKey(b *store.AdmissionBlock) string {
	if b == nil {
		return ""
	}
	switch {
	case b.Set != nil:
		return string(b.Kind) + "|" + b.Set.Set.ContainerID + "|" + b.Set.RuntimePath
	case b.Checkout != nil:
		return string(b.Kind) + "|" + b.Checkout.Holder.ContainerID + "|" + string(b.Checkout.Reason)
	default:
		return string(b.Kind) + "|" + b.AheadSetID
	}
}

// admissionBlockLine renders one Admission block as the wait line: what is being
// waited on, who holds it, why, and where to reach them. The contact half — PID,
// controlling tty, drain pane — is what makes an unbounded wait tolerable,
// because the answer is almost always to go and answer a prompt that is still
// open rather than to keep waiting.
func admissionBlockLine(d *Deps, s *store.Store, b *store.AdmissionBlock) string {
	if b == nil {
		return ""
	}
	switch b.Kind {
	case store.AdmissionBlockSetClaimed:
		if b.Set == nil {
			break
		}
		return fmt.Sprintf("⏳ Waiting for set %s — already draining in %s%s",
			b.Set.Set.ContainerID, b.Set.RuntimePath,
			admissionContact(d, s, b.Set.Set.ContainerID, b.Set.RuntimePath, b.Set.PID))
	case store.AdmissionBlockCheckoutClaimed:
		if b.Checkout == nil {
			break
		}
		return fmt.Sprintf("⏳ Waiting for checkout %s — held by set %s (%s)%s",
			b.Checkout.RuntimePath, b.Checkout.Holder.ContainerID, b.Checkout.Reason.Phrase(),
			admissionContact(d, s, b.Checkout.Holder.ContainerID, b.Checkout.RuntimePath, b.Checkout.PID))
	case store.AdmissionBlockBehindWaiter:
		return fmt.Sprintf("⏳ Waiting for the admission queue — set %s asked first", b.AheadSetID)
	}
	return "⏳ Waiting for admission"
}

// admissionContact renders the reachable half of the wait line — "(PID 4242, tty
// s003, pane %7)" — dropping each fact it cannot establish rather than printing
// a placeholder. Everything here is derived at print time from the holder's PID
// and set id, so it works for every claim source, including one that never
// recorded a pane.
func admissionContact(d *Deps, s *store.Store, setID, runtimePath string, pid int) string {
	var parts []string
	if pid > 0 {
		parts = append(parts, fmt.Sprintf("PID %d", pid))
	}
	if tty := processTTY(d, pid); tty != "" {
		parts = append(parts, "tty "+tty)
	}
	if pane := admissionHolderPane(s, setID, runtimePath); pane != "" {
		parts = append(parts, "pane "+pane)
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// admissionHolderPane finds the tmux pane the supervisor recorded for the
// holding set's drain, preferring one recorded against the same checkout when a
// set has drained in more than one. Empty when nothing was recorded — a
// foreground drain a human started by hand has no pane row.
func admissionHolderPane(s *store.Store, setID, runtimePath string) string {
	if s == nil || setID == "" {
		return ""
	}
	panes, err := s.AllDrainPanes()
	if err != nil {
		return ""
	}
	fallback := ""
	for _, p := range panes {
		if p.SetID != setID || p.PaneID == "" {
			continue
		}
		if p.RuntimePath == runtimePath {
			return p.PaneID
		}
		fallback = p.PaneID
	}
	return fallback
}

// QueuedCommand is one live place in an Admission queue at the tasks boundary:
// the set a waiting command wants to drain, the checkout it is queued for, and
// when it joined the line (ADR-0239). It is the read a display surface needs —
// who is waiting — rather than the store row, whose id is a queue position no
// reader outside the grant should reason about.
type QueuedCommand struct {
	SetID        string
	Repo         string
	RuntimePath  string
	PID          int
	RegisteredAt time.Time
}

// LiveAdmissionWaiters returns every registered Admission waiter whose command
// is still running, in registration order. Dead owners are filtered with the
// same PID+start-token liveness the running-drain read applies, so a closed
// terminal never leaves a phantom marker on a row between reconcile passes. It
// opens the store only when it already exists.
func LiveAdmissionWaiters(d *Deps) ([]QueuedCommand, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.AllAdmissionWaiters()
	if err != nil {
		return nil, err
	}
	var out []QueuedCommand
	for _, w := range rows {
		if !drainProcessAlive(d, w.PID, w.ProcStart) {
			continue
		}
		out = append(out, QueuedCommand{
			SetID:        w.SetID,
			Repo:         w.Repo,
			RuntimePath:  w.RuntimePath,
			PID:          w.PID,
			RegisteredAt: w.RegisteredAt,
		})
	}
	return out, nil
}
