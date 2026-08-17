package tasks

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
)

// agentRole is what the shared fallback walk needs to know about the role it is
// running: how the role names itself to the operator, where one invocation of it
// is persisted, and what counts as an answer worth stopping on. Verification and
// Code review are the two roles today, and they differ in exactly those three
// things — nothing about how an agent list is walked is theirs.
type agentRole struct {
	// Noun names the role inside a fall-through or skip message ("Verifier
	// agent"), which reads "<preset> …; falling through to the next <Noun>".
	Noun string
	// Gerund opens the once-per-preset heading: "━━ <Gerund> with <agent>".
	Gerund string
	// Persist records as a Captured run one invocation that ended without the
	// role answering — interrupted, quota-paused, or refused — best-effort.
	Persist func(rec *streamRecorder, invocation *AgentInvocation, try int, outcome, reason string, exitCode int)
	// PersistAnswer records an invocation that ran to an ending of its own and
	// so has the agent's normalized output beside it, which is what lets a role
	// file something derived from that output (the Verifier's verdict) on the
	// run. The output having been produced is not the same as it being usable:
	// this is also the path a blank or unparseable answer is filed on.
	PersistAnswer func(rec *streamRecorder, invocation *AgentInvocation, try int, outcome, reason string, exitCode int, answer string)
	// PersistSkipped records an invocation the Effort ladder walked past,
	// naming the model that was refused (ADR-0168).
	PersistSkipped func(rec *streamRecorder, invocation *AgentInvocation, model string, try int, reason string, exitCode int)
	// RetryEligible reports whether an invocation that ended without the role's
	// answer should be retried on the current preset.
	RetryEligible func(outcome *attemptOutcome, raw string) bool
}

// persist, persistAnswer and persistSkipped are how the walk files a run: each
// no-ops when its role left the seam unset, so a role that has nowhere to file
// an invocation costs the walk no branch of its own.
func (r agentRole) persist(rec *streamRecorder, invocation *AgentInvocation, try int, outcome, reason string, exitCode int) {
	if r.Persist != nil {
		r.Persist(rec, invocation, try, outcome, reason, exitCode)
	}
}

func (r agentRole) persistAnswer(rec *streamRecorder, invocation *AgentInvocation, try int, outcome, reason string, exitCode int, answer string) {
	if r.PersistAnswer != nil {
		r.PersistAnswer(rec, invocation, try, outcome, reason, exitCode, answer)
	}
}

func (r agentRole) persistSkipped(rec *streamRecorder, invocation *AgentInvocation, model string, try int, reason string, exitCode int) {
	if r.PersistSkipped != nil {
		r.PersistSkipped(rec, invocation, model, try, reason, exitCode)
	}
}

// agentFallbackWalk is one role's walk over its resolved agent list: the inputs
// that do not change between presets, gathered so the walk reads as a loop over
// the one thing that does.
type agentFallbackWalk struct {
	role            agentRole
	sel             verifierSelection
	runtimePath     string
	prompt          string
	out, errOut     io.Writer
	timeout         time.Duration
	maxTries        int
	retryDelays     []time.Duration
	quotaRetryAfter time.Duration
	cfg             *config.Config
	probeMemo       *agentAvailabilityProbeMemo
}

// agentWalkResult is what one walk over an agent list came back with: the
// answer and who gave it, plus every unavailability verdict collected on the
// way there. Both halves are always populated — a walk that answered on its
// third preset still has the two fall-throughs that got it there, and a caller
// reporting an exhausted list needs them.
type agentWalkResult struct {
	// Answer is the agent's normalized output, empty when no preset answered.
	Answer string
	// Agent is the resolved agent that produced Answer, empty when none did.
	Agent string
	// AnswerRetryEligible reports that the attempt behind Answer was one the role
	// would have retried had a try been left — a timeout, a run error or a
	// non-zero exit. The walk hands the text back either way, because a role that
	// only parses it (the Verifier) reaches the same refusal on its own; a role
	// that would otherwise persist the text verbatim (the Reviewer) reads this to
	// tell a finished document from the prose an attempt died halfway through.
	AnswerRetryEligible bool
	// Unavailable holds each preset the walk could not get an answer out of.
	Unavailable []AgentProceedVerdict
}

// runAgentFallbackWalk walks a role's resolved agent list at the resolved
// effort, retrying each available preset up to the role's cap with Task attempt
// retry delays between invocation failures, then falling through to the next
// agent on quota pause or exhausted retries. Cooling, missing-binary and
// logged-out presets are skipped via the shared Agent unavailability kinds
// (cooldown store, PATH check, availability probe, passive auth detection).
//
// Inside a preset it also walks that preset's Effort tier the way implement
// does: a model-scoped verdict records an Effort model skip and restarts the try
// on the tier's next entry, spending no try, and only an exhausted tier advances
// the agent list (ADR-0168).
//
// It returns the last answer it got (empty when it never got one) beside every
// unavailability verdict it collected on the way, and leaves what an exhausted
// list means to the caller: a role with a typed quota-pause error and a role
// that only has a message to print disagree about that, and about nothing else.
func runAgentFallbackWalk(d *Deps, w agentFallbackWalk) (agentWalkResult, error) {
	var result agentWalkResult
	specs := nonEmptyAgentSpecs(w.sel.Agents, DefaultAgentPreset)
	probeMemo := w.probeMemo
	if probeMemo == nil {
		probeMemo = newAgentAvailabilityProbeMemo()
	}
	// The recorded Effort model skips are shared by every preset in the list and
	// read once, on the first preset that actually reaches its tier — a round
	// where every preset is skipped before invocation never opens the store.
	var skips effortModelSkips
	// The machine-global cooldown store is what a quota pause anywhere on this
	// machine left behind, so the walk reads it before invoking anything: a preset
	// the last hour already proved exhausted is skipped here rather than spending
	// an invocation to be told the same thing again (ADR-0034, ADR-0100).
	cooldowns, err := readAgentCooldowns(d)
	if err != nil {
		return agentWalkResult{}, exitErr(ExitOperational, "%v", err)
	}
	activeCooldowns := activeAgentCooldowns(cooldowns, time.Now())
	resolveSpec := newEffortSpecResolver("", w.sel.Effort, true, w.cfg)
	for i, agentSpec := range specs {
		preset, err := AgentPresetName(agentSpec)
		if err != nil {
			return agentWalkResult{}, exitErr(ExitSetup, "resolve %s: %v", strings.ToLower(w.role.Noun), err)
		}
		if until, cooling := activeCooldowns[preset]; cooling {
			v := NewQuotaPauseVerdict(preset, fmt.Sprintf("agent quota cooldown until %s", until.UTC().Format(time.RFC3339)), until)
			result.Unavailable = append(result.Unavailable, v)
			if i+1 < len(specs) && w.out != nil {
				outputFor(w.out).line(ansiDim, "   %s", v.fallThroughMessage(w.role.Noun))
			}
			continue
		}
		if !agentBinaryAvailable(d, preset) {
			v := NewMissingBinaryVerdict(preset, "binary not found on PATH")
			result.Unavailable = append(result.Unavailable, v)
			if i+1 < len(specs) && w.out != nil {
				outputFor(w.out).line(ansiDim, "   %s", v.fallThroughMessage(w.role.Noun))
			}
			continue
		}
		if v := probeMemo.checkProceedVerdict(d, w.runtimePath, preset); v != nil {
			result.Unavailable = append(result.Unavailable, *v)
			if i+1 < len(specs) && w.out != nil {
				outputFor(w.out).line(ansiDim, "   %s", v.fallThroughMessage(w.role.Noun))
			}
			continue
		}
		if skips == nil {
			loaded, err := loadEffortModelSkips(d, time.Now())
			if err != nil {
				return agentWalkResult{}, exitErr(ExitOperational, "%v", err)
			}
			skips = loaded
		}
		walk := &effortModelWalk{
			d: d, preset: preset, baseSpec: agentSpec, resolve: resolveSpec, skips: skips,
			build: func(spec string) (func(prompt string) (*AgentInvocation, error), error) {
				return func(prompt string) (*AgentInvocation, error) {
					return ResolveAgentInvocationWithMode(spec, "", prompt, w.runtimePath, AgentOutputAuto)
				}, nil
			},
		}
		announced := false

		// try advances by hand because an Effort model skip restarts the try on
		// the tier's next entry rather than spending one: only an answer about
		// the work under judgment charges the cap (ADR-0168).
		for try := 1; try <= w.maxTries; {
			build, exhausted, err := walk.builder()
			if err != nil {
				return agentWalkResult{}, exitErr(ExitSetup, "resolve %s: %v", strings.ToLower(w.role.Noun), err)
			}
			if exhausted != nil {
				result.Unavailable = append(result.Unavailable, *exhausted)
				if i+1 < len(specs) && w.out != nil {
					outputFor(w.out).line(ansiDim, "   %s", exhausted.fallThroughMessage(w.role.Noun))
				}
				break
			}
			invocation, err := build(w.prompt)
			if err != nil {
				return agentWalkResult{}, exitErr(ExitSetup, "resolve %s: %v", strings.ToLower(w.role.Noun), err)
			}
			if !announced && w.out != nil {
				outputFor(w.out).line(ansiBold+ansiCyan, "━━ %s with %s", w.role.Gerund, invocation.RequestedAgent)
			}
			announced = true
			if w.out != nil {
				outputFor(w.out).line(ansiDim, "   Attempt %d/%d · %s", try, w.maxTries, invocation.RequestedAgent)
			}
			raw, outcome, err := runAgentAttempt(d, w.runtimePath, w.out, w.timeout, invocation)
			if err != nil {
				return agentWalkResult{}, exitErr(ExitOperational, "run %s: %v", strings.ToLower(w.role.Noun), err)
			}
			outcomeStr := verifyAttemptOutcome(outcome)
			reason := verifyAttemptReason(outcome)
			exitCode := 0
			if outcome != nil {
				exitCode = outcome.exitCode
			}
			// Interrupted attempts are persisted but yield no answer.
			if outcome != nil && outcome.interrupted {
				w.role.persist(outcome.stream, invocation, try, outcomeStr, reason, exitCode)
				return agentWalkResult{}, exitErr(ExitInterrupted, "interrupted")
			}
			normalized := invocation.NormalizeOutput(raw)
			// Proceed-verdict fall-through: a stopped agent answers nothing, so
			// the walk moves to the next preset (ADR-0153, ADR-0168).
			if normalized.ProceedVerdict != nil {
				v := stampDetectedVerdict(*normalized.ProceedVerdict, preset, invocation.PinnedModel())
				// A model-scoped verdict condemns the entry, not the preset: the
				// run is persisted as the refusal it is, the model is recorded as
				// an Effort model skip, and this try restarts on the tier's next
				// entry. Only an exhausted tier escalates into the preset-scoped
				// handling below (ADR-0168).
				persisted := false
				if v.Scope == ProceedScopeModel {
					stop, err := walk.retire(v)
					if err != nil {
						return agentWalkResult{}, exitErr(ExitOperational, "%v", err)
					}
					// The run is persisted as what the verdict finally
					// condemned: a skip when another tier entry takes over, an
					// unusable agent when the escalation hands the preset on.
					if stop == nil {
						w.role.persistSkipped(outcome.stream, invocation, v.Model, try, v.Reason, exitCode)
						if w.out != nil {
							outputFor(w.out).line(ansiDim, "   %s", v.effortModelSkipMessage(w.role.Noun, walk.nextModel()))
						}
						continue
					}
					w.role.persist(outcome.stream, invocation, try, streamOutcomeAgentUnusable, v.Reason, exitCode)
					v, persisted = *stop, true
				}
				if _, ok := v.TimeHealing(); ok {
					resetAt := agentQuotaResetAt(preset, v.Reason, time.Now())
					v = v.WithResetAt(resetAt)
					until := agentQuotaCooldownUntil(resetAt, time.Now(), w.quotaRetryAfter)
					_ = updateAgentCooldown(d, preset, until)
					if !persisted {
						w.role.persist(outcome.stream, invocation, try, streamOutcomeQuotaPaused, "", exitCode)
					}
					result.Unavailable = append(result.Unavailable, v)
					if w.out != nil {
						outputFor(w.out).line(ansiDim, "   %s", v.fallThroughMessage(w.role.Noun))
					}
					break
				}
				if !persisted {
					w.role.persist(outcome.stream, invocation, try, streamOutcomeAgentUnusable, v.Reason, exitCode)
				}
				result.Unavailable = append(result.Unavailable, v)
				if i+1 < len(specs) && w.out != nil {
					outputFor(w.out).line(ansiDim, "   %s", v.fallThroughMessage(w.role.Noun))
				}
				break
			}
			// A timeout in either role is a retry-eligible failure. Unlike an
			// implement timeout (which restarts instantly from a compact digest),
			// a hang here is more likely a genuine stall than a bloated context,
			// so it falls through to the shared retry path below and waits the
			// Task attempt retry delay. It consumes the cap and, once exhausted,
			// follows the existing next-preset agent fall-through.
			if outcome != nil && outcome.timedOut && w.out != nil {
				outputFor(w.out).line(ansiRed, "   Attempt %d/%d timed out after %s", try, w.maxTries, w.timeout)
			}

			w.role.persistAnswer(outcome.stream, invocation, try, outcomeStr, reason, exitCode, normalized.Output)
			result.Answer = normalized.Output
			result.Agent = invocation.RequestedAgent

			result.AnswerRetryEligible = w.role.RetryEligible(outcome, normalized.Output)
			if !result.AnswerRetryEligible {
				return result, nil
			}
			if try >= w.maxTries {
				break
			}
			delay := attemptRetryDelay(w.retryDelays, try)
			try++
			if delay <= 0 {
				if w.out != nil {
					outputFor(w.out).line(ansiYellow, "↻ Retrying with preserved changes...")
				}
			} else if waitRetryDelay(d, w.out, delay) {
				return agentWalkResult{}, exitErr(ExitInterrupted, "interrupted")
			}
		}
	}
	return result, nil
}
