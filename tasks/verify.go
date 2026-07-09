package tasks

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
)

// VerifyQuotaPause reports that every configured Verifier agent was exhausted
// by Agent quota pause with no verdict rendered (ADR-0100).
type VerifyQuotaPause struct {
	Preset  string
	ResetAt time.Time
	Reason  string
}

type verifyQuotaPauseError struct {
	VerifyQuotaPause
}

func (e *verifyQuotaPauseError) Error() string {
	return fmt.Sprintf("verifier quota paused: %s", e.Preset)
}

func newVerifyQuotaPause(pause VerifyQuotaPause) error {
	return &verifyQuotaPauseError{VerifyQuotaPause: pause}
}

// AsVerifyQuotaPause reports whether err is a Verifier quota pause.
func AsVerifyQuotaPause(err error) (*VerifyQuotaPause, bool) {
	var pause *verifyQuotaPauseError
	if errors.As(err, &pause) {
		return &pause.VerifyQuotaPause, true
	}
	return nil, false
}

func earliestVerifyQuotaPause(pauses []VerifyQuotaPause) VerifyQuotaPause {
	var best *VerifyQuotaPause
	for i := range pauses {
		p := &pauses[i]
		if best == nil {
			best = p
			continue
		}
		if p.ResetAt.IsZero() {
			continue
		}
		if best.ResetAt.IsZero() || p.ResetAt.Before(best.ResetAt) {
			best = p
		}
	}
	if best == nil {
		return VerifyQuotaPause{}
	}
	return *best
}

// Verdict is the three-way Verify verdict (ADR-0086): the cached judgment an
// independent Verifier agent renders over a Task set's completed AFK work.
type Verdict string

const (
	// VerdictPass — every acceptance criterion is met; the set may advance.
	VerdictPass Verdict = "PASS"
	// VerdictFixable — the work falls short, but the findings are things an agent
	// can resolve (a later slice spawns a Remediation task from them).
	VerdictFixable Verdict = "FIXABLE"
	// VerdictNeedsHuman — only a human can resolve the findings; the set parks.
	// A malformed or absent Verifier response also resolves here, so an
	// unparseable result never silently reads as PASS.
	VerdictNeedsHuman Verdict = "NEEDS-HUMAN"
)

// emptyTreeSHA is git's well-known hash of the empty tree, used as the diff base
// when the set's earliest commit is a root commit with no parent.
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// DefaultVerifyEffort is the model-strength tier the Verifier runs at when
// neither a CLI flag, a per-set override, nor [tasks.verify].effort names one
// (ADR-0086): verification defaults to the strongest tier.
const DefaultVerifyEffort = "heavy"

// VerifyOptions configures a `pop tasks verify <set>` run.
type VerifyOptions struct {
	ResolveInput ResolveInput
	// TaskSetID is the bare Task-set identifier to verify.
	TaskSetID string
	// Agents is the ordered CLI Verifier fallback list (`--agent`, repeatable).
	// Empty ⇒ resolution falls through to the per-set override, then
	// [tasks.verify].agents, then [tasks.implement].agents.
	Agents []string
	// Effort is the CLI Verifier effort override (`--effort`). Empty ⇒ resolution
	// falls through to the per-set override, then config, then DefaultVerifyEffort.
	Effort string
	// Timeout bounds the single Verifier attempt. Zero uses DefaultAttemptTimeout.
	Timeout time.Duration
	// Output receives the live agent stream and the rendered verdict.
	Output io.Writer
}

// VerifyResult is the outcome of one verification, returned after the verdict is
// persisted.
type VerifyResult struct {
	SetID    string
	WorkSHA  string
	Verdict  Verdict
	Findings string
}

// verifyCoreOptions carries the already-resolved inputs to the core verify
// routine, so tests can drive it without real path-resolution git calls. The
// runVerifier seam replaces the real agent spawn in tests (no real agent calls).
type verifyCoreOptions struct {
	Repo        string
	DefPath     string
	RuntimePath string
	SetID       string
	// Agents and Effort are the CLI-level Verifier overrides (highest
	// precedence). Empty ⇒ resolution falls through to the per-set manifest
	// override, then [tasks.verify], then [tasks.implement].agents / DefaultVerifyEffort.
	Agents      []string
	Effort      string
	Timeout     time.Duration
	Output      io.Writer
	runVerifier func(prompt string) (string, error)
}

// VerifyTaskSet runs the Verifier over a set using default dependencies.
func VerifyTaskSet(opts VerifyOptions) (*VerifyResult, error) {
	return VerifyTaskSetWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// VerifyTaskSetWith is the tracer bullet for Agent verification (ADR-0086): it
// resolves the set, hands its acceptance criteria, task bodies, and accumulated
// work diff to an independent Verifier agent, parses a three-way verdict, writes
// it to the Drain store keyed by (set, work SHA), and prints it. It always
// re-runs the Verifier and overwrites the verdict for the current SHA (force);
// it never reads a cached verdict.
func VerifyTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts VerifyOptions) (*VerifyResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, err
	}
	runtimePath, err := ResolveRuntimePathWith(d, resolved.ProjectPath, opts.ResolveInput.RuntimeOverride)
	if err != nil {
		return nil, err
	}
	id, err := ResolveRepositoryIdentity(d, runtimePath)
	if err != nil {
		return nil, err
	}
	cfg, _ := loadConfig(config.DefaultConfigPath())
	return verifyResolvedSet(d, cfg, verifyCoreOptions{
		Repo:        id.CommonDir,
		DefPath:     resolved.DefinitionPath,
		RuntimePath: runtimePath,
		SetID:       strings.TrimSpace(opts.TaskSetID),
		Agents:      opts.Agents,
		Effort:      opts.Effort,
		Timeout:     opts.Timeout,
		Output:      opts.Output,
	})
}

// verifyResolvedSet is the resolved-path core of `pop tasks verify`: it loads
// the set, runs the Verifier (force — it never reads the SHA cache), persists
// the verdict, and prints it. All external effects go through injectable seams
// (d.Git for SHA and diff, runVerifier for the agent, the store for
// persistence).
func verifyResolvedSet(d *Deps, cfg *config.Config, opts verifyCoreOptions) (*VerifyResult, error) {
	m, err := loadVerifiableManifest(d, opts)
	if err != nil {
		return nil, err
	}
	workSHA := verifyWorkSHA(d, opts.RuntimePath)
	v, err := runAndStoreVerdict(d, cfg, opts, m, workSHA)
	if err != nil {
		return nil, err
	}
	verdict := Verdict(v.Verdict)
	printVerdict(opts.Output, opts.SetID, workSHA, verdict, v.Findings, opts.Agents, opts.Effort)
	return &VerifyResult{SetID: opts.SetID, WorkSHA: workSHA, Verdict: verdict, Findings: v.Findings}, nil
}

// reverifyGateContext carries the resolved Verifier inputs a HITL gate needs to
// force a re-verify (ADR-0086/ADR-0012). It mirrors the drain's Verifier
// overrides so the gate re-check honours the same agent/effort precedence, and
// keeps the test-only runVerifier seam so gate tests never spawn a real agent.
type reverifyGateContext struct {
	cfg         *config.Config
	agents      []string
	effort      string
	timeout     time.Duration
	runVerifier func(prompt string) (string, error)
}

// reverifyAtGate force-runs the Verifier against the set's current work SHA
// (bypassing the SHA cache), stores the fresh verdict, and prints it. It is the
// gate's re-verify path (ADR-0012) and reuses the exact force machinery behind
// `pop tasks verify` — runAndStoreVerdict always re-invokes the Verifier and
// overwrites the verdict at the current SHA, so a human who made inline changes
// re-checks the work without kicking off a fresh drain.
func reverifyAtGate(d *Deps, rv *reverifyGateContext, out io.Writer, repo, runtimePath, setID string, m *Manifest) error {
	opts := verifyCoreOptions{
		Repo:        repo,
		RuntimePath: runtimePath,
		SetID:       setID,
		Agents:      rv.agents,
		Effort:      rv.effort,
		Timeout:     rv.timeout,
		Output:      out,
		runVerifier: rv.runVerifier,
	}
	workSHA := verifyWorkSHA(d, runtimePath)
	v, err := runAndStoreVerdict(d, rv.cfg, opts, m, workSHA)
	if err != nil {
		return err
	}
	printVerdict(out, setID, workSHA, Verdict(v.Verdict), v.Findings, rv.agents, rv.effort)
	return nil
}

// loadVerifiableManifest resolves and validates the set named in opts from its
// definition path, returning a hard error for an absent identifier, an unknown
// set, or a malformed manifest.
func loadVerifiableManifest(d *Deps, opts verifyCoreOptions) (*Manifest, error) {
	if opts.SetID == "" {
		return nil, exitErr(ExitSetup, "a task set identifier is required")
	}
	disc, err := DiscoverWith(d, opts.DefPath)
	if err != nil {
		return nil, exitErr(ExitOperational, "discover task sets: %v", err)
	}
	manifestPath, ok := disc.Manifests[opts.SetID]
	if !ok {
		return nil, exitErr(ExitSetup, "unknown task set %q", opts.SetID)
	}
	m := LoadManifest(d, opts.SetID, manifestPath)
	if !m.Valid {
		return nil, exitErr(ExitSetup, "task set %q is malformed: %s", opts.SetID, MalformedSummary(m))
	}
	return m, nil
}

// runAndStoreVerdict runs the Verifier once for the resolved set at workSHA,
// parses the three-way verdict, upserts it into the Drain store keyed by (repo,
// set, work SHA), and returns the stored record. It always invokes the Verifier
// (the force path shared by `pop tasks verify` and, on a cache miss, the drain).
func runAndStoreVerdict(d *Deps, cfg *config.Config, opts verifyCoreOptions, m *Manifest, workSHA string) (*store.VerifyVerdict, error) {
	diff := verifyWorkDiff(d, opts.RuntimePath, opts.SetID)
	prompt := buildVerifierPrompt(d, m, workSHA, diff)

	run := opts.runVerifier
	if run == nil {
		sel := resolveVerifier(opts.Agents, opts.Effort, m, cfg)
		run = func(prompt string) (string, error) {
			return runConfiguredVerifier(d, cfg, sel, m.Dir, opts.SetID, workSHA, opts.RuntimePath, prompt, opts.Output, opts.Output, opts.Timeout)
		}
	}
	raw, err := run(prompt)
	if err != nil {
		if _, ok := AsVerifyQuotaPause(err); ok {
			return nil, err
		}
		return nil, err
	}

	verdict, findings := ParseVerdict(raw)
	v := store.VerifyVerdict{
		Repo:       opts.Repo,
		SetID:      opts.SetID,
		WorkSHA:    workSHA,
		Verdict:    string(verdict),
		Findings:   findings,
		Scope:      afkTaskCount(m),
		ComputedAt: time.Now().UTC(),
	}

	s, err := openDrainStore(d)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	if err := s.PutVerifyVerdict(v); err != nil {
		return nil, exitErr(ExitOperational, "record verify verdict: %v", err)
	}
	return &v, nil
}

// ensureVerifyVerdict returns the Verify verdict for the set at workSHA,
// computing and persisting one only when none is cached at that SHA. The
// cache-first read is what keeps a re-drain from re-invoking the Verifier: once
// a verdict is stored at the current work SHA, reaching it is a stopping point,
// so a drain with unchanged work reuses the verdict instead of looping
// (ADR-0086). When no verdict exists at the current SHA, the most recent PASS
// verdict for the set immunizes the terminal status against later commits, so
// that stale PASS is returned without re-invoking the Verifier (ADR-0096). Only
// when no PASS verdict exists in the episode does the Verifier actually run.
func ensureVerifyVerdict(d *Deps, cfg *config.Config, opts verifyCoreOptions, m *Manifest, workSHA string) (*store.VerifyVerdict, error) {
	if s, ok, err := openDrainStoreIfExists(d); err == nil && ok {
		defer func() { _ = s.Close() }()
		if cached, gerr := s.GetVerifyVerdict(opts.Repo, opts.SetID, workSHA); gerr == nil && cached != nil {
			return cached, nil
		}
		if pass, gerr := s.GetLatestPassVerifyVerdict(opts.Repo, opts.SetID); gerr == nil && pass != nil {
			// A PASS certifies the set as verified *as scoped* (ADR-0101). When the
			// set has since grown a new AFK task — a direct manifest edit during a
			// HITL assistance session, drained back to terminal at a new work SHA —
			// the stale PASS no longer covers the enlarged scope. End the episode so
			// the Verifier re-fires against the added work, symmetric with the
			// remediation add-work path. A scope that only equals or trails the PASS
			// (an incidental commit that moved the SHA without adding a task) still
			// coasts on the immunizing PASS (ADR-0096). A zero recorded scope means
			// unknown (legacy verdict) and never triggers the growth check.
			if pass.Scope > 0 && afkTaskCount(m) > pass.Scope {
				_ = s.InvalidateVerifyVerdicts(opts.Repo, opts.SetID)
			} else {
				return pass, nil
			}
		}
	}
	return runAndStoreVerdict(d, cfg, opts, m, workSHA)
}

// afkTaskCount returns the number of AFK-typed tasks in the set — the scope a
// Verify verdict certifies (ADR-0101). HITL tasks are excluded: the Verifier
// judges only AFK work, and adding a HITL sign-off does not enlarge what
// "verified end-to-end" means for the agent-completed work.
func afkTaskCount(m *Manifest) int {
	if m == nil {
		return 0
	}
	n := 0
	for _, t := range m.Tasks {
		if t.Type == "AFK" {
			n++
		}
	}
	return n
}

// drainVerifyPhase runs the pre-approval Verifier phase for a drain that has
// exhausted a set's AFK work (ADR-0086), returning the verdict-derived effective
// status the drain should act on plus the verdict behind it. It gates only the
// terminal zone — a manifestStatus of DONE (pure-AFK) or AWAITING-APPROVAL
// (terminal HITL open); any other status is returned unchanged with a nil
// verdict, so a READY/BLOCKED/FAILED set is never verified. It is cache-first:
// a verdict already stored at the current work SHA is reused and the Verifier is
// not re-invoked. A PASS verdict (at the current SHA or an older immunizing
// SHA) lets the terminal status stand; a FIXABLE or NEEDS-HUMAN verdict at the
// current SHA resolves to VERIFY-FAILED, parking the set (automatic remediation
// from a FIXABLE verdict lands in a later slice). The caller has already
// checked that verification is enabled and the set has not opted out.
func drainVerifyPhase(d *Deps, cfg *config.Config, opts verifyCoreOptions, m *Manifest, manifestStatus TaskSetStatus) (TaskSetStatus, *store.VerifyVerdict, error) {
	if manifestStatus != StatusDone && manifestStatus != StatusAwaitingApproval {
		return manifestStatus, nil, nil
	}
	workSHA := verifyWorkSHA(d, opts.RuntimePath)
	v, err := ensureVerifyVerdict(d, cfg, opts, m, workSHA)
	if err != nil {
		return manifestStatus, nil, err
	}
	// Map the single cache-first verdict into the two Verified status resolution
	// slots. This is the drain's run-decision leftover: ensureVerifyVerdict
	// returned one verdict (current, stale immunizing PASS, or freshly run), so
	// which slot it fills is decided here by SHA, not in the shared resolver.
	var currentAtSHA, latestPass *store.VerifyVerdict
	if v.WorkSHA == workSHA {
		currentAtSHA = v
	} else if Verdict(v.Verdict) == VerdictPass {
		// Stale immunizing PASS: feed it as the episode's latest PASS.
		latestPass = v
	} else {
		// Defensive: ensureVerifyVerdict only returns a stale verdict when it
		// is a PASS, so this preserves any non-PASS failure signal.
		currentAtSHA = v
	}
	printVerdict(opts.Output, opts.SetID, workSHA, Verdict(v.Verdict), v.Findings, opts.Agents, opts.Effort)
	// verifiedAtSHA is discarded: the drain does not yet surface it (deferred).
	status, _ := ResolveVerifiedStatus(m, workSHA, currentAtSHA, latestPass)
	return status, v, nil
}

// verifierSelection is the resolved Verifier agent fallback list and effort tier
// after the precedence chain has been applied.
type verifierSelection struct {
	Agents []string
	Effort string
}

// verifyAttemptOutcome maps an attempt outcome to the persisted stream outcome
// for a Verifier run.
func verifyAttemptOutcome(outcome *attemptOutcome) string {
	if outcome == nil {
		return streamOutcomeFailed
	}
	switch {
	case outcome.interrupted:
		return streamOutcomeInterrupted
	case outcome.timedOut:
		return streamOutcomeTimedOut
	case outcome.runErr != nil || outcome.exitCode != 0:
		return streamOutcomeFailed
	default:
		return streamOutcomeCompleted
	}
}

// verdictCleanlyParsed reports whether raw contains a recognized VERDICT line.
// Unparseable or empty responses are retry-eligible; a clean PASS, FIXABLE, or
// NEEDS-HUMAN parse is not.
func verdictCleanlyParsed(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	for _, line := range strings.Split(trimmed, "\n") {
		value, ok := verdictLabelValue(line)
		if !ok {
			continue
		}
		if _, ok := canonicalVerdict(value); ok {
			return true
		}
	}
	return false
}

// verifyAttemptRetryEligible reports whether a verifier invocation failure should
// be retried on the current preset. Timeout stops the whole verify run;
// quota pause is handled by the caller; clean verdict parses are not retried.
func verifyAttemptRetryEligible(outcome *attemptOutcome, raw string) bool {
	if outcome != nil && outcome.timedOut {
		return false
	}
	if verdictCleanlyParsed(raw) {
		return false
	}
	if outcome != nil && (outcome.runErr != nil || outcome.exitCode != 0) {
		return true
	}
	return strings.TrimSpace(raw) == "" || !verdictCleanlyParsed(raw)
}

// verifyAttemptReason returns a human-facing reason for a failed verifier
// attempt, or an empty string for non-failure terminal outcomes.
func verifyAttemptReason(outcome *attemptOutcome) string {
	if outcome == nil {
		return ""
	}
	if outcome.runErr != nil {
		return fmt.Sprintf("agent execution error: %v", outcome.runErr)
	}
	if outcome.exitCode != 0 {
		return fmt.Sprintf("agent exited with status %d", outcome.exitCode)
	}
	return ""
}

// resolveVerifier applies the Verifier precedence chain (ADR-0086), highest
// first: CLI flags (cliAgents / cliEffort) → the per-set manifest `verifier`
// override → [tasks.verify] config → [tasks.implement].agents / DefaultVerifyEffort.
// Agents and effort resolve independently, so a CLI effort can steer a
// config-listed agent list, and vice versa. When no layer names an agent list it
// falls back to [tasks.implement].agents (ResolveDefaultAgentPresets), so the
// Verifier always has at least one agent to walk.
func resolveVerifier(cliAgents []string, cliEffort string, m *Manifest, cfg *config.Config) verifierSelection {
	agents := nonEmptyStrings(cliAgents)
	effort := strings.TrimSpace(cliEffort)

	if over := m.VerifierOverride(); over != nil {
		if len(agents) == 0 {
			agents = nonEmptyStrings(over.Agents)
		}
		if effort == "" {
			effort = strings.TrimSpace(over.Effort)
		}
	}
	if cfg != nil && cfg.Task != nil && cfg.Task.Verify != nil {
		v := cfg.Task.Verify
		if len(agents) == 0 {
			agents = nonEmptyStrings(v.Agents)
		}
		if effort == "" {
			effort = strings.TrimSpace(v.Effort)
		}
	}
	if len(agents) == 0 {
		agents = ResolveDefaultAgentPresets(nil, "", false, cfg)
	}
	if effort == "" {
		effort = DefaultVerifyEffort
	}
	return verifierSelection{Agents: agents, Effort: effort}
}

// nonEmptyStrings returns the non-blank entries of specs, preserving order.
func nonEmptyStrings(specs []string) []string {
	var out []string
	for _, s := range specs {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// runConfiguredVerifier walks the resolved Verifier agent list at the resolved
// effort, retrying each available preset up to the verify cap with Task attempt
// retry delays between invocation failures, then falling through to the next
// agent on quota pause or exhausted retries. A timeout stops immediately with
// no further attempts. A missing binary skips to the next agent. An empty
// result or an exhausted list yields empty output, which ParseVerdict turns
// into a NEEDS-HUMAN the human is told about.
//
// Every structured adapter-mode invocation is persisted as a Captured run pair
// under <task-set>/streams/runs/. Quota-paused fall-through attempts are
// persisted without a verdict; the parsed invocation is persisted with its
// verdict. Persistence is best-effort and never fails the verify command.
func runConfiguredVerifier(d *Deps, cfg *config.Config, sel verifierSelection, taskSetDir, setID, workSHA, runtimePath, prompt string, out, errOut io.Writer, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = DefaultAttemptTimeout
	}
	maxTries, err := resolveVerifyMaxTries(cfg)
	if err != nil {
		return "", exitErr(ExitSetup, "%v", err)
	}
	retryDelays, err := resolveAttemptRetryDelays(cfg)
	if err != nil {
		return "", exitErr(ExitSetup, "%v", err)
	}

	quotaRetryAfter, err := resolveAgentQuotaRetryAfter(cfg)
	if err != nil {
		return "", exitErr(ExitSetup, "%v", err)
	}

	var (
		lastRaw     string
		quotaPauses []VerifyQuotaPause
	)
	for _, agentSpec := range nonEmptyAgentSpecs(sel.Agents, DefaultAgentPreset) {
		preset, err := AgentPresetName(agentSpec)
		if err != nil {
			return "", exitErr(ExitSetup, "resolve verifier agent: %v", err)
		}
		// Missing-binary fall-through: an agent whose binary is not on PATH is
		// skipped so the next configured agent gets a turn.
		if !verifierBinaryAvailable(d, preset) {
			if out != nil {
				outputFor(out).line(ansiDim, "   Verifier agent %s unavailable (binary not found); trying next", preset)
			}
			continue
		}
		spec := resolveTaskAgentSpecForEffortWithConfig(agentSpec, sel.Effort, true, cfg)
		invocation, err := ResolveAgentInvocationWithMode(spec, "", prompt, runtimePath, AgentOutputAuto)
		if err != nil {
			return "", exitErr(ExitSetup, "resolve verifier agent: %v", err)
		}
		if out != nil {
			outputFor(out).line(ansiBold+ansiCyan, "━━ Verifying with %s", invocation.RequestedAgent)
		}

		for try := 1; try <= maxTries; try++ {
			if out != nil {
				outputFor(out).line(ansiDim, "   Attempt %d/%d · %s", try, maxTries, invocation.RequestedAgent)
			}
			raw, outcome, err := runAgentAttempt(d, runtimePath, out, timeout, invocation)
			if err != nil {
				return "", exitErr(ExitOperational, "run verifier: %v", err)
			}
			outcomeStr := verifyAttemptOutcome(outcome)
			reason := verifyAttemptReason(outcome)
			exitCode := 0
			if outcome != nil {
				exitCode = outcome.exitCode
			}
			// Interrupted attempts are persisted but yield no verdict.
			if outcome != nil && outcome.interrupted {
				_ = persistVerifyRun(d, errOut, taskSetDir, setID, workSHA, outcome.stream, invocation.AgentPreset(), invocation.RequestedAgent, try, outcomeStr, reason, exitCode, "")
				return "", exitErr(ExitInterrupted, "interrupted")
			}
			normalized := invocation.NormalizeOutput(raw)
			// Quota fall-through: a paused agent renders no verdict, so try the next.
			if normalized.QuotaPause != nil {
				_ = persistVerifyRun(d, errOut, taskSetDir, setID, workSHA, outcome.stream, invocation.AgentPreset(), invocation.RequestedAgent, try, streamOutcomeQuotaPaused, "", exitCode, "")
				pause := *normalized.QuotaPause
				resetAt := agentQuotaResetAt(preset, pause.Reason, time.Now())
				until := agentQuotaCooldownUntil(resetAt, time.Now(), quotaRetryAfter)
				_ = updateAgentCooldown(d, preset, until)
				quotaPauses = append(quotaPauses, VerifyQuotaPause{
					Preset:  preset,
					ResetAt: resetAt,
					Reason:  pause.Reason,
				})
				if out != nil {
					outputFor(out).line(ansiDim, "   Verifier agent %s quota-paused; trying next", preset)
				}
				break
			}
			// Timeout stops immediately with no further attempts on any preset.
			if outcome != nil && outcome.timedOut {
				verdict, _ := ParseVerdict(normalized.Output)
				_ = persistVerifyRun(d, errOut, taskSetDir, setID, workSHA, outcome.stream, invocation.AgentPreset(), invocation.RequestedAgent, try, outcomeStr, reason, exitCode, string(verdict))
				return normalized.Output, nil
			}

			verdict, _ := ParseVerdict(normalized.Output)
			_ = persistVerifyRun(d, errOut, taskSetDir, setID, workSHA, outcome.stream, invocation.AgentPreset(), invocation.RequestedAgent, try, outcomeStr, reason, exitCode, string(verdict))
			lastRaw = normalized.Output

			if !verifyAttemptRetryEligible(outcome, normalized.Output) {
				return normalized.Output, nil
			}
			if try < maxTries {
				delay := attemptRetryDelay(retryDelays, try)
				if delay <= 0 {
					if out != nil {
						outputFor(out).line(ansiYellow, "↻ Retrying with preserved changes...")
					}
				} else if waitRetryDelay(out, delay) {
					return "", exitErr(ExitInterrupted, "interrupted")
				}
				continue
			}
		}
	}
	// Every configured agent was unavailable, quota-paused, or exhausted.
	if len(quotaPauses) > 0 && strings.TrimSpace(lastRaw) == "" {
		return "", newVerifyQuotaPause(earliestVerifyQuotaPause(quotaPauses))
	}
	// Return the last invocation output when present so ParseVerdict can surface why.
	return lastRaw, nil
}

// verifierBinaryAvailable reports whether the agent preset's binary is resolvable
// on PATH, so a missing agent can be skipped before it is invoked.
func verifierBinaryAvailable(d *Deps, preset string) bool {
	adapter, err := ResolveAgentAdapter(preset)
	if err != nil {
		return false
	}
	lookPath := exec.LookPath
	if d != nil && d.LookPath != nil {
		lookPath = d.LookPath
	}
	_, err = lookPath(AgentBinary(adapter))
	return err == nil
}

// verifyWorkSHA reads the runtime checkout's HEAD — the set's current work SHA.
// A checkout with no commits (or not a repo) has no work SHA; the verdict then
// keys on the empty string, which is fine for a set with nothing committed yet.
func verifyWorkSHA(d *Deps, runtimePath string) string {
	out, err := d.Git.CommandInDir(runtimePath, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// invalidateVerifyVerdicts ends the cached verification episode for (repo, set)
// by deleting every stored Verify verdict. It is best-effort: an unresolvable
// repo, missing store, or store error is silently ignored so that reopen and
// remediation always succeed (ADR-0096).
func invalidateVerifyVerdicts(d *Deps, repo, setID string) {
	if d == nil || repo == "" || setID == "" {
		return
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return
	}
	defer func() { _ = s.Close() }()
	_ = s.InvalidateVerifyVerdicts(repo, setID)
}

// verifyWorkDiff returns the accumulated diff of the set's committed work. The
// drain commits every task as a `tasks(<slug>): <id>` commit, so the set's work
// is the range from the parent of its earliest such commit to HEAD. Absent set
// commits (nothing drained yet) yields an empty diff — the Verifier still judges
// the criteria, just against no changes. Diff computation is best-effort: any git
// failure yields an empty diff rather than aborting the verification.
func verifyWorkDiff(d *Deps, runtimePath, setID string) string {
	prefix := commitSubjectPrefix(setID)
	out, err := d.Git.CommandInDir(runtimePath, "log", "--format=%H", "--fixed-strings", "--grep", prefix, "HEAD")
	if err != nil {
		return ""
	}
	hashes := strings.Fields(strings.TrimSpace(out))
	if len(hashes) == 0 {
		return ""
	}
	earliest := hashes[len(hashes)-1]
	if diff, err := d.Git.CommandInDir(runtimePath, "diff", earliest+"^..HEAD"); err == nil {
		return strings.TrimSpace(diff)
	}
	// The earliest set commit is a root commit (no parent); diff from the empty tree.
	diff, err := d.Git.CommandInDir(runtimePath, "diff", emptyTreeSHA+".."+earliest)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(diff)
}

// commitSubjectPrefix is the leading text every implementation commit for a set
// shares (see CommitSubject), used to find the set's commits.
func commitSubjectPrefix(taskSetID string) string {
	return fmt.Sprintf("tasks(%s):", taskSetSlug(taskSetID))
}

// prdFileName is the optional co-located enrichment file a Verifier prompt
// includes when present (see buildVerifierPrompt).
const prdFileName = "prd.md"

// buildVerifierPrompt assembles the Verifier's input: the authoritative
// acceptance criteria and task bodies for the set's `done` AFK tasks only, plus
// the accumulated work diff at the current SHA, and the exact response format
// the parser expects. Open/not-`done` AFK tasks and HITL tasks of any status are
// excluded (ADR-0102): an agent cannot judge a human sign-off, and a not-yet-run
// task is not an unmet criterion — emitting either made a still-open terminal
// HITL gate read as a failing criterion, deadlocking the drain before the gate
// that would clear it. When the set folder has a co-located prd.md, its content
// is folded in as optional context that sharpens judgment — it never replaces
// the acceptance criteria as the authoritative contract, and its absence is not
// an error.
func buildVerifierPrompt(d *Deps, m *Manifest, workSHA, diff string) string {
	var b strings.Builder
	b.WriteString("You are an independent Verifier. A separate agent has already implemented this Task set; ")
	b.WriteString("your job is to confirm reality, not to trust its self-report.\n\n")
	b.WriteString(fmt.Sprintf("Task set: %s\n", m.Stem))
	if workSHA != "" {
		b.WriteString(fmt.Sprintf("Work SHA: %s\n", workSHA))
	}
	b.WriteString("\nThe checkboxes under each task's \"## Acceptance criteria\" heading are authoritative. ")
	b.WriteString("Judge the done AFK work below against them using the accumulated work diff. ")
	b.WriteString("Tasks awaiting a human sign-off, and tasks not yet done, are deliberately omitted — do not treat their absence as a failure.\n\n")

	if prd, ok := readPRD(d, m); ok {
		b.WriteString("## PRD (context only — the acceptance criteria above remain authoritative)\n")
		b.WriteString(prd)
		b.WriteString("\n\n")
	}

	b.WriteString("## Tasks\n")
	for _, task := range m.Tasks {
		// Scope to done AFK work only (ADR-0102): open/not-done AFK tasks and
		// HITL tasks of any status are not judged criteria.
		if task.Type != "AFK" || task.Status != "done" {
			continue
		}
		b.WriteString(fmt.Sprintf("\n### %s [%s] (%s): %s\n", task.ID, task.Type, task.Status, task.Title))
		body, err := d.FS.ReadFile(filepath.Join(m.Dir, task.File))
		if err != nil {
			b.WriteString(fmt.Sprintf("(could not read task body: %v)\n", err))
			continue
		}
		b.WriteString(strings.TrimSpace(string(body)))
		b.WriteString("\n")
	}

	b.WriteString("\n## Accumulated work diff")
	if workSHA != "" {
		b.WriteString(fmt.Sprintf(" (at %s)", workSHA))
	}
	b.WriteString("\n")
	if strings.TrimSpace(diff) == "" {
		b.WriteString("(no committed changes for this set)\n")
	} else {
		b.WriteString("```diff\n")
		b.WriteString(diff)
		b.WriteString("\n```\n")
	}

	b.WriteString("\n## Respond in exactly this format\n")
	b.WriteString("On the first line, one of:\n")
	b.WriteString("VERDICT: PASS\n")
	b.WriteString("VERDICT: FIXABLE\n")
	b.WriteString("VERDICT: NEEDS-HUMAN\n")
	b.WriteString("Then, on the following lines:\n")
	b.WriteString("FINDINGS: <what fails a criterion and why — leave empty for PASS>\n\n")
	b.WriteString("PASS = every acceptance criterion is met. ")
	b.WriteString("FIXABLE = criteria are unmet but an agent could resolve the findings. ")
	b.WriteString("NEEDS-HUMAN = the findings need a human decision.\n")
	return b.String()
}

// readPRD returns the trimmed content of the set folder's co-located prd.md
// and true when it exists and is non-blank. Any read failure (most commonly:
// the file does not exist) is treated as "no PRD" rather than an error — a
// missing prd.md must never fail verification.
func readPRD(d *Deps, m *Manifest) (string, bool) {
	body, err := d.FS.ReadFile(filepath.Join(m.Dir, prdFileName))
	if err != nil {
		return "", false
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

// FormatVerifyCommand builds a copy-pasteable `pop tasks verify` invocation.
// Only explicit CLI overrides (agents, effort) are included; config and manifest
// defaults are picked up automatically on re-run.
func FormatVerifyCommand(setID string, cliAgents []string, cliEffort string) string {
	parts := []string{"pop", "tasks", "verify", shellQuote(setID)}
	for _, agent := range nonEmptyStrings(cliAgents) {
		parts = append(parts, "--agent", shellQuote(agent))
	}
	if effort := strings.TrimSpace(cliEffort); effort != "" {
		parts = append(parts, "--effort", shellQuote(effort))
	}
	return strings.Join(parts, " ")
}

// printVerdict renders the verdict and findings to the operator.
func printVerdict(w io.Writer, setID, workSHA string, verdict Verdict, findings string, cliAgents []string, cliEffort string) {
	if w == nil {
		return
	}
	out := outputFor(w)
	out.line(ansiBold, "━━ Verify verdict for %s", setID)
	if workSHA != "" {
		out.line(ansiDim, "   Work SHA: %s", ShortSHA(workSHA))
	}
	out.line(verdictStyle(verdict), "   Verdict:  %s", string(verdict))
	if strings.TrimSpace(findings) != "" {
		out.line(ansiBold, "   Findings:")
		for _, line := range strings.Split(strings.TrimRight(findings, "\n"), "\n") {
			fmt.Fprintf(out, "     %s\n", line)
		}
	}
	if verdict != VerdictPass {
		out.line(ansiDim, "   Re-run: %s", FormatVerifyCommand(setID, cliAgents, cliEffort))
	}
}

func verdictStyle(v Verdict) string {
	switch v {
	case VerdictPass:
		return ansiGreen
	case VerdictFixable:
		return ansiYellow
	default:
		return ansiRed
	}
}

func ShortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// ParseVerdict extracts the three-way verdict and findings from a Verifier's raw
// response. It looks for a `VERDICT:` line naming PASS, FIXABLE, or NEEDS-HUMAN
// (tolerating markdown decoration and spelling variants) and takes the findings
// from a `FINDINGS:` line or, failing that, everything after the verdict line. A
// missing verdict, an unrecognized token, or an empty response all resolve to
// NEEDS-HUMAN with findings that tell the human what happened — so a malformed
// or absent response never reads as PASS.
func ParseVerdict(raw string) (Verdict, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return VerdictNeedsHuman, unparsedFindings(raw)
	}
	lines := strings.Split(trimmed, "\n")
	for i, line := range lines {
		value, ok := verdictLabelValue(line)
		if !ok {
			continue
		}
		v, ok := canonicalVerdict(value)
		if !ok {
			return VerdictNeedsHuman, unparsedFindings(raw)
		}
		return v, extractFindings(lines, i)
	}
	return VerdictNeedsHuman, unparsedFindings(raw)
}

// verdictLabelValue reports whether a line is the `VERDICT:` label line and, if
// so, returns the text after the label. Leading markdown decoration (bullets,
// headings, bold) is tolerated.
func verdictLabelValue(line string) (string, bool) {
	stripped := stripMarkdown(line)
	if !strings.HasPrefix(strings.ToUpper(stripped), "VERDICT") {
		return "", false
	}
	rest := stripped[len("VERDICT"):]
	rest = strings.TrimLeft(rest, "*: \t")
	return rest, true
}

// canonicalVerdict maps a verdict token to its canonical Verdict, tolerating
// trailing text and spelling variants (NEEDS_HUMAN, NEEDS HUMAN, …).
func canonicalVerdict(s string) (Verdict, bool) {
	up := strings.ToUpper(strings.TrimSpace(stripMarkdown(s)))
	switch {
	case strings.HasPrefix(up, "PASS"):
		return VerdictPass, true
	case strings.HasPrefix(up, "FIXABLE"):
		return VerdictFixable, true
	case strings.HasPrefix(up, "NEEDS") && strings.Contains(up, "HUMAN"):
		return VerdictNeedsHuman, true
	}
	return "", false
}

// extractFindings returns the findings text: the value of a `FINDINGS:` label
// (and everything after it) when present, otherwise every line after the verdict.
func extractFindings(lines []string, verdictIdx int) string {
	for i, line := range lines {
		stripped := stripMarkdown(line)
		if !strings.HasPrefix(strings.ToUpper(stripped), "FINDINGS") {
			continue
		}
		rest := strings.TrimLeft(stripped[len("FINDINGS"):], "*: \t")
		body := rest
		if i+1 < len(lines) {
			body = rest + "\n" + strings.Join(lines[i+1:], "\n")
		}
		return strings.TrimSpace(body)
	}
	if verdictIdx+1 < len(lines) {
		return strings.TrimSpace(strings.Join(lines[verdictIdx+1:], "\n"))
	}
	return ""
}

// unparsedFindings is the findings text for a malformed or absent response, so
// the human is told the Verifier could not be understood rather than seeing a
// bare NEEDS-HUMAN.
func unparsedFindings(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "The Verifier produced no output; a human must review this set."
	}
	return "The Verifier's response could not be parsed into a verdict; a human must review it.\n\nRaw response:\n" + trimmed
}

// stripMarkdown removes leading whitespace and common markdown decoration so a
// label check sees the bare token.
func stripMarkdown(line string) string {
	return strings.TrimLeft(line, " \t*#`>-")
}
