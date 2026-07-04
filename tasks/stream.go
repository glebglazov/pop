package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// streamsDirName is the Task-set subdirectory holding Captured attempt streams
// (ADR 0016). The name is the substrate, not one derived view: timing is the
// first lens over these files, not the last.
const streamsDirName = "streams"

// capturedRunsDirName is the subdirectory under streams/ that holds the
// uuid-keyed Captured run pairs introduced by ADR-0094.
const capturedRunsDirName = "runs"

// Footer outcomes. The kill outcomes distinguish why an attempt stopped,
// mirroring the Exhausted task / Interrupted task / Agent quota pause
// vocabulary, so a killed attempt's file still carries its terminal outcome.
const (
	streamOutcomeCompleted   = "completed"
	streamOutcomeFailed      = "failed"
	streamOutcomeTimedOut    = "timed_out"
	streamOutcomeInterrupted = "interrupted"
	streamOutcomeQuotaPaused = "quota_paused"
)

// streamHeaderRecord opens a Captured attempt stream file.
type streamHeaderRecord struct {
	Type           string    `json:"type"`
	Agent          string    `json:"agent"`
	RequestedAgent string    `json:"requested_agent,omitempty"`
	Attempt        int       `json:"attempt"`
	StartTime      time.Time `json:"start_time"`
}

// streamEventRecord is one raw stream event tagged with its arrival time
// relative to the attempt's start.
type streamEventRecord struct {
	Type string `json:"type"`
	AtMS int64  `json:"at_ms"`
	Raw  string `json:"raw"`
}

// streamFooterRecord closes a Captured attempt stream file. Reason is the
// structured failure verdict assessment produced (the agent's own TASK_FAILED
// text, a missing sentinel/summary, unchecked acceptance criteria, or a
// non-zero exit) and ExitCode the agent's exit status; both ride beside the
// outcome so a later run re-entering a Failed Task set can recover why the last
// attempt failed without scraping the human-facing progress record (ADR 0020).
// Reason is a finer-grained verdict than Outcome and is populated only on
// failure paths; on other terminal paths it is empty and omitted.
type streamFooterRecord struct {
	Type       string `json:"type"`
	Outcome    string `json:"outcome"`
	DurationMS int64  `json:"duration_ms"`
	Reason     string `json:"reason,omitempty"`
	ExitCode   int    `json:"exit_code,omitempty"`
}

// capturedRunMeta is the index half of a Captured run pair (ADR-0094). It
// stores the run's identity, timing, and outcome so the event payload can be
// located and filtered without decompressing every events file.
type capturedRunMeta struct {
	RunID          string    `json:"run_id"`
	Phase          string    `json:"phase"`
	TaskSetID      string    `json:"task_set_id"`
	TaskID         string    `json:"task_id"`
	TaskFile       string    `json:"task_file"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Outcome        string    `json:"outcome"`
	Reason         string    `json:"reason,omitempty"`
	ExitCode       int       `json:"exit_code,omitempty"`
	Agent          string    `json:"agent"`
	RequestedAgent string    `json:"requested_agent,omitempty"`
	Attempt        int       `json:"attempt"`
}

// capturedRun is an in-memory representation of one persisted run, pairing its
// meta record with the raw event sequence. legacyPath is set only for runs
// loaded from legacy task-stem gzips and keeps the original storage path so
// raw replay can locate the file without parsing the synthetic run_id.
type capturedRun struct {
	meta       capturedRunMeta
	events     []streamEventRecord
	legacyPath string
}

// phaseOrder returns a sort rank for run phases. Lower values sort earlier.
func phaseOrder(phase string) int {
	switch phase {
	case "implement":
		return 0
	case "verify":
		return 1
	default:
		return 2
	}
}

// sortRunsChronologically sorts runs by start time, with implement before
// verify at equal timestamps. The sort is stable.
func sortRunsChronologically(runs []capturedRun) {
	sort.SliceStable(runs, func(i, j int) bool {
		if !runs[i].meta.StartTime.Equal(runs[j].meta.StartTime) {
			return runs[i].meta.StartTime.Before(runs[j].meta.StartTime)
		}
		return phaseOrder(runs[i].meta.Phase) < phaseOrder(runs[j].meta.Phase)
	})
}

// newRunID produces a fresh run identifier. It is a variable so tests can
// substitute a deterministic generator.
var newRunID = func() string { return uuid.New().String() }

// streamRecorder sits on the capture tee: it forwards raw bytes to the capture
// buffer unchanged and records each complete line with its arrival time. The
// timestamps live here — where the authoritative bytes are kept — so a
// rendering bug or an unrendered event type can never distort the persisted
// record (ADR 0016).
type streamRecorder struct {
	dst    io.Writer
	now    func() time.Time
	start  time.Time
	end    time.Time
	events []streamEventRecord
	buf    []byte
}

func newStreamRecorder(dst io.Writer, now func() time.Time) *streamRecorder {
	if now == nil {
		now = time.Now
	}
	return &streamRecorder{dst: dst, now: now, start: now()}
}

func (r *streamRecorder) Write(p []byte) (int, error) {
	n, err := r.dst.Write(p)
	if err != nil {
		return n, err
	}
	at := r.now().Sub(r.start)
	r.buf = append(r.buf, p...)
	for {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			break
		}
		r.record(at, r.buf[:i])
		r.buf = r.buf[i+1:]
	}
	return len(p), nil
}

func (r *streamRecorder) record(at time.Duration, line []byte) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	r.events = append(r.events, streamEventRecord{
		Type: "event",
		AtMS: at.Milliseconds(),
		Raw:  string(line),
	})
}

// finish records any unterminated trailing line and stamps the attempt's end time.
func (r *streamRecorder) finish() {
	end := r.now()
	if len(r.buf) > 0 {
		r.record(end.Sub(r.start), r.buf)
		r.buf = nil
	}
	r.end = end
}

// encodeAttemptStream renders one self-contained JSONL document: header,
// timestamped raw events, footer.
func encodeAttemptStream(r *streamRecorder, agent, requestedAgent string, attempt int, outcome, reason string, exitCode int) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(streamHeaderRecord{Type: "header", Agent: agent, RequestedAgent: requestedAgent, Attempt: attempt, StartTime: r.start.UTC()}); err != nil {
		return nil, err
	}
	for _, ev := range r.events {
		if err := enc.Encode(ev); err != nil {
			return nil, err
		}
	}
	end := r.end
	if end.IsZero() {
		end = r.now()
	}
	if err := enc.Encode(streamFooterRecord{Type: "footer", Outcome: outcome, DurationMS: end.Sub(r.start).Milliseconds(), Reason: reason, ExitCode: exitCode}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeCapturedRunEvents renders the event payload for a Captured run: one
// timestamped raw event per JSONL line, no header or footer.
func encodeCapturedRunEvents(rec *streamRecorder) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, ev := range rec.events {
		if err := enc.Encode(ev); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// taskStreamDir returns the Captured-attempt-stream directory for one task:
// <task-set>/streams/<task-stem>.
func taskStreamDir(taskSetDir, taskFile string) string {
	stem := strings.TrimSuffix(taskFile, filepath.Ext(taskFile))
	return filepath.Join(taskSetDir, streamsDirName, stem)
}

// capturedRunsDir returns the Captured run directory for a task set:
// <task-set>/streams/runs.
func capturedRunsDir(taskSetDir string) string {
	return filepath.Join(taskSetDir, streamsDirName, capturedRunsDirName)
}

var attemptStreamNamePattern = regexp.MustCompile(`^attempt-(\d+)\.jsonl\.gz$`)

// writeCapturedRun persists one Captured run pair (ADR-0094): a uuid-keyed
// meta.json index and a matching events.jsonl.gz payload. Both files are
// written best-effort; if the meta write fails the events file is removed so
// an orphan payload never lacks an index.
func writeCapturedRun(d *Deps, taskSetDir string, sel *Selection, rec *streamRecorder, agent, requestedAgent string, attempt int, outcome, reason string, exitCode int) (string, string, error) {
	dir := capturedRunsDir(taskSetDir)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}

	runID := newRunID()
	end := rec.end
	if end.IsZero() {
		end = rec.now()
	}
	meta := capturedRunMeta{
		RunID:          runID,
		Phase:          "implement",
		TaskSetID:      sel.TaskSetID,
		TaskID:         sel.TaskID,
		TaskFile:       sel.TaskFile,
		StartTime:      rec.start.UTC(),
		EndTime:        end.UTC(),
		Outcome:        outcome,
		Reason:         reason,
		ExitCode:       exitCode,
		Agent:          agent,
		RequestedAgent: requestedAgent,
		Attempt:        attempt,
	}

	eventsPath := filepath.Join(dir, runID+".events.jsonl.gz")
	eventsData, err := encodeCapturedRunEvents(rec)
	if err != nil {
		return "", "", err
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(eventsData); err != nil {
		return "", "", err
	}
	if err := zw.Close(); err != nil {
		return "", "", err
	}
	if err := d.FS.WriteFile(eventsPath, gz.Bytes(), 0o644); err != nil {
		return "", "", err
	}

	metaPath := filepath.Join(dir, runID+".meta.json")
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = d.FS.RemoveAll(eventsPath)
		return "", "", err
	}
	if err := d.FS.WriteFile(metaPath, metaData, 0o644); err != nil {
		_ = d.FS.RemoveAll(eventsPath)
		return "", "", err
	}
	return metaPath, eventsPath, nil
}

// persistAttemptStream writes one Captured run pair best-effort: a storage
// failure is reported on errOut but never fails the implement run. A nil
// recorder (plain-output or custom-command attempt) records nothing.
// Returns the written meta file's path, or "" when nothing was persisted.
func persistAttemptStream(d *Deps, errOut io.Writer, sel *Selection, rec *streamRecorder, agent, requestedAgent string, attempt int, outcome, reason string, exitCode int) string {
	if rec == nil {
		return ""
	}
	metaPath, _, err := writeCapturedRun(d, sel.Manifest.Dir, sel, rec, agent, requestedAgent, attempt, outcome, reason, exitCode)
	if err != nil {
		fmt.Fprintf(errOut, "warning: persist attempt stream for %s/%s: %v\n", sel.TaskSetID, sel.TaskID, err)
		return ""
	}
	return metaPath
}

// collectAllRuns reads every captured run under a task set, both uuid-keyed
// Captured run pairs (ADR-0094) and legacy task-stem gzipped JSONL files
// synthesized into virtual metas. Results are unordered.
func collectAllRuns(d *Deps, taskSetDir string) ([]capturedRun, error) {
	var runs []capturedRun

	// New-format runs: <task-set>/streams/runs/<uuid>.meta.json plus
	// <uuid>.events.jsonl.gz.
	runsDir := capturedRunsDir(taskSetDir)
	if entries, err := d.FS.ReadDir(runsDir); err == nil {
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".meta.json") {
				continue
			}
			run, err := loadCapturedRun(d, runsDir, e.Name())
			if err != nil {
				return nil, fmt.Errorf("load captured run %s: %w", e.Name(), err)
			}
			runs = append(runs, run)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	// Legacy runs: <task-set>/streams/<task-stem>/attempt-NNN.jsonl.gz.
	legacyBase := filepath.Join(taskSetDir, streamsDirName)
	if entries, err := d.FS.ReadDir(legacyBase); err == nil {
		for _, e := range entries {
			if e.Name() == capturedRunsDirName {
				continue
			}
			legacyDir := filepath.Join(legacyBase, e.Name())
			info, err := d.FS.Stat(legacyDir)
			if err != nil || !info.IsDir() {
				continue
			}
			subEntries, err := d.FS.ReadDir(legacyDir)
			if err != nil {
				return nil, err
			}
			// The legacy directory name is the task stem; task files are .md.
			taskFile := e.Name() + ".md"
			for _, se := range subEntries {
				if !attemptStreamNamePattern.MatchString(se.Name()) {
					continue
				}
				run, err := loadLegacyRun(d, filepath.Join(legacyDir, se.Name()), taskFile)
				if err != nil {
					return nil, fmt.Errorf("load legacy run %s: %w", se.Name(), err)
				}
				runs = append(runs, run)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	return runs, nil
}

// listSetRuns returns every captured run under a task set, sorted
// chronologically by start time. At equal timestamps implement runs sort
// before verify runs.
func listSetRuns(d *Deps, taskSetDir string) ([]capturedRun, error) {
	runs, err := collectAllRuns(d, taskSetDir)
	if err != nil {
		return nil, err
	}
	sortRunsChronologically(runs)
	return runs, nil
}

// listTaskRuns returns every implement-phase captured run for one task,
// reading both the uuid-keyed Captured run directory (ADR-0094) and the
// legacy task-stem gzipped JSONL files. Results are merged and ordered by
// start time.
func listTaskRuns(d *Deps, taskSetDir, taskFile string) ([]capturedRun, error) {
	runs, err := collectAllRuns(d, taskSetDir)
	if err != nil {
		return nil, err
	}
	var filtered []capturedRun
	for _, run := range runs {
		if run.meta.TaskFile != taskFile {
			continue
		}
		if run.meta.Phase != "implement" {
			continue
		}
		filtered = append(filtered, run)
	}
	sortRunsChronologically(filtered)
	return filtered, nil
}

// loadCapturedRun reads one Captured run pair from its meta file.
func loadCapturedRun(d *Deps, dir, metaName string) (capturedRun, error) {
	metaPath := filepath.Join(dir, metaName)
	metaData, err := d.FS.ReadFile(metaPath)
	if err != nil {
		return capturedRun{}, err
	}
	var meta capturedRunMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return capturedRun{}, fmt.Errorf("parse meta: %w", err)
	}

	runID := strings.TrimSuffix(metaName, ".meta.json")
	eventsPath := filepath.Join(dir, runID+".events.jsonl.gz")
	eventsData, err := d.FS.ReadFile(eventsPath)
	if err != nil {
		return capturedRun{}, fmt.Errorf("read events: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(eventsData))
	if err != nil {
		return capturedRun{}, fmt.Errorf("decompress events: %w", err)
	}
	jsonl, err := io.ReadAll(zr)
	_ = zr.Close()
	if err != nil {
		return capturedRun{}, fmt.Errorf("decompress events: %w", err)
	}

	var events []streamEventRecord
	for _, line := range bytes.Split(jsonl, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev streamEventRecord
		if err := json.Unmarshal(line, &ev); err != nil {
			return capturedRun{}, fmt.Errorf("parse event: %w", err)
		}
		events = append(events, ev)
	}
	return capturedRun{meta: meta, events: events}, nil
}

// loadLegacyRun reads one legacy attempt-NNN.jsonl.gz file into a capturedRun.
// taskFile is the task's manifest file name (e.g. "01-a.md") used to populate
// the synthesized meta so single-task replay filters include the legacy run.
func loadLegacyRun(d *Deps, path, taskFile string) (capturedRun, error) {
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return capturedRun{}, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return capturedRun{}, err
	}
	jsonl, err := io.ReadAll(zr)
	_ = zr.Close()
	if err != nil {
		return capturedRun{}, err
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
			return capturedRun{}, fmt.Errorf("parse record: %w", err)
		}
		switch probe.Type {
		case "header":
			if err := json.Unmarshal(line, &header); err != nil {
				return capturedRun{}, fmt.Errorf("parse header: %w", err)
			}
			hasHeader = true
		case "footer":
			if err := json.Unmarshal(line, &footer); err != nil {
				return capturedRun{}, fmt.Errorf("parse footer: %w", err)
			}
			hasFooter = true
		case "event":
			var ev streamEventRecord
			if err := json.Unmarshal(line, &ev); err != nil {
				return capturedRun{}, fmt.Errorf("parse event: %w", err)
			}
			events = append(events, ev)
		}
	}
	if !hasHeader {
		return capturedRun{}, fmt.Errorf("missing header record")
	}
	if !hasFooter {
		return capturedRun{}, fmt.Errorf("missing footer record")
	}

	end := header.StartTime.Add(time.Duration(footer.DurationMS) * time.Millisecond)
	stem := strings.TrimSuffix(taskFile, filepath.Ext(taskFile))
	name := filepath.Base(path)
	meta := capturedRunMeta{
		RunID:          fmt.Sprintf("legacy:%s:%s", stem, strings.TrimSuffix(name, ".jsonl.gz")),
		Phase:          "implement",
		TaskID:         stem,
		TaskFile:       taskFile,
		StartTime:      header.StartTime,
		EndTime:        end,
		Outcome:        footer.Outcome,
		Reason:         footer.Reason,
		ExitCode:       footer.ExitCode,
		Agent:          header.Agent,
		RequestedAgent: header.RequestedAgent,
		Attempt:        header.Attempt,
	}
	return capturedRun{meta: meta, events: events, legacyPath: path}, nil
}

// runToHeaderFooter builds the legacy-style header/footer records from a
// capturedRun so existing derivation helpers (deriveAttemptTiming, etc.) keep
// working across both storage layouts.
func runToHeaderFooter(run capturedRun) (streamHeaderRecord, streamFooterRecord) {
	header := streamHeaderRecord{
		Type:           "header",
		Agent:          run.meta.Agent,
		RequestedAgent: run.meta.RequestedAgent,
		Attempt:        run.meta.Attempt,
		StartTime:      run.meta.StartTime,
	}
	footer := streamFooterRecord{
		Type:       "footer",
		Outcome:    run.meta.Outcome,
		DurationMS: run.meta.EndTime.Sub(run.meta.StartTime).Milliseconds(),
		Reason:     run.meta.Reason,
		ExitCode:   run.meta.ExitCode,
	}
	return header, footer
}

// attemptStreamFromRun converts a capturedRun into the replay shape used by
// StreamWith.
func attemptStreamFromRun(run capturedRun) AttemptStream {
	header, footer := runToHeaderFooter(run)
	return AttemptStream{
		Timing: deriveAttemptTiming(header, footer, run.events),
		Events: renderStreamEvents(run.meta.Agent, run.events),
	}
}

// attemptTimingFromRun converts a capturedRun into the timing summary used by
// the timing lens and inline breakdown.
func attemptTimingFromRun(run capturedRun) AttemptTiming {
	header, footer := runToHeaderFooter(run)
	return deriveAttemptTiming(header, footer, run.events)
}

// isLegacyRun reports whether a capturedRun was loaded from a legacy
// attempt-NNN.jsonl.gz file.
func isLegacyRun(run capturedRun) bool {
	return run.legacyPath != ""
}

// rawRunFileName returns the storage filename to display in raw stream
// delimiters for a capturedRun.
func rawRunFileName(run capturedRun) string {
	if isLegacyRun(run) {
		return filepath.Base(run.legacyPath)
	}
	return run.meta.RunID + ".events.jsonl.gz"
}

// rawRunJSONL returns the raw JSONL bytes to emit for a capturedRun in raw
// stream replay. Legacy runs replay the full gzipped JSONL document; new
// Captured runs replay the events payload only.
func rawRunJSONL(d *Deps, taskSetDir string, run capturedRun) ([]byte, error) {
	var path string
	if isLegacyRun(run) {
		path = run.legacyPath
	} else {
		path = filepath.Join(capturedRunsDir(taskSetDir), run.meta.RunID+".events.jsonl.gz")
	}
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	jsonl, err := io.ReadAll(zr)
	_ = zr.Close()
	if err != nil {
		return nil, err
	}
	return jsonl, nil
}
