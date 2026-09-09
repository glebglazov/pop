package tasks

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

const spendPhaseEval = "eval"

// CapturedAgentOptions describes one Bare-arm invocation. DestinationDir is
// the pair directory itself; no streams/runs suffix is added.
type CapturedAgentOptions struct {
	AgentSpec      string
	Prompt         string
	RuntimePath    string
	Timeout        time.Duration
	DestinationDir string
	OutputMode     AgentOutputMode
	// DeclaredRates applies the same model-key overrides as the Spend lens.
	// Nil uses the published Rate table only.
	DeclaredRates map[string]ModelRates
}

// CapturedAgentAttempt carries the persisted outcome and its read-derived
// spend. Has* flags in Spend and Notional distinguish absent figures from zero.
// Notional follows the Spend lens: agent-reported cost outranks table pricing.
type CapturedAgentAttempt struct {
	RunID          string
	Output         string
	Outcome        string
	Reason         string
	ExitCode       int
	ProceedVerdict *AgentProceedVerdict
	ActualModel    string
	Spend          RunSpend
	Notional       PricedSpend
}

// RunCapturedAgentInvocation runs once, without a drain or a task prompt wrapper.
// A non-zero exit, timeout, or quota refusal is a recorded outcome, not a Go
// error. Setup, capture, and spend-reading failures return errors. Once the
// agent ran, the result is returned too, even when its capture could not be
// written; when a pair was written, the result identifies it.
func RunCapturedAgentInvocation(d *Deps, opts CapturedAgentOptions) (*CapturedAgentAttempt, error) {
	if strings.TrimSpace(opts.AgentSpec) == "" {
		return nil, fmt.Errorf("captured invocation requires a recognized Agent preset; custom commands file no Captured run")
	}
	if opts.RuntimePath == "" || opts.DestinationDir == "" || opts.Timeout <= 0 {
		return nil, fmt.Errorf("captured invocation requires a working directory, destination directory, and positive timeout")
	}
	invocation, err := ResolveAgentInvocationWithMode(opts.AgentSpec, "", opts.Prompt, opts.RuntimePath, opts.OutputMode)
	if err != nil {
		return nil, fmt.Errorf("captured invocation requires a recognized Agent preset (custom commands file no Captured run): %w", err)
	}
	if invocation.OutputFormat == AgentOutputPlain {
		return nil, fmt.Errorf("plain-output specs file no Captured run")
	}
	raw, attempt, err := runAgentAttempt(d, opts.RuntimePath, io.Discard, opts.Timeout, invocation)
	if err != nil {
		return nil, err
	}
	normalized := invocation.NormalizeOutput(raw)
	result := &CapturedAgentAttempt{
		Output:   normalized.Output,
		Outcome:  verifyAttemptOutcome(attempt),
		Reason:   verifyAttemptReason(attempt),
		ExitCode: attempt.exitCode,
	}
	if !attempt.interrupted && !attempt.timedOut && attempt.runErr == nil {
		if verdict := normalized.ProceedVerdict; verdict != nil {
			v := stampDetectedVerdict(*verdict, invocation.AgentPreset(), invocation.PinnedModel())
			result.Outcome = streamOutcomeAgentUnusable
			if _, ok := v.TimeHealing(); ok {
				v = resolveProceedResetAt(v, d.Now())
				result.Outcome = streamOutcomeQuotaPaused
			}
			result.ProceedVerdict = &v
			result.Reason = v.Reason
		}
	}
	model := ""
	if extractActualModel(invocation.AgentPreset(), attempt.stream.events) == "" {
		model = invocation.PinnedModel()
	}
	metaPath, _, err := writeCapturedRunInDir(d, opts.DestinationDir, spendPhaseEval, "", "", "", attempt.stream, invocation.AgentPreset(), invocation.RequestedAgent, model, 1, result.Outcome, result.Reason, result.ExitCode, "", "")
	if err != nil {
		return result, fmt.Errorf("write Captured run: %w", err)
	}
	result.RunID = strings.TrimSuffix(filepath.Base(metaPath), ".meta.json")
	// Read the stored payload so Codex's Rollout splice is priced too.
	run, err := loadCapturedRun(d, opts.DestinationDir, filepath.Base(metaPath))
	if err != nil {
		return result, err
	}
	result.Spend, err = runSpend(run)
	if err != nil {
		return result, err
	}
	result.ActualModel = extractActualModel(run.meta.Agent, run.events)
	result.Notional = priceRunSpend(run, result.Spend, loadRateTableForSpend(d), opts.DeclaredRates)
	return result, nil
}
