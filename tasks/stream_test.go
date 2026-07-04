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
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

// fakeClock returns a now() that advances by step on every call after the first.
func fakeClock(start time.Time, step time.Duration) func() time.Time {
	current := start
	first := true
	return func() time.Time {
		if first {
			first = false
			return current
		}
		current = current.Add(step)
		return current
	}
}

func decodeStreamRecords(t *testing.T, jsonl []byte) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(jsonl)), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode record %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}

// demoRunsDir returns the Captured runs directory for the demo task set's
// first task (01-a.md).
func demoRunsDir(env *execFixture) string {
	return capturedRunsDir(env.demoDir())
}

// findRunFiles lists the captured run files in dir and returns the single
// meta/events pair. It fails the test if the count is not exactly one pair.
func findRunFiles(t *testing.T, dir string) (metaPath, eventsPath string) {
	t.Helper()
	pairs := listRunFilePairs(t, dir)
	if len(pairs) != 1 {
		t.Fatalf("want 1 meta + 1 events file, got %d pairs in %s", len(pairs), dir)
	}
	return pairs[0].meta, pairs[0].events
}

// runFilePair is one captured run's meta and events paths.
type runFilePair struct {
	meta   string
	events string
	metaData capturedRunMeta
}

// listRunFilePairs returns every captured run pair in dir, sorted by meta
// start_time so attempt ordering is stable.
func listRunFilePairs(t *testing.T, dir string) []runFilePair {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read runs dir: %v", err)
	}
	byBase := map[string]*runFilePair{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".meta.json") {
			base := strings.TrimSuffix(e.Name(), ".meta.json")
			if byBase[base] == nil {
				byBase[base] = &runFilePair{}
			}
			byBase[base].meta = filepath.Join(dir, e.Name())
			byBase[base].metaData = readCapturedRunMeta(t, byBase[base].meta)
		} else if strings.HasSuffix(e.Name(), ".events.jsonl.gz") {
			base := strings.TrimSuffix(e.Name(), ".events.jsonl.gz")
			if byBase[base] == nil {
				byBase[base] = &runFilePair{}
			}
			byBase[base].events = filepath.Join(dir, e.Name())
		}
	}
	var pairs []runFilePair
	for _, p := range byBase {
		if p.meta == "" || p.events == "" {
			t.Fatalf("incomplete run pair: meta=%q events=%q", p.meta, p.events)
		}
		pairs = append(pairs, *p)
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return pairs[i].metaData.StartTime.Before(pairs[j].metaData.StartTime)
	})
	return pairs
}

// assertNoLegacyAttemptFiles ensures no attempt-NNN.jsonl.gz files exist under
// the task's legacy task-stem stream directory.
func assertNoLegacyAttemptFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read legacy stream dir: %v", err)
	}
	for _, e := range entries {
		if attemptStreamNamePattern.MatchString(e.Name()) {
			t.Fatalf("unexpected legacy attempt file: %s", filepath.Join(dir, e.Name()))
		}
	}
}

// readCapturedRunMeta reads and parses a captured run meta file.
func readCapturedRunMeta(t *testing.T, path string) capturedRunMeta {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read meta %s: %v", path, err)
	}
	var meta capturedRunMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse meta %s: %v", path, err)
	}
	return meta
}

// readCapturedRunEvents decompresses and decodes a captured run events file.
func readCapturedRunEvents(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open events %s: %v", path, err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip open %s: %v", path, err)
	}
	defer zr.Close()
	jsonl, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read events %s: %v", path, err)
	}
	return decodeStreamRecords(t, jsonl)
}

func TestStreamRecorderRecordFormat(t *testing.T) {
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	var capture bytes.Buffer
	rec := newStreamRecorder(&capture, fakeClock(start, 100*time.Millisecond))

	// Two events split across writes plus an unterminated trailing line.
	if _, err := rec.Write([]byte("{\"type\":\"system\"}\n{\"type\":\"assist")); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Write([]byte("ant\"}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Write([]byte("trailing stderr")); err != nil {
		t.Fatal(err)
	}
	rec.finish()

	if got := capture.String(); got != "{\"type\":\"system\"}\n{\"type\":\"assistant\"}\ntrailing stderr" {
		t.Fatalf("capture distorted: %q", got)
	}

	jsonl, err := encodeAttemptStream(rec, "claude", "claude --model opus4.8", 2, "completed", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	records := decodeStreamRecords(t, jsonl)
	if len(records) != 5 {
		t.Fatalf("records = %d, want header + 3 events + footer:\n%s", len(records), jsonl)
	}

	header := records[0]
	if header["type"] != "header" || header["agent"] != "claude" || header["requested_agent"] != "claude --model opus4.8" || header["attempt"] != float64(2) {
		t.Fatalf("header = %v", header)
	}
	if _, err := time.Parse(time.RFC3339, header["start_time"].(string)); err != nil {
		t.Fatalf("header start_time: %v", err)
	}

	events := records[1:4]
	wantRaw := []string{"{\"type\":\"system\"}", "{\"type\":\"assistant\"}", "trailing stderr"}
	for i, ev := range events {
		if ev["type"] != "event" {
			t.Fatalf("event[%d] type = %v", i, ev["type"])
		}
		if ev["raw"] != wantRaw[i] {
			t.Fatalf("event[%d] raw = %q, want %q", i, ev["raw"], wantRaw[i])
		}
	}
	// Arrival times are relative to attempt start and non-decreasing.
	if events[0]["at_ms"].(float64) <= 0 {
		t.Fatalf("event[0] at_ms = %v, want > 0", events[0]["at_ms"])
	}
	if events[1]["at_ms"].(float64) < events[0]["at_ms"].(float64) ||
		events[2]["at_ms"].(float64) < events[1]["at_ms"].(float64) {
		t.Fatalf("at_ms not monotonic: %v", events)
	}

	footer := records[4]
	if footer["type"] != "footer" || footer["outcome"] != "completed" {
		t.Fatalf("footer = %v", footer)
	}
	if footer["duration_ms"].(float64) < events[2]["at_ms"].(float64) {
		t.Fatalf("footer duration %v shorter than last event %v", footer["duration_ms"], events[2]["at_ms"])
	}
}

func TestEncodeAttemptStreamFooterCarriesReasonAndExitCode(t *testing.T) {
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	encodeFooter := func(outcome, reason string, exitCode int) map[string]any {
		var capture bytes.Buffer
		rec := newStreamRecorder(&capture, fakeClock(start, 100*time.Millisecond))
		rec.finish()
		jsonl, err := encodeAttemptStream(rec, "claude", "claude", 1, outcome, reason, exitCode)
		if err != nil {
			t.Fatal(err)
		}
		records := decodeStreamRecords(t, jsonl)
		return records[len(records)-1]
	}

	// A failure footer carries the structured reason and exit code beside outcome.
	failed := encodeFooter(streamOutcomeFailed, "missing TASK_COMPLETE sentinel", 0)
	if failed["type"] != "footer" || failed["outcome"] != streamOutcomeFailed {
		t.Fatalf("footer = %v", failed)
	}
	if failed["reason"] != "missing TASK_COMPLETE sentinel" {
		t.Fatalf("footer reason = %v", failed["reason"])
	}

	exited := encodeFooter(streamOutcomeFailed, "agent exited with status 3", 3)
	if exited["reason"] != "agent exited with status 3" || exited["exit_code"].(float64) != 3 {
		t.Fatalf("footer = %v, want reason + exit_code 3", exited)
	}

	// A non-failure footer omits both fields, so the prior shape is unchanged.
	completed := encodeFooter(streamOutcomeCompleted, "", 0)
	if _, ok := completed["reason"]; ok {
		t.Fatalf("completed footer should omit reason: %v", completed)
	}
	if _, ok := completed["exit_code"]; ok {
		t.Fatalf("completed footer should omit exit_code: %v", completed)
	}
}

func TestStreamRecorderUnaffectedByRenderer(t *testing.T) {
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	lines := "{\"type\":\"assistant\"}\nnot json stderr noise\n"

	record := func(render lineRenderer) []streamEventRecord {
		var capture bytes.Buffer
		rec := newStreamRecorder(&capture, fakeClock(start, 50*time.Millisecond))
		var w io.Writer = rec
		if render != nil {
			w = newLiveRenderWriter(io.Discard, rec, render, fakeClock(start, 50*time.Millisecond))
		}
		if _, err := w.Write([]byte(lines)); err != nil {
			t.Fatal(err)
		}
		rec.finish()
		return rec.events
	}

	bare := record(nil)
	// A renderer that handles nothing must not change what is recorded.
	rendered := record(func(line []byte) (string, bool) { return "", false })
	if len(bare) != len(rendered) || len(bare) != 2 {
		t.Fatalf("events: bare %d, rendered %d, want 2", len(bare), len(rendered))
	}
	for i := range bare {
		if bare[i].Raw != rendered[i].Raw {
			t.Fatalf("event[%d] raw diverged: %q vs %q", i, bare[i].Raw, rendered[i].Raw)
		}
	}
}

func realFSDeps() *Deps {
	return &Deps{FS: deps.NewRealFileSystem(), Git: deps.NewRealGit()}
}

func TestWriteCapturedRunCreatesMetaAndEventsPair(t *testing.T) {
	d := realFSDeps()
	taskSetDir := t.TempDir()
	sel := &Selection{
		TaskSetID: "demo",
		TaskID:    "01-a",
		TaskFile:  "01-a.md",
		Manifest:  &Manifest{Dir: taskSetDir},
	}
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	rec := newStreamRecorder(io.Discard, fakeClock(start, 100*time.Millisecond))
	if _, err := rec.Write([]byte(`{"type":"system"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	rec.finish()

	metaPath, eventsPath, err := writeCapturedRun(d, taskSetDir, "implement", sel.TaskSetID, sel.TaskID, sel.TaskFile, rec, "claude", "claude --model opus", 1, streamOutcomeCompleted, "", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(metaPath, ".meta.json") {
		t.Fatalf("meta path = %s", metaPath)
	}
	if !strings.HasSuffix(eventsPath, ".events.jsonl.gz") {
		t.Fatalf("events path = %s", eventsPath)
	}
	if filepath.Dir(metaPath) != filepath.Dir(eventsPath) {
		t.Fatalf("meta and events not in same dir: %s vs %s", metaPath, eventsPath)
	}

	meta := readCapturedRunMeta(t, metaPath)
	if meta.RunID == "" {
		t.Fatalf("meta missing run_id")
	}
	if meta.Phase != "implement" {
		t.Fatalf("meta phase = %q, want implement", meta.Phase)
	}
	if meta.TaskSetID != "demo" || meta.TaskID != "01-a" || meta.TaskFile != "01-a.md" {
		t.Fatalf("meta task identity = %+v", meta)
	}
	if meta.Agent != "claude" || meta.RequestedAgent != "claude --model opus" {
		t.Fatalf("meta agent = %+v", meta)
	}
	if meta.Attempt != 1 {
		t.Fatalf("meta attempt = %d, want 1", meta.Attempt)
	}
	if meta.Outcome != streamOutcomeCompleted {
		t.Fatalf("meta outcome = %q", meta.Outcome)
	}
	if !meta.StartTime.Equal(rec.start.UTC()) {
		t.Fatalf("meta start_time = %v, want %v", meta.StartTime, rec.start.UTC())
	}
	if meta.EndTime.Before(meta.StartTime) {
		t.Fatalf("meta end_time %v before start_time %v", meta.EndTime, meta.StartTime)
	}

	events := readCapturedRunEvents(t, eventsPath)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0]["type"] != "event" || events[0]["raw"] != `{"type":"system"}` {
		t.Fatalf("event = %v", events[0])
	}

	// Two runs for the same task get distinct uuids.
	metaPath2, eventsPath2, err := writeCapturedRun(d, taskSetDir, "implement", sel.TaskSetID, sel.TaskID, sel.TaskFile, rec, "claude", "claude --model opus", 2, streamOutcomeFailed, "missing sentinel", 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if metaPath == metaPath2 || eventsPath == eventsPath2 {
		t.Fatalf("second run reused paths")
	}
	meta2 := readCapturedRunMeta(t, metaPath2)
	if meta2.Attempt != 2 || meta2.Outcome != streamOutcomeFailed || meta2.Reason != "missing sentinel" || meta2.ExitCode != 1 {
		t.Fatalf("meta2 = %+v", meta2)
	}
}

func TestListTaskRunsMergesNewAndLegacyLayouts(t *testing.T) {
	d := realFSDeps()
	taskSetDir := t.TempDir()

	writeNewRun := func(taskFile string, attempt int, start time.Time, outcome string) {
		t.Helper()
		stem := strings.TrimSuffix(taskFile, filepath.Ext(taskFile))
		sel := &Selection{
			TaskSetID: "demo",
			TaskID:    stem,
			TaskFile:  taskFile,
			Manifest:  &Manifest{Dir: taskSetDir},
		}
		rec := newStreamRecorder(io.Discard, func() time.Time { return start })
		if _, err := rec.Write([]byte(`{"type":"system"}` + "\n")); err != nil {
			t.Fatal(err)
		}
		rec.finish()
		if _, _, err := writeCapturedRun(d, taskSetDir, "implement", sel.TaskSetID, sel.TaskID, sel.TaskFile, rec, "claude", "claude", attempt, outcome, "", 0, "", ""); err != nil {
			t.Fatal(err)
		}
	}

	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// New-format run for 01-a at base + 5 minutes.
	writeNewRun("01-a.md", 1, base.Add(5*time.Minute), streamOutcomeCompleted)

	// Legacy run for 01-a at base (earlier than the new-format run).
	legacyDir := taskStreamDir(taskSetDir, "01-a.md")
	writeTimingStreamWithEvents(t, legacyDir, "attempt-001.jsonl.gz", "claude", "", 1, base, "failed", 30_000, []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
	})

	// Legacy run for 02-b should not appear when filtering 01-a.
	writeTimingStream(t, taskStreamDir(taskSetDir, "02-b.md"), "attempt-001.jsonl.gz", "claude", 1, base.Add(10*time.Minute), "completed", 60_000)

	runs, err := listTaskRuns(d, taskSetDir, "01-a.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 runs for 01-a, got %d", len(runs))
	}

	// Ordered by start time: legacy first, then new-format.
	if !runs[0].meta.StartTime.Equal(base) {
		t.Fatalf("run[0] start = %v, want %v", runs[0].meta.StartTime, base)
	}
	if !runs[1].meta.StartTime.Equal(base.Add(5*time.Minute)) {
		t.Fatalf("run[1] start = %v", runs[1].meta.StartTime)
	}

	// Legacy run synthesized meta carries the task file and implement phase.
	legacy := runs[0].meta
	if legacy.Phase != "implement" {
		t.Fatalf("legacy phase = %q, want implement", legacy.Phase)
	}
	if legacy.TaskFile != "01-a.md" {
		t.Fatalf("legacy task_file = %q, want 01-a.md", legacy.TaskFile)
	}
	if !strings.HasPrefix(legacy.RunID, "legacy:01-a:attempt-") {
		t.Fatalf("legacy run_id = %q", legacy.RunID)
	}
	if legacy.Outcome != "failed" {
		t.Fatalf("legacy outcome = %q, want failed", legacy.Outcome)
	}

	// New-format run meta fields.
	newRun := runs[1].meta
	if newRun.Phase != "implement" || newRun.TaskFile != "01-a.md" || newRun.Outcome != streamOutcomeCompleted {
		t.Fatalf("new run meta = %+v", newRun)
	}
}

// installClaudeStreamAgent puts a fake `claude` on PATH that emits
// claude-stream-json events. With tick set it checks the task's acceptance
// boxes and ends with the completion sentinel; otherwise the attempt fails
// assessment while still finishing normally.
func installClaudeStreamAgent(t *testing.T, root string, tick bool) {
	t.Helper()
	dir := filepath.Join(root, ".agent-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("for arg in \"$@\"; do LAST=$arg; done\n")
	b.WriteString("TASK=$(printf '%s' \"$LAST\" | sed -n 's|^You are implementing the task at: ||p' | head -1)\n")
	if tick {
		b.WriteString("if [ -n \"$TASK\" ] && [ -f \"$TASK\" ]; then\n")
		b.WriteString("  sed -i '' 's/- \\[ \\]/- [x]/g' \"$TASK\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$TASK\"\n")
		b.WriteString("fi\n")
	}
	b.WriteString(`printf '%s\n' '{"type":"system","subtype":"init"}'` + "\n")
	b.WriteString(`printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}'` + "\n")
	if tick {
		b.WriteString(`printf '%s\n' '{"type":"result","subtype":"success","result":"SUMMARY_START\ndid the work\nSUMMARY_END\nTASK_COMPLETE"}'` + "\n")
	} else {
		b.WriteString(`printf '%s\n' '{"type":"result","subtype":"success","result":"no sentinel here"}'` + "\n")
	}
	writeFile(t, filepath.Join(dir, "claude"), b.String())
	if err := os.Chmod(filepath.Join(dir, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}


func TestRunTaskStructuredAttemptWritesStream(t *testing.T) {
	env := setupExecutorFixture(t, false)
	installClaudeStreamAgent(t, env.root, true)

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	if _, err := RunTaskWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatal(err)
	}
	assertTaskDone(t, env, "01-a")

	metaPath, eventsPath := findRunFiles(t, demoRunsDir(env))
	meta := readCapturedRunMeta(t, metaPath)
	if meta.Phase != "implement" {
		t.Fatalf("meta phase = %q, want implement", meta.Phase)
	}
	if meta.TaskSetID != "demo" || meta.TaskID != "01-a" || meta.TaskFile != "01-a.md" {
		t.Fatalf("meta task identity = %+v", meta)
	}
	if meta.Agent != "claude" {
		t.Fatalf("meta agent = %q", meta.Agent)
	}
	if meta.RequestedAgent == "" {
		t.Fatalf("meta requested_agent empty")
	}
	if meta.Attempt != 1 {
		t.Fatalf("meta attempt = %d, want 1", meta.Attempt)
	}
	if meta.Outcome != "completed" {
		t.Fatalf("meta outcome = %q", meta.Outcome)
	}
	if meta.StartTime.IsZero() || meta.EndTime.IsZero() || meta.EndTime.Before(meta.StartTime) {
		t.Fatalf("meta times invalid: %v -> %v", meta.StartTime, meta.EndTime)
	}

	events := readCapturedRunEvents(t, eventsPath)
	var raws []string
	for _, rec := range events {
		if rec["type"] != "event" {
			t.Fatalf("record not event: %v", rec)
		}
		if _, ok := rec["at_ms"].(float64); !ok {
			t.Fatalf("event missing at_ms: %v", rec)
		}
		raws = append(raws, rec["raw"].(string))
	}
	joined := strings.Join(raws, "\n")
	for _, want := range []string{`{"type":"system","subtype":"init"}`, `"type":"assistant"`, `"type":"result"`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("events missing %q:\n%s", want, joined)
		}
	}
}

func TestRunTaskFailedAttemptsWriteOneStreamEach(t *testing.T) {
	env := setupExecutorFixture(t, false)
	installClaudeStreamAgent(t, env.root, false)

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.MaxTries = 2
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)

	pairs := listRunFilePairs(t, demoRunsDir(env))
	if len(pairs) != 2 {
		t.Fatalf("want 2 captured run pairs, got %d", len(pairs))
	}
	assertNoLegacyAttemptFiles(t, taskStreamDir(env.demoDir(), "01-a.md"))

	for i, p := range pairs {
		meta := readCapturedRunMeta(t, p.meta)
		if meta.Attempt != i+1 {
			t.Fatalf("pair[%d] attempt = %d, want %d", i, meta.Attempt, i+1)
		}
		if meta.Outcome != "failed" {
			t.Fatalf("pair[%d] outcome = %q, want failed", i, meta.Outcome)
		}
		if meta.Phase != "implement" {
			t.Fatalf("pair[%d] phase = %q, want implement", i, meta.Phase)
		}
		if meta.TaskFile != "01-a.md" {
			t.Fatalf("pair[%d] task_file = %q", i, meta.TaskFile)
		}
		// Events payload carries only raw event lines, no inline header/footer.
		events := readCapturedRunEvents(t, p.events)
		for _, rec := range events {
			if rec["type"] != "event" {
				t.Fatalf("events contain non-event record: %v", rec)
			}
		}
	}
}

func TestRunTaskCustomCommandWritesNoStream(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "custom run"})

	if _, err := RunTaskWith(env.deps(), nil, nil, env.runOpts(true, agent)); err != nil {
		t.Fatal(err)
	}
	assertTaskDone(t, env, "01-a")
	if _, err := os.Stat(filepath.Join(env.demoDir(), "streams")); !os.IsNotExist(err) {
		t.Fatalf("custom-command attempt wrote a stream: %v", err)
	}
}

func TestRunTaskPlainOutputWritesNoStream(t *testing.T) {
	env := setupExecutorFixture(t, false)
	// In text mode the claude preset gets no structured-output flags, so the
	// fake emits the sentinel as plain text.
	dir := filepath.Join(env.root, ".agent-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do LAST=$arg; done\n" +
		"TASK=$(printf '%s' \"$LAST\" | sed -n 's|^You are implementing the task at: ||p' | head -1)\n" +
		"if [ -n \"$TASK\" ] && [ -f \"$TASK\" ]; then\n" +
		"  sed -i '' 's/- \\[ \\]/- [x]/g' \"$TASK\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$TASK\"\n" +
		"fi\n" +
		"printf 'SUMMARY_START\\nplain run\\nSUMMARY_END\\nTASK_COMPLETE\\n'\n"
	writeFile(t, filepath.Join(dir, "claude"), script)
	if err := os.Chmod(filepath.Join(dir, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.AgentOutput = AgentOutputText
	if _, err := RunTaskWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatal(err)
	}
	assertTaskDone(t, env, "01-a")
	if _, err := os.Stat(filepath.Join(env.demoDir(), "streams")); !os.IsNotExist(err) {
		t.Fatalf("plain-output attempt wrote a stream: %v", err)
	}
}

// installClaudeHangingAgent puts a fake `claude` on PATH that emits structured
// events, touches the slow-agent start sentinel, then hangs until killed. With
// trapTerm it ignores SIGTERM so only SIGKILL escalation ends it.
func installClaudeHangingAgent(t *testing.T, root string, trapTerm bool) {
	t.Helper()
	dir := filepath.Join(root, ".agent-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(slowAgentSentinel(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	if trapTerm {
		b.WriteString("trap '' TERM\n")
	}
	b.WriteString(`printf '%s\n' '{"type":"system","subtype":"init"}'` + "\n")
	b.WriteString(`printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}'` + "\n")
	fmt.Fprintf(&b, ": > %q\n", slowAgentSentinel(root))
	b.WriteString("while true; do sleep 0.05; done\n")
	writeFile(t, filepath.Join(dir, "claude"), b.String())
	if err := os.Chmod(filepath.Join(dir, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// assertKilledStreamFinalized checks that a killed single-attempt run wrote
// exactly one new-format captured run pair whose meta carries the kill outcome
// and whose events payload is a valid gzip.
func assertKilledStreamFinalized(t *testing.T, env *execFixture, outcome string) {
	t.Helper()
	metaPath, eventsPath := findRunFiles(t, demoRunsDir(env))
	meta := readCapturedRunMeta(t, metaPath)
	if meta.Agent != "claude" {
		t.Fatalf("meta agent = %q, want claude", meta.Agent)
	}
	if meta.Outcome != outcome {
		t.Fatalf("meta outcome = %q, want %q", meta.Outcome, outcome)
	}
	if meta.Phase != "implement" {
		t.Fatalf("meta phase = %q, want implement", meta.Phase)
	}
	assertNoLegacyAttemptFiles(t, taskStreamDir(env.demoDir(), "01-a.md"))

	// Decompressing the events file validates the gzip trailer; a truncated
	// file would fail here.
	events := readCapturedRunEvents(t, eventsPath)
	for _, rec := range events {
		if rec["type"] != "event" {
			t.Fatalf("events contain non-event record: %v", rec)
		}
	}
}

func TestRunTaskTimeoutFinalizesStream(t *testing.T) {
	env := setupExecutorFixture(t, false)
	installClaudeHangingAgent(t, env.root, false)

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.Timeout = 500 * time.Millisecond
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}

	assertKilledStreamFinalized(t, env, streamOutcomeTimedOut)
}

func TestRunTaskInterruptFinalizesStream(t *testing.T) {
	env := setupExecutorFixture(t, false)
	installClaudeHangingAgent(t, env.root, false)

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.Timeout = time.Minute
	signalOwnPidWhenAgentStarts(t, env.root)

	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitInterrupted)

	assertKilledStreamFinalized(t, env, streamOutcomeInterrupted)
}

func TestRunTaskInterruptSigkillEscalationFinalizesStream(t *testing.T) {
	env := setupExecutorFixture(t, false)
	// The agent ignores SIGTERM, so only the SIGKILL escalation after the
	// grace period ends it.
	installClaudeHangingAgent(t, env.root, true)
	old := signalGracePeriod
	signalGracePeriod = 200 * time.Millisecond
	t.Cleanup(func() { signalGracePeriod = old })

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.Timeout = time.Minute
	signalOwnPidWhenAgentStarts(t, env.root)

	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitInterrupted)

	assertKilledStreamFinalized(t, env, streamOutcomeInterrupted)
}

func TestRunTaskQuotaPauseFinalizesStream(t *testing.T) {
	env := setupExecutorFixture(t, false)
	installClaudeQuotaAgent(t, env.root)

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	result, err := RunTaskWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused {
		t.Fatalf("result = %#v", result)
	}

	assertKilledStreamFinalized(t, env, streamOutcomeQuotaPaused)
}

func TestRunTaskKillPathStreamFailureKeepsExitBehavior(t *testing.T) {
	env := setupExecutorFixture(t, false)
	installClaudeHangingAgent(t, env.root, false)
	// A regular file where streams/ must go makes the kill-path finalization fail.
	writeFile(t, filepath.Join(env.demoDir(), "streams"), "not a directory")

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.Timeout = 500 * time.Millisecond
	var errBuf bytes.Buffer
	opts.ConfirmOut = &errBuf
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("finalization failure changed the exit: %v", err)
	}
	if !strings.Contains(errBuf.String(), "persist attempt stream") {
		t.Fatalf("storage failure not reported:\n%s", errBuf.String())
	}
}

func TestRunTaskStreamWriteFailureDoesNotFailRun(t *testing.T) {
	env := setupExecutorFixture(t, false)
	installClaudeStreamAgent(t, env.root, true)
	// A regular file where streams/ must go makes every stream write fail.
	writeFile(t, filepath.Join(env.demoDir(), "streams"), "not a directory")

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	var errBuf bytes.Buffer
	opts.ConfirmOut = &errBuf
	if _, err := RunTaskWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatalf("storage failure failed the run: %v", err)
	}
	assertTaskDone(t, env, "01-a")
	if !strings.Contains(errBuf.String(), "persist attempt stream") {
		t.Fatalf("storage failure not reported:\n%s", errBuf.String())
	}
}
