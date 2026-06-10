package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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

	jsonl, err := encodeAttemptStream(rec, "claude", 2, "completed")
	if err != nil {
		t.Fatal(err)
	}
	records := decodeStreamRecords(t, jsonl)
	if len(records) != 5 {
		t.Fatalf("records = %d, want header + 3 events + footer:\n%s", len(records), jsonl)
	}

	header := records[0]
	if header["type"] != "header" || header["agent"] != "claude" || header["attempt"] != float64(2) {
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

func TestStreamRecorderUnaffectedByRenderer(t *testing.T) {
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	lines := "{\"type\":\"assistant\"}\nnot json stderr noise\n"

	record := func(render lineRenderer) []streamEventRecord {
		var capture bytes.Buffer
		rec := newStreamRecorder(&capture, fakeClock(start, 50*time.Millisecond))
		var w io.Writer = rec
		if render != nil {
			w = newLiveRenderWriter(io.Discard, rec, render)
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

func TestWriteAttemptStreamNumberingIsMonotonic(t *testing.T) {
	d := realFSDeps()
	dir := filepath.Join(t.TempDir(), "streams", "01-a")

	first, err := writeAttemptStream(d, dir, []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "attempt-001.jsonl.gz" {
		t.Fatalf("first = %s", first)
	}

	second, err := writeAttemptStream(d, dir, []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "attempt-002.jsonl.gz" {
		t.Fatalf("second = %s", second)
	}

	// Numbering continues past the highest persisted attempt, never reuses gaps.
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "attempt-007.jsonl.gz"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := writeAttemptStream(d, dir, []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(third) != "attempt-008.jsonl.gz" {
		t.Fatalf("third = %s", third)
	}
}

func TestWriteAttemptStreamBumpsNumberOnCollision(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "attempt-001.jsonl.gz"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// A stale directory listing simulates the cross-worktree race: the scan
	// misses attempt-001, so the exclusive open must collide and bump.
	real := deps.NewRealFileSystem()
	d := &Deps{FS: &deps.MockFileSystem{
		MkdirAllFunc: real.MkdirAll,
		ReadDirFunc:  func(string) ([]os.DirEntry, error) { return nil, nil },
	}}

	path, err := writeAttemptStream(d, dir, []byte("{}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "attempt-002.jsonl.gz" {
		t.Fatalf("path = %s, want collision bump to attempt-002", path)
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

func readAttemptStream(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip open %s: %v", path, err)
	}
	defer zr.Close()
	jsonl, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return decodeStreamRecords(t, jsonl)
}

func demoStreamDir(env *execFixture) string {
	return filepath.Join(env.demoDir(), "streams", "01-a")
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

	records := readAttemptStream(t, filepath.Join(demoStreamDir(env), "attempt-001.jsonl.gz"))
	header := records[0]
	if header["type"] != "header" || header["agent"] != "claude" || header["attempt"] != float64(1) {
		t.Fatalf("header = %v", header)
	}
	var raws []string
	for _, rec := range records[1 : len(records)-1] {
		if rec["type"] != "event" {
			t.Fatalf("middle record not event: %v", rec)
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
	footer := records[len(records)-1]
	if footer["type"] != "footer" || footer["outcome"] != "completed" {
		t.Fatalf("footer = %v", footer)
	}
	if _, ok := footer["duration_ms"].(float64); !ok {
		t.Fatalf("footer missing duration_ms: %v", footer)
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

	for i, name := range []string{"attempt-001.jsonl.gz", "attempt-002.jsonl.gz"} {
		records := readAttemptStream(t, filepath.Join(demoStreamDir(env), name))
		if got := records[0]["attempt"]; got != float64(i+1) {
			t.Fatalf("%s header attempt = %v, want %d", name, got, i+1)
		}
		footer := records[len(records)-1]
		if footer["outcome"] != "failed" {
			t.Fatalf("%s footer = %v", name, footer)
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
