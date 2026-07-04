package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// AttemptTiming is one Captured attempt stream summarized for the timing lens:
// agent, outcome, and total duration, keyed by start time. No ordinal is
// carried, so the persisted sequence never contradicts the executor's
// per-invocation "Attempt N/max" line (ADR 0016). Tools holds the per-tool
// breakdown for agents with a pairing parser; empty otherwise. Model is the
// attempt's Model time — the residual of the total not covered by any tool
// window — and is meaningful only when Tools is non-empty. Tokens is the
// read-time-derived token spend (input/output/cache); it is absent when the
// agent's stream reports no usage.
type AttemptTiming struct {
	Agent          string
	RequestedAgent string
	ActualModel    string
	Start          time.Time
	Outcome        string
	Duration       time.Duration
	Tools          []ToolTiming
	Model          time.Duration
	Tokens         TokenUsage
	// Reason is the structured failure verdict from the footer and ExitCode the
	// agent's exit status; both are meaningful only when Outcome is a failure
	// and empty otherwise (ADR 0020). The timing lens does not render them — they
	// feed the Failed Task re-entry path via LatestFailureReason.
	Reason   string
	ExitCode int
}

// TokenUsage is the per-attempt token spend derived from a structured agent
// stream. Presence flags distinguish a reported zero from an absent field;
// a zero-value TokenUsage means the stream reported no usage.
type TokenUsage struct {
	Input         int64
	Output        int64
	CacheRead     int64
	CacheWrite    int64
	HasInput      bool
	HasOutput     bool
	HasCacheRead  bool
	HasCacheWrite bool
}

// HasUsage reports whether any token field was reported.
func (u TokenUsage) HasUsage() bool {
	return u.HasInput || u.HasOutput || u.HasCacheRead || u.HasCacheWrite
}

// ToolTiming aggregates one tool's paired invocations within an attempt:
// how many times it ran and the total wall-clock spent across those runs.
type ToolTiming struct {
	Name  string
	Count int
	Total time.Duration
}

// toolWindow is one tool-active interval within an attempt, in stream-relative
// milliseconds. EndMS of openWindowEndMS marks a window still open when the
// attempt ended (a tool_use with no result); modelTime clamps it to the
// attempt's total duration.
type toolWindow struct {
	StartMS int64
	EndMS   int64
}

const openWindowEndMS = int64(-1)

// toolTimingParsers maps agent preset → pairing parser over one attempt's
// stored events. Pairing tool_use with tool_result is per-adapter work because
// the stream shape differs across agents (ADR 0016); agents without a parser
// show outcome + total only. The windows feed the agent-independent Model
// time derivation.
var toolTimingParsers = map[string]func([]streamEventRecord) ([]ToolTiming, []toolWindow){
	"claude": claudeToolTimings,
	"codex":  codexToolTimings,
}

var actualModelParsers = map[string]func([]streamEventRecord) string{
	"claude": claudeActualModel,
}

var tokenUsageParsers = map[string]func([]streamEventRecord) TokenUsage{
	"claude": claudeTokenUsage,
}

// modelTime derives Model time: the attempt's total duration minus the union
// of tool-active intervals. The union — not the sum — is subtracted so
// parallel tool calls are not double-counted; open windows clamp to the
// attempt's end, and the result clamps at zero against clock skew.
func modelTime(windows []toolWindow, totalMS int64) time.Duration {
	clamped := make([]toolWindow, 0, len(windows))
	for _, w := range windows {
		if w.EndMS == openWindowEndMS || w.EndMS > totalMS {
			w.EndMS = totalMS
		}
		if w.StartMS < 0 {
			w.StartMS = 0
		}
		if w.StartMS >= w.EndMS {
			continue
		}
		clamped = append(clamped, w)
	}
	sort.Slice(clamped, func(i, j int) bool { return clamped[i].StartMS < clamped[j].StartMS })
	var coveredMS, coveredUntil int64
	for _, w := range clamped {
		if w.StartMS > coveredUntil {
			coveredUntil = w.StartMS
		}
		if w.EndMS > coveredUntil {
			coveredMS += w.EndMS - coveredUntil
			coveredUntil = w.EndMS
		}
	}
	residual := totalMS - coveredMS
	if residual < 0 {
		residual = 0
	}
	return time.Duration(residual) * time.Millisecond
}

// TaskTimings groups one task's attempts ordered by start time.
type TaskTimings struct {
	TaskID   string
	File     string
	Title    string
	Attempts []AttemptTiming
}

// TimingsOptions configures the Attempt timing breakdown.
type TimingsOptions struct {
	ResolveInput
	Target string
}

// TimingsResult is the per-task Attempt timing breakdown for one Task set.
type TimingsResult struct {
	TaskSetID string
	Tasks     []TaskTimings
}

// Timings derives the Attempt timing breakdown from stored Captured attempt
// streams; nothing new is persisted.
func Timings(opts TimingsOptions) (*TimingsResult, error) {
	return TimingsWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// TimingsWith derives the Attempt timing breakdown using injected dependencies.
// The target grammar mirrors implement: a bare Task set identifier covers every
// task in the set; an <task-set>/<file>.md reference covers one task.
func TimingsWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts TimingsOptions) (*TimingsResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	refresh, err := RefreshWith(d, resolved.DefinitionPath, StatePathFor(resolved.DefinitionPath))
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	taskSetID, taskID, err := ResolveTaskTarget(refresh, opts.Target)
	if err != nil {
		return nil, err
	}
	if taskSetID == "" {
		return nil, exitErr(ExitSetup, "requires a task set or <task-set>/<file>.md target")
	}

	m := refresh.Manifests[taskSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "task set %q has no task manifest", taskSetID)
	}
	if !m.Valid {
		return nil, exitErr(ExitNoRunnable, "task set %q is malformed", taskSetID)
	}

	result := &TimingsResult{TaskSetID: taskSetID}
	for _, task := range m.Tasks {
		if taskID != "" && task.ID != taskID {
			continue
		}
		attempts, err := readTaskAttemptTimings(d, m.Dir, task.File)
		if err != nil {
			return nil, exitErr(ExitOperational, "read attempt streams for %s/%s: %v", taskSetID, task.ID, err)
		}
		result.Tasks = append(result.Tasks, TaskTimings{
			TaskID:   task.ID,
			File:     task.File,
			Title:    task.Title,
			Attempts: attempts,
		})
	}
	return result, nil
}

// readTaskAttemptTimings summarizes every stored Captured attempt stream for
// one task, ordered by start time. History spans the task's whole lifetime:
// every attempt-NNN file in the directory, regardless of which implement
// invocation wrote it. A missing directory means no recorded attempts.
func readTaskAttemptTimings(d *Deps, taskSetDir, taskFile string) ([]AttemptTiming, error) {
	dir := taskStreamDir(taskSetDir, taskFile)
	entries, err := d.FS.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []AttemptTiming
	for _, e := range entries {
		if !attemptStreamNamePattern.MatchString(e.Name()) {
			continue
		}
		at, err := readAttemptTiming(d, filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, at)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

// readAttemptTiming reads one Captured attempt stream — gzip via the stdlib,
// so no external decompressor is needed — and summarizes its header and footer.
func readAttemptTiming(d *Deps, path string) (AttemptTiming, error) {
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return AttemptTiming{}, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return AttemptTiming{}, err
	}
	jsonl, err := io.ReadAll(zr)
	if err != nil {
		return AttemptTiming{}, err
	}
	if err := zr.Close(); err != nil {
		return AttemptTiming{}, err
	}

	var (
		header               streamHeaderRecord
		footer               streamFooterRecord
		hasHeader, hasFooter bool
		events               []streamEventRecord
	)
	for _, line := range bytes.Split(jsonl, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return AttemptTiming{}, fmt.Errorf("parse record: %w", err)
		}
		switch probe.Type {
		case "header":
			if err := json.Unmarshal(line, &header); err != nil {
				return AttemptTiming{}, fmt.Errorf("parse header: %w", err)
			}
			hasHeader = true
		case "footer":
			if err := json.Unmarshal(line, &footer); err != nil {
				return AttemptTiming{}, fmt.Errorf("parse footer: %w", err)
			}
			hasFooter = true
		case "event":
			var ev streamEventRecord
			if err := json.Unmarshal(line, &ev); err != nil {
				return AttemptTiming{}, fmt.Errorf("parse event: %w", err)
			}
			events = append(events, ev)
		}
	}
	if !hasHeader {
		return AttemptTiming{}, fmt.Errorf("missing header record")
	}
	if !hasFooter {
		return AttemptTiming{}, fmt.Errorf("missing footer record")
	}
	return deriveAttemptTiming(header, footer, events), nil
}

// deriveAttemptTiming builds an AttemptTiming from the parsed stream records.
// It is shared by the stream tracer and the inline breakdown so both lenses agree
// on the derived header; nothing is persisted.
func deriveAttemptTiming(header streamHeaderRecord, footer streamFooterRecord, events []streamEventRecord) AttemptTiming {
	requestedAgent := header.RequestedAgent
	if requestedAgent == "" {
		requestedAgent = header.Agent
	}
	var actualModel string
	if parse := actualModelParsers[header.Agent]; parse != nil {
		actualModel = parse(events)
	}
	var tools []ToolTiming
	var model time.Duration
	if parse := toolTimingParsers[header.Agent]; parse != nil {
		var windows []toolWindow
		tools, windows = parse(events)
		if len(tools) > 0 {
			model = modelTime(windows, footer.DurationMS)
		}
	}
	var tokens TokenUsage
	if parse := tokenUsageParsers[header.Agent]; parse != nil {
		tokens = parse(events)
	}
	return AttemptTiming{
		Agent:          header.Agent,
		RequestedAgent: requestedAgent,
		ActualModel:    actualModel,
		Start:          header.StartTime,
		Outcome:        footer.Outcome,
		Duration:       time.Duration(footer.DurationMS) * time.Millisecond,
		Tools:          tools,
		Model:          model,
		Tokens:         tokens,
		Reason:         footer.Reason,
		ExitCode:       footer.ExitCode,
	}
}

// LatestFailureReason returns the structured failure reason recorded on the
// latest persisted attempt footer for one task — the durable source the Failed
// Task re-entry path reads to recover why the last attempt failed without
// scraping the human-facing progress record (ADR 0020). Attempts are ordered by
// start time, so the last is the most recent regardless of attempt numbering.
// Returns "" when the task has no recorded attempts or the latest attempt did
// not record a reason (a non-failure outcome). The reason comes from the stream
// footer, never the task markdown.
func LatestFailureReason(d *Deps, taskSetDir, taskFile string) (string, error) {
	attempts, err := readTaskAttemptTimings(d, taskSetDir, taskFile)
	if err != nil {
		return "", err
	}
	if len(attempts) == 0 {
		return "", nil
	}
	return attempts[len(attempts)-1].Reason, nil
}

// RenderTimings writes the Attempt timing breakdown: per task, attempts
// ordered by start time, each row showing agent, outcome, and total duration.
func RenderTimings(w io.Writer, result *TimingsResult) {
	out := outputFor(w)
	for i, task := range result.Tasks {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s/%s  %s\n", result.TaskSetID, task.File, task.Title)
		if len(task.Attempts) == 0 {
			out.line(ansiDim, "  no recorded attempts")
			continue
		}
		renderAttemptRows(out, task.Attempts)
	}
}

// renderAttemptRows writes one task's attempt rows: agent, outcome, total
// duration, and derived token spend per attempt, with per-tool rows beneath.
// Shared by the stream tracer and the inline breakdown so the two views
// render identically.
func renderAttemptRows(out *output, attempts []AttemptTiming) {
	agentW, actualModelW, outcomeW := 0, 0, 0
	for _, a := range attempts {
		agentW = max(agentW, len(displayAttemptAgent(a)))
		actualModelW = max(actualModelW, len(a.ActualModel))
		outcomeW = max(outcomeW, len(a.Outcome))
	}
	for _, a := range attempts {
		tokens := formatTokenUsage(a.Tokens)
		if actualModelW > 0 {
			out.line(timingOutcomeStyle(a.Outcome), "  %s  %-*s  %-*s  %-*s  %s  %s",
				a.Start.Format(time.RFC3339), agentW, displayAttemptAgent(a), actualModelW, a.ActualModel, outcomeW, a.Outcome, formatAttemptDuration(a.Duration), tokens)
		} else {
			out.line(timingOutcomeStyle(a.Outcome), "  %s  %-*s  %-*s  %s  %s",
				a.Start.Format(time.RFC3339), agentW, displayAttemptAgent(a), outcomeW, a.Outcome, formatAttemptDuration(a.Duration), tokens)
		}
		renderToolTimings(out, a.Tools, a.Model)
	}
}

// formatTokenUsage renders a TokenUsage for the attempt row. When no usage was
// reported it returns an em dash; otherwise it shows input/output and any
// reported cache read/write figures.
func formatTokenUsage(u TokenUsage) string {
	if !u.HasUsage() {
		return "—"
	}
	var parts []string
	if u.HasInput {
		parts = append(parts, fmt.Sprintf("in %d", u.Input))
	}
	if u.HasOutput {
		parts = append(parts, fmt.Sprintf("out %d", u.Output))
	}
	if u.HasCacheRead || u.HasCacheWrite {
		var cache []string
		if u.HasCacheRead {
			cache = append(cache, fmt.Sprintf("%dr", u.CacheRead))
		}
		if u.HasCacheWrite {
			cache = append(cache, fmt.Sprintf("%dw", u.CacheWrite))
		}
		parts = append(parts, "cache "+strings.Join(cache, " "))
	}
	return strings.Join(parts, " / ")
}

func displayAttemptAgent(a AttemptTiming) string {
	if a.RequestedAgent != "" {
		return a.RequestedAgent
	}
	return a.Agent
}

// printAttemptBreakdown prints the Attempt timing breakdown for the Captured
// attempt streams written by this invocation, as a task reaches a terminal
// state during implement. It re-reads the stored files through the reader's
// derivation (readAttemptTiming), so the inline view and the stream replay
// can never disagree; full history stays with the reader. Best-effort like
// the write path: an unreadable stream is skipped, never an error. No paths
// (plain-output or custom-command attempts) prints nothing.
func printAttemptBreakdown(d *Deps, w io.Writer, paths []string) {
	var attempts []AttemptTiming
	for _, p := range paths {
		at, err := readAttemptTiming(d, p)
		if err != nil {
			continue
		}
		attempts = append(attempts, at)
	}
	if len(attempts) == 0 {
		return
	}
	sort.SliceStable(attempts, func(i, j int) bool { return attempts[i].Start.Before(attempts[j].Start) })
	out := outputFor(w)
	out.line(ansiDim, "  Attempt timing")
	renderAttemptRows(out, attempts)
}

// renderToolTimings writes one attempt's per-tool rows indented under the
// attempt line, so tool figures sit under the agent that ran them, closed by
// the Model time row. The model row has no count column — it is the residual
// of the attempt, not a tool — and renders only when tool rows do.
func renderToolTimings(out *output, tools []ToolTiming, model time.Duration) {
	if len(tools) == 0 {
		return
	}
	nameW, countW := len("model"), 0
	for _, t := range tools {
		nameW = max(nameW, len(t.Name))
		countW = max(countW, len(fmt.Sprintf("%d", t.Count)))
	}
	for _, t := range tools {
		out.line(ansiDim, "    %-*s  ×%-*d  %s", nameW, t.Name, countW, t.Count, formatAttemptDuration(t.Total))
	}
	out.line(ansiDim, "    %-*s  %-*s  %s", nameW, "model", countW+1, "", formatAttemptDuration(model))
}

func timingOutcomeStyle(outcome string) string {
	switch outcome {
	case streamOutcomeCompleted:
		return ansiGreen
	case streamOutcomeFailed:
		return ansiRed
	default:
		return ansiYellow
	}
}

// formatAttemptDuration keeps sub-second durations exact and rounds the rest
// to whole seconds.
func formatAttemptDuration(d time.Duration) string {
	if d >= time.Second {
		d = d.Round(time.Second)
	}
	return d.String()
}
