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
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// AttemptTiming is one Captured attempt stream summarized for the timing lens:
// agent, outcome, and total duration, keyed by start time. No ordinal is
// carried, so the persisted sequence never contradicts the executor's
// per-invocation "Attempt N/max" line (ADR 0016). Tools holds the per-tool
// breakdown for agents with a pairing parser; empty otherwise.
type AttemptTiming struct {
	Agent    string
	Start    time.Time
	Outcome  string
	Duration time.Duration
	Tools    []ToolTiming
}

// ToolTiming aggregates one tool's paired invocations within an attempt:
// how many times it ran and the total wall-clock spent across those runs.
type ToolTiming struct {
	Name  string
	Count int
	Total time.Duration
}

// toolTimingParsers maps agent preset → pairing parser over one attempt's
// stored events. Pairing tool_use with tool_result is per-adapter work because
// the stream shape differs across agents (ADR 0016); agents without a parser
// show outcome + total only.
var toolTimingParsers = map[string]func([]streamEventRecord) []ToolTiming{
	"claude": claudeToolTimings,
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
		return nil, exitErr(ExitSetup, "timings requires a task set or <task-set>/<file>.md target")
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
	var tools []ToolTiming
	if parse := toolTimingParsers[header.Agent]; parse != nil {
		tools = parse(events)
	}
	return AttemptTiming{
		Agent:    header.Agent,
		Start:    header.StartTime,
		Outcome:  footer.Outcome,
		Duration: time.Duration(footer.DurationMS) * time.Millisecond,
		Tools:    tools,
	}, nil
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
		agentW, outcomeW := 0, 0
		for _, a := range task.Attempts {
			agentW = max(agentW, len(a.Agent))
			outcomeW = max(outcomeW, len(a.Outcome))
		}
		for _, a := range task.Attempts {
			out.line(timingOutcomeStyle(a.Outcome), "  %s  %-*s  %-*s  %s",
				a.Start.Format(time.RFC3339), agentW, a.Agent, outcomeW, a.Outcome, formatAttemptDuration(a.Duration))
			renderToolTimings(out, a.Tools)
		}
	}
}

// renderToolTimings writes one attempt's per-tool rows indented under the
// attempt line, so tool figures sit under the agent that ran them.
func renderToolTimings(out *output, tools []ToolTiming) {
	nameW, countW := 0, 0
	for _, t := range tools {
		nameW = max(nameW, len(t.Name))
		countW = max(countW, len(fmt.Sprintf("%d", t.Count)))
	}
	for _, t := range tools {
		out.line(ansiDim, "    %-*s  ×%-*d  %s", nameW, t.Name, countW, t.Count, formatAttemptDuration(t.Total))
	}
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
