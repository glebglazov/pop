package tasks

import (
	"io"
	"time"

	"github.com/glebglazov/pop/store"
)

// Quota probe cadence (ADR-0235). A cooldown pop guessed ends when the agent
// says it will run, so the numbers here decide how much of a reopened window is
// wasted before anyone asks. They are constants rather than config for the same
// reason the recovery poll cadence is: an operator has no way to choose them
// better than the window class does, and `[work.daemon].agent_quota_retry_after`
// already carries the one policy that is theirs — the unclassed ceiling.
const (
	// quotaProbesPerWindow divides the window class's span into asks. Thirty
	// bounds the overshoot to roughly three percent of the window, which is what
	// makes one number serve a five-hour window and a week-long one alike.
	quotaProbesPerWindow = 30
	// minQuotaProbeInterval is the densest pop asks. Ten minutes is
	// five-hours-over-thirty, and asking faster than this buys back minutes at
	// the price of a refused invocation every few minutes for hours.
	minQuotaProbeInterval = 10 * time.Minute
	// maxQuotaProbeInterval is the sparsest pop asks. A weekly window divided
	// thirty ways is nearly six hours, which is long enough that a window
	// reopening just after an ask goes unnoticed for most of a working day.
	maxQuotaProbeInterval = 2 * time.Hour
	// quotaProbeLeaseTTL is how long the checkout that took an ask owns it. It
	// outlives one probe invocation (quotaProbeTimeout) and nothing more, so a
	// prober killed mid-ask frees the question by elapsing rather than by being
	// swept — the same bargain spawnIntentTTL makes.
	quotaProbeLeaseTTL = 60 * time.Second
	// quotaProbeTimeout bounds one ask. A refused agent answers in seconds and
	// an allowed one has a single word to say, so a probe still running at this
	// point is a CLI that is not going to answer.
	quotaProbeTimeout = 45 * time.Second
)

// quotaProbePrompt is the whole question. The answer pop reads is not in the
// reply — it is whether the invocation was refused at all — so the prompt only
// has to be something the agent can finish cheaply without touching the
// checkout.
const quotaProbePrompt = "Reply with the single word READY. Do not read or change any files."

// quotaProbeInterval is how long a park waits before asking the exhausted preset
// again: the span of the window the refusal named, divided into
// quotaProbesPerWindow asks and bounded at both ends.
//
// A refusal that named no window assumes the shortest one, so it starts at the
// dense end where a five-hour window is served. refusedFor is how long the park
// has already been refused, and it is what disproves that assumption: once the
// shortest window has come and gone with the preset still refusing, the window
// is demonstrably longer than the assumption and the interval widens with it,
// toward the bound (ADR-0235).
func quotaProbeInterval(class AgentQuotaWindowClass, refusedFor time.Duration) time.Duration {
	span, named := class.Span()
	if !named {
		span, _ = QuotaWindowFiveHour.Span()
		if refusedFor > span {
			span = refusedFor
		}
	}
	switch interval := span / quotaProbesPerWindow; {
	case interval < minQuotaProbeInterval:
		return minQuotaProbeInterval
	case interval > maxQuotaProbeInterval:
		return maxQuotaProbeInterval
	default:
		return interval
	}
}

// askAgentWillRun asks one exhausted preset whether its window has reopened, and
// is the cheapest question pop can ask an agent. It runs on the store-pure
// attempt path RunRoutineAgentInvocation uses, so there is no Drain row, no
// Captured run and no attempt against the Task retry cap; it takes the read-only
// posture, because the reply is discarded and nothing it could write is wanted;
// and it reads its answer from the proceed verdict rather than from the reply,
// because "will you run?" is answered by being refused or not.
//
// Only an invocation that ran clean and was not refused answers yes. Anything
// else — a quota refusal, an auth failure, a crash, a timeout — leaves the
// cooldown standing: the park's ceiling is still its backstop, and a preset that
// cannot run for some other reason is not one to resume work on.
func askAgentWillRun(d *Deps, runtimePath, preset string) bool {
	invocation, err := ResolveReadOnlyAgentInvocation(preset, "", quotaProbePrompt, runtimePath, AgentOutputAuto)
	if err != nil {
		return false
	}
	raw, outcome, err := runAgentAttempt(d, runtimePath, io.Discard, quotaProbeTimeout, invocation)
	if err != nil || outcome == nil {
		return false
	}
	if outcome.exitCode != 0 || outcome.timedOut || outcome.interrupted || outcome.runErr != nil {
		return false
	}
	return invocation.NormalizeOutput(raw).ProceedVerdict == nil
}

// quotaProbeSession is one park's relationship with the cooldown it is waiting
// on. A guessed cooldown is not a deadline to sleep through: the park asks the
// exhausted preset whether it will run yet, and it is the answer, not the
// ceiling, that ends the wait (ADR-0235).
//
// Whether there is anything to ask is decided once, when the park begins: a
// cooldown carrying an instant the provider stated is waited to exactly, as it
// always was, and a park with no cooldown row behind it keeps the plain
// countdown it always had.
type quotaProbeSession struct {
	d            *Deps
	s            *store.Store
	preset       string
	runtimePath  string
	refusedSince time.Time
	out          *output
	active       bool
}

// newQuotaProbeSession decides whether this park probes at all, from the
// cooldown row as it stands when the park begins.
func newQuotaProbeSession(d *Deps, s *store.Store, w *RecoveryWaiter, out *output) *quotaProbeSession {
	p := &quotaProbeSession{d: d, s: s, out: out}
	if w == nil || s == nil {
		return p
	}
	p.preset, p.runtimePath, p.refusedSince = w.Preset, w.RuntimePath, w.RegisteredAt
	if p.refusedSince.IsZero() {
		p.refusedSince = time.Now().UTC()
	}
	row, err := s.GetAgentCooldown(w.Preset)
	if err != nil || row == nil {
		return p
	}
	p.active = row.StatedUntil.IsZero()
	return p
}

// step is one turn of the probe schedule, run once per recovery poll. It reports
// whether the preset's window is open again — because this park's own ask was
// answered yes, or because another checkout's was and the row is gone.
//
// The row is re-read every turn rather than trusted from park time, since the
// cooldown is machine-global: whichever checkout gets the claim answers the
// question for all of them.
func (p *quotaProbeSession) step(now time.Time) (bool, error) {
	if p == nil || !p.active {
		return false, nil
	}
	row, err := p.s.GetAgentCooldown(p.preset)
	if err != nil {
		return false, err
	}
	switch {
	case row == nil:
		// Some checkout's ask was answered yes and the cooldown is retired. This
		// park's own reset_at was moved with it, so it can resume now.
		p.active = false
		return true, nil
	case !row.StatedUntil.IsZero():
		// A later refusal read a real instant off the wire. There is nothing left
		// to guess about, so the park waits to it.
		p.active = false
		return false, nil
	case !row.ExhaustedUntil.After(now):
		// The ceiling elapsed with every ask refused. The park ends on the
		// backstop, exactly as it did before there were probes.
		p.active = false
		return false, nil
	}

	claimed, err := p.s.ClaimQuotaProbe(p.preset, now, quotaProbeLeaseTTL)
	if err != nil || claimed == nil {
		return false, err
	}
	if askAgentWillRun(p.d, p.runtimePath, p.preset) {
		if err := p.s.ClearAgentQuotaCooldown(p.preset, now); err != nil {
			return false, err
		}
		p.active = false
		p.line(ansiGreen, "✓ %s will run again — quota cooldown cleared by a probe", p.preset)
		return true, nil
	}
	next := now.Add(quotaProbeInterval(AgentQuotaWindowClass(claimed.Class), now.Sub(p.refusedSince)))
	if err := p.s.ScheduleNextQuotaProbe(p.preset, next); err != nil {
		return false, err
	}
	p.line(ansiDim, "⏳ %s still refuses a quota probe; asking again at %s",
		p.preset, next.Local().Format("15:04:05 MST"))
	return false, nil
}

// probing reports whether this park's wait is ending on an ask rather than on
// the clock, which is what the countdown line has to say out loud: the instant
// it would otherwise state is a ceiling pop invented.
func (p *quotaProbeSession) probing() bool {
	return p != nil && p.active
}

// line prints one probe line, tolerating a park that was given no writer.
func (p *quotaProbeSession) line(color, format string, args ...any) {
	if p == nil || p.out == nil {
		return
	}
	p.out.line(color, format, args...)
}
