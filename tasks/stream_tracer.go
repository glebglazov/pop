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
	"unicode/utf8"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// StreamOptions configures the stream tracer lens.
type StreamOptions struct {
	ResolveInput
	Target string
}

// RenderStreamOptions configures how the stream replay is rendered.
type RenderStreamOptions struct {
	// Full disables payload truncation and prints every payload verbatim.
	Full bool
}

// streamDelimiter is written as a JSON line before each attempt file in raw
// concatenated output so downstream parsers can identify attempt boundaries.
// It is a valid JSONL line with a unique "delimiter" type.
type streamDelimiter struct {
	Type string `json:"type"`
	File string `json:"file"`
}

// streamTruncationLimits defines the default head/tail truncation thresholds
// for large tool payloads in the rendered replay.
var streamTruncationLimits = struct {
	MaxLines  int
	HeadLines int
	TailLines int
	MaxBytes  int
	HeadBytes int
	TailBytes int
}{
	MaxLines:  40,
	HeadLines: 15,
	TailLines: 15,
	MaxBytes:  4096,
	HeadBytes: 1536,
	TailBytes: 1536,
}

// StreamResult is the per-task attempt stream replay for one Task set.
type StreamResult struct {
	TaskSetID string
	Tasks     []TaskStream
}

// TaskStream groups one task's attempts ordered by start time.
type TaskStream struct {
	TaskID   string
	File     string
	Title    string
	Attempts []AttemptStream
}

// AttemptStream is one captured attempt's timing header and event sequence.
type AttemptStream struct {
	Timing AttemptTiming
	Events []StreamEvent
}

// StreamEvent is one rendered event from the attempt's stream.
type StreamEvent struct {
	AtMS     int64
	Type     string // "assistant", "user", "system", "result", "narration"
	Text     string // rendered text content
	ToolName string // for tool_use events
	ToolArgs string // for tool_use events, the input JSON
}

// Stream derives the attempt stream replay from stored captured attempt
// streams; nothing new is persisted.
func Stream(opts StreamOptions) (*StreamResult, error) {
	return StreamWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// StreamWith derives the attempt stream replay using injected dependencies.
// The target grammar mirrors implement and timings: a bare Task set identifier
// covers every task in the set; a <task-set>/<file>.md reference covers one task.
func StreamWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts StreamOptions) (*StreamResult, error) {
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
		return nil, exitErr(ExitSetup, "stream requires a task set or <task-set>/<file>.md target")
	}

	m := refresh.Manifests[taskSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "task set %q has no task manifest", taskSetID)
	}
	if !m.Valid {
		return nil, exitErr(ExitNoRunnable, "task set %q is malformed", taskSetID)
	}

	result := &StreamResult{TaskSetID: taskSetID}
	for _, task := range m.Tasks {
		if taskID != "" && task.ID != taskID {
			continue
		}
		attempts, err := readTaskAttemptStreams(d, m.Dir, task.File)
		if err != nil {
			return nil, exitErr(ExitOperational, "read attempt streams for %s/%s: %v", taskSetID, task.ID, err)
		}
		result.Tasks = append(result.Tasks, TaskStream{
			TaskID:   task.ID,
			File:     task.File,
			Title:    task.Title,
			Attempts: attempts,
		})
	}
	return result, nil
}

// readTaskAttemptStreams reads every stored captured attempt stream for one
// task, ordered by start time, returning both the timing summary and the full
// event sequence for each attempt.
func readTaskAttemptStreams(d *Deps, taskSetDir, taskFile string) ([]AttemptStream, error) {
	dir := taskStreamDir(taskSetDir, taskFile)
	entries, err := d.FS.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []AttemptStream
	for _, e := range entries {
		if !attemptStreamNamePattern.MatchString(e.Name()) {
			continue
		}
		at, err := loadAttemptStream(d, filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, at)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timing.Start.Before(out[j].Timing.Start) })
	return out, nil
}

// loadAttemptStream reads one captured attempt stream — gzip via the stdlib —
// and returns both the timing summary and the full event sequence.
func loadAttemptStream(d *Deps, path string) (AttemptStream, error) {
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return AttemptStream{}, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return AttemptStream{}, err
	}
	jsonl, err := io.ReadAll(zr)
	if err != nil {
		return AttemptStream{}, err
	}
	if err := zr.Close(); err != nil {
		return AttemptStream{}, err
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
			return AttemptStream{}, fmt.Errorf("parse record: %w", err)
		}
		switch probe.Type {
		case "header":
			if err := json.Unmarshal(line, &header); err != nil {
				return AttemptStream{}, fmt.Errorf("parse header: %w", err)
			}
			hasHeader = true
		case "footer":
			if err := json.Unmarshal(line, &footer); err != nil {
				return AttemptStream{}, fmt.Errorf("parse footer: %w", err)
			}
			hasFooter = true
		case "event":
			var ev streamEventRecord
			if err := json.Unmarshal(line, &ev); err != nil {
				return AttemptStream{}, fmt.Errorf("parse event: %w", err)
			}
			events = append(events, ev)
		}
	}
	if !hasHeader {
		return AttemptStream{}, fmt.Errorf("missing header record")
	}
	if !hasFooter {
		return AttemptStream{}, fmt.Errorf("missing footer record")
	}

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

	timing := AttemptTiming{
		Agent:          header.Agent,
		RequestedAgent: requestedAgent,
		ActualModel:    actualModel,
		Start:          header.StartTime,
		Outcome:        footer.Outcome,
		Duration:       time.Duration(footer.DurationMS) * time.Millisecond,
		Tools:          tools,
		Model:          model,
		Reason:         footer.Reason,
		ExitCode:       footer.ExitCode,
	}

	renderedEvents := renderStreamEvents(header.Agent, events)

	return AttemptStream{
		Timing: timing,
		Events: renderedEvents,
	}, nil
}

// renderStreamEvents converts raw stream events into a readable sequence for
// the tracer. For claude agents, it parses assistant messages (text and
// tool_use) and user tool_result messages. For other agents, it renders the
// raw JSON.
func renderStreamEvents(agent string, events []streamEventRecord) []StreamEvent {
	var out []StreamEvent
	for _, ev := range events {
		rendered := renderOneStreamEvent(agent, ev)
		out = append(out, rendered...)
	}
	return out
}

// renderOneStreamEvent renders one raw event into zero or more StreamEvents.
// For claude agents, assistant events produce text and tool_use entries, user
// events produce tool_result entries. System events (like init) are rendered
// as narration.
func renderOneStreamEvent(agent string, ev streamEventRecord) []StreamEvent {
	var out []StreamEvent

	switch agent {
	case "claude":
		out = renderClaudeEvent(ev)
	default:
		// For agents without a specific renderer, show the raw JSON
		out = append(out, StreamEvent{
			AtMS: ev.AtMS,
			Type: "raw",
			Text: ev.Raw,
		})
	}

	return out
}

// renderClaudeEvent parses one claude-format event into readable entries.
func renderClaudeEvent(ev streamEventRecord) []StreamEvent {
	var out []StreamEvent

	var event struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Model   string `json:"model"`
		Message struct {
			Content []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				Name  string          `json:"name"`
				ID    string          `json:"id"`
				Input json.RawMessage `json:"input"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
		// Malformed event: show as raw
		return []StreamEvent{{
			AtMS: ev.AtMS,
			Type: "raw",
			Text: ev.Raw,
		}}
	}

	switch event.Type {
	case "system":
		if event.Subtype == "init" {
			text := "system init"
			if event.Model != "" {
				text += " (model: " + event.Model + ")"
			}
			out = append(out, StreamEvent{
				AtMS: ev.AtMS,
				Type: "system",
				Text: text,
			})
		}
	case "assistant":
		for _, c := range event.Message.Content {
			switch c.Type {
			case "text":
				if text := strings.TrimRight(c.Text, "\n"); text != "" {
					out = append(out, StreamEvent{
						AtMS: ev.AtMS,
						Type: "assistant",
						Text: text,
					})
				}
			case "tool_use":
				args := ""
				if len(c.Input) > 0 {
					args = string(c.Input)
				}
				out = append(out, StreamEvent{
					AtMS:     ev.AtMS,
					Type:     "tool_use",
					ToolName: c.Name,
					ToolArgs: args,
				})
			}
		}
	case "user":
		for _, c := range event.Message.Content {
			if c.Type == "tool_result" {
				// Extract the result content
				var resultText string
				if len(c.Content) > 0 {
					var parts []string
					for _, part := range c.Content {
						if part.Type == "text" {
							parts = append(parts, part.Text)
						}
					}
					resultText = strings.Join(parts, "\n")
				}
				out = append(out, StreamEvent{
					AtMS: ev.AtMS,
					Type: "tool_result",
					Text: resultText,
				})
			}
		}
	}

	return out
}

// StreamRawWith resolves the target, finds every stored captured attempt stream,
// decompresses each gzip file via the stdlib, and writes the JSONL verbatim to
// w in the same chronological / manifest order the rendered view uses. For
// multi-attempt or bare-set targets, a lightweight JSON delimiter record is
// inserted before each attempt file so boundaries stay parseable.
func StreamRawWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts StreamOptions, w io.Writer) error {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return exitErr(ExitSetup, "%v", err)
	}

	refresh, err := RefreshWith(d, resolved.DefinitionPath, StatePathFor(resolved.DefinitionPath))
	if err != nil {
		return exitErr(ExitSetup, "%v", err)
	}

	taskSetID, taskID, err := ResolveTaskTarget(refresh, opts.Target)
	if err != nil {
		return err
	}
	if taskSetID == "" {
		return exitErr(ExitSetup, "stream requires a task set or <task-set>/<file>.md target")
	}

	m := refresh.Manifests[taskSetID]
	if m == nil {
		return exitErr(ExitNoRunnable, "task set %q has no task manifest", taskSetID)
	}
	if !m.Valid {
		return exitErr(ExitNoRunnable, "task set %q is malformed", taskSetID)
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	first := true

	for _, task := range m.Tasks {
		if taskID != "" && task.ID != taskID {
			continue
		}
		dir := taskStreamDir(m.Dir, task.File)
		entries, err := d.FS.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return exitErr(ExitOperational, "read stream dir for %s/%s: %v", taskSetID, task.ID, err)
		}

		// Collect matching attempt files with their start time for chronological order.
		type namedAttempt struct {
			name  string
			start time.Time
		}
		var attempts []namedAttempt
		for _, e := range entries {
			if !attemptStreamNamePattern.MatchString(e.Name()) {
				continue
			}
			path := filepath.Join(dir, e.Name())
			start, err := peekAttemptStart(d, path)
			if err != nil {
				return exitErr(ExitOperational, "peek start time for %s: %v", e.Name(), err)
			}
			attempts = append(attempts, namedAttempt{name: e.Name(), start: start})
		}
		if len(attempts) == 0 {
			continue
		}

		sort.SliceStable(attempts, func(i, j int) bool { return attempts[i].start.Before(attempts[j].start) })

		for _, na := range attempts {
			path := filepath.Join(dir, na.name)
			data, err := d.FS.ReadFile(path)
			if err != nil {
				return exitErr(ExitOperational, "read %s: %v", na.name, err)
			}
			zr, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return exitErr(ExitOperational, "decompress %s: %v", na.name, err)
			}
			jsonl, err := io.ReadAll(zr)
			_ = zr.Close()
			if err != nil {
				return exitErr(ExitOperational, "decompress %s: %v", na.name, err)
			}

			// Write delimiter when this is not the first attempt file overall.
			// For a single attempt (most common case) no delimiter is emitted,
			// preserving pure JSONL for the simplest path.
			if !first {
				if err := enc.Encode(streamDelimiter{Type: "delimiter", File: na.name}); err != nil {
					return fmt.Errorf("write delimiter: %w", err)
				}
			}
			first = false

			if _, err := w.Write(bytes.TrimSpace(jsonl)); err != nil {
				return fmt.Errorf("write decompressed stream: %w", err)
			}
			if _, err := w.Write([]byte("\n")); err != nil {
				return fmt.Errorf("write newline: %w", err)
			}
		}
	}

	if first {
		fmt.Fprintf(w, "no captured attempt streams for %s\n", taskSetID)
	}
	return nil
}

// peekAttemptStart reads the header record from a gzipped attempt stream to
// extract the start time without fully decompressing the file.
func peekAttemptStart(d *Deps, path string) (time.Time, error) {
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return time.Time{}, err
	}
	defer zr.Close()
	dec := json.NewDecoder(zr)
	var header streamHeaderRecord
	if err := dec.Decode(&header); err != nil {
		return time.Time{}, err
	}
	if header.Type != "header" {
		return time.Time{}, fmt.Errorf("first record is not a header")
	}
	return header.StartTime, nil
}

// RenderStream writes the attempt stream replay: per task, attempts ordered by
// start time, each showing the timing breakdown header followed by the event
// sequence with +Xs offsets.
func RenderStream(w io.Writer, result *StreamResult, opts RenderStreamOptions) {
	out := outputFor(w)
	hasAnyAttempts := false
	for _, task := range result.Tasks {
		if len(task.Attempts) > 0 {
			hasAnyAttempts = true
			break
		}
	}

	if !hasAnyAttempts {
		fmt.Fprintf(out, "no captured attempt streams for %s\n", result.TaskSetID)
		return
	}

	for i, task := range result.Tasks {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s/%s  %s\n", result.TaskSetID, task.File, task.Title)
		if len(task.Attempts) == 0 {
			out.line(ansiDim, "  no captured attempt streams")
			continue
		}
		renderAttemptStreams(out, task.Attempts, opts)
	}
}

// renderAttemptStreams writes one task's attempt streams: the timing breakdown
// header followed by the event replay with +Xs offsets.
func renderAttemptStreams(out *output, attempts []AttemptStream, opts RenderStreamOptions) {
	// First render the timing breakdown header for each attempt
	for _, a := range attempts {
		renderAttemptTimingHeader(out, a.Timing)
	}

	// Then render the event sequence for each attempt
	for i, a := range attempts {
		if i > 0 {
			fmt.Fprintln(out)
		}
		renderAttemptEventReplay(out, a, opts)
	}
}

// renderAttemptTimingHeader writes the timing breakdown for one attempt,
// mirroring the format used by RenderTimings.
func renderAttemptTimingHeader(out *output, a AttemptTiming) {
	if a.ActualModel != "" {
		out.line(timingOutcomeStyle(a.Outcome), "  %s  %s  %s  %s  %s",
			a.Start.Format(time.RFC3339), displayAttemptAgent(a), a.ActualModel, a.Outcome, formatAttemptDuration(a.Duration))
	} else {
		out.line(timingOutcomeStyle(a.Outcome), "  %s  %s  %s  %s",
			a.Start.Format(time.RFC3339), displayAttemptAgent(a), a.Outcome, formatAttemptDuration(a.Duration))
	}
	renderToolTimings(out, a.Tools, a.Model)
}

// renderAttemptEventReplay writes one attempt's event sequence with +Xs offsets.
func renderAttemptEventReplay(out *output, a AttemptStream, opts RenderStreamOptions) {
	if len(a.Events) == 0 {
		out.line(ansiDim, "  no events")
		return
	}

	fmt.Fprintf(out, "  Attempt starting %s:\n", a.Timing.Start.Format(time.RFC3339))
	for _, ev := range a.Events {
		offset := formatOffset(ev.AtMS)
		switch ev.Type {
		case "system":
			out.line(ansiDim, "    %s  %s", offset, ev.Text)
		case "assistant":
			fmt.Fprintf(out, "    %s  %s\n", offset, ev.Text)
		case "tool_use":
			args := truncatePayload(ev.ToolArgs, opts.Full)
			out.line(ansiDim, "    %s  → %s %s", offset, ev.ToolName, args)
		case "tool_result":
			if ev.Text != "" {
				// Indent multiline results
				text := truncatePayload(ev.Text, opts.Full)
				lines := strings.Split(text, "\n")
				for i, line := range lines {
					if i == 0 {
						out.line(ansiDim, "    %s    %s", offset, line)
					} else {
						out.line(ansiDim, "    %s    %s", offset, line)
					}
				}
			}
		case "raw":
			out.line(ansiDim, "    %s  %s", offset, ev.Text)
		}
	}
}

// truncatePayload clips oversized tool payloads to a head+tail excerpt with an
// elision marker. Assistant text and small payloads are returned unchanged.
func truncatePayload(text string, full bool) string {
	if full || text == "" {
		return text
	}

	lim := streamTruncationLimits

	lines := strings.Split(text, "\n")
	if len(lines) > lim.MaxLines {
		head := strings.Join(lines[:lim.HeadLines], "\n")
		tail := strings.Join(lines[len(lines)-lim.TailLines:], "\n")
		elidedLines := len(lines) - lim.HeadLines - lim.TailLines
		elidedBytes := len(text) - len(head) - len(tail)
		if head != "" && tail != "" {
			elidedBytes-- // the newline that joined head and tail in the original text
		}
		marker := fmt.Sprintf("… %s / %d lines elided …", humanizeBytes(elidedBytes), elidedLines)
		if head == "" {
			return marker + "\n" + tail
		}
		if tail == "" {
			return head + "\n" + marker
		}
		return head + "\n" + marker + "\n" + tail
	}

	if len(text) > lim.MaxBytes {
		head := text[:lim.HeadBytes]
		for !utf8.ValidString(head) && len(head) > 0 {
			head = head[:len(head)-1]
		}
		tail := text[len(text)-lim.TailBytes:]
		for !utf8.ValidString(tail) && len(tail) > 0 {
			tail = tail[1:]
		}
		elidedBytes := len(text) - len(head) - len(tail)
		marker := fmt.Sprintf("… %s / 0 lines elided …", humanizeBytes(elidedBytes))
		return head + "\n" + marker + "\n" + tail
	}

	return text
}

// humanizeBytes formats a byte count as a compact, human-readable string.
func humanizeBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	kb := float64(n) / 1024
	if kb < 1024 {
		return fmt.Sprintf("%.1f KB", kb)
	}
	mb := kb / 1024
	return fmt.Sprintf("%.1f MB", mb)
}

// formatOffset formats a millisecond offset as +Xs or +Xm Ys.
func formatOffset(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("+%dms", ms)
	}
	secs := ms / 1000
	if secs < 60 {
		return fmt.Sprintf("+%ds", secs)
	}
	mins := secs / 60
	secs = secs % 60
	return fmt.Sprintf("+%dm%ds", mins, secs)
}
