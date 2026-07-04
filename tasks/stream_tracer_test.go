package tasks

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func streamFixture(t *testing.T) *execFixture {
	t.Helper()
	return setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
}

func TestStreamWholeSetSpansAttemptsOrderedByStartTime(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")

	claudeEvents := []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init","model":"claude-sonnet-4-20250514"}`},
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"Let me start by reading the file."}]}}`},
		{Type: "event", AtMS: 1500, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"file_path":"/tmp/test.go"}}]}}`},
		{Type: "event", AtMS: 2500, Raw: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"text","text":"file contents"}]}]}}`},
		{Type: "event", AtMS: 3000, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"Now I'll make the changes."}]}}`},
	}

	writeTimingStreamWithEvents(t, streamDir, "attempt-001.jsonl.gz", "claude", "", 1, base.Add(10*time.Minute), "failed", 45_000, claudeEvents)
	writeTimingStreamWithEvents(t, streamDir, "attempt-002.jsonl.gz", "claude", "", 2, base, "timed_out", 83_000, claudeEvents)
	writeTimingStream(t, taskStreamDir(env.demoDir(), "02-b.md"), "attempt-001.jsonl.gz", "claude", 1, base.Add(20*time.Minute), "completed", 500)

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskSetID != "demo" {
		t.Fatalf("task set = %q", result.TaskSetID)
	}
	if len(result.Tasks) != 2 || result.Tasks[0].TaskID != "01-a" || result.Tasks[1].TaskID != "02-b" {
		t.Fatalf("tasks = %#v", result.Tasks)
	}

	attempts := result.Tasks[0].Attempts
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want all stored attempts", len(attempts))
	}

	// Verify chronological order
	if attempts[0].Timing.Start.After(attempts[1].Timing.Start) {
		t.Fatalf("attempts not ordered by start time")
	}

	// Verify events were parsed
	if len(attempts[0].Events) == 0 {
		t.Fatalf("no events parsed for attempt")
	}
}

func TestStreamSingleTaskTarget(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	
	claudeEvents := []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init","model":"claude-sonnet-4-20250514"}`},
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`},
		{Type: "event", AtMS: 2000, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"ls -la"}}]}}`},
		{Type: "event", AtMS: 3000, Raw: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"text","text":"file1.txt\nfile2.txt"}]}]}}`},
	}

	writeTimingStreamWithEvents(t, taskStreamDir(env.demoDir(), "02-b.md"), "attempt-001.jsonl.gz", "claude", "", 1, base, "completed", 60_000, claudeEvents)
	writeTimingStream(t, taskStreamDir(env.demoDir(), "01-a.md"), "attempt-001.jsonl.gz", "claude", 1, base, "failed", 30_000)

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/02-b.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].TaskID != "02-b" {
		t.Fatalf("tasks = %#v, want only 02-b", result.Tasks)
	}
	if len(result.Tasks[0].Attempts) != 1 || result.Tasks[0].Attempts[0].Timing.Outcome != "completed" {
		t.Fatalf("attempts = %#v", result.Tasks[0].Attempts)
	}

	// Verify events were parsed correctly
	events := result.Tasks[0].Attempts[0].Events
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}

	// Check system init event
	if events[0].Type != "system" || !strings.Contains(events[0].Text, "init") {
		t.Fatalf("first event should be system init, got %+v", events[0])
	}

	// Check assistant text event
	if events[1].Type != "assistant" || events[1].Text != "Hello world" {
		t.Fatalf("second event should be assistant text, got %+v", events[1])
	}

	// Check tool_use event
	if events[2].Type != "tool_use" || events[2].ToolName != "Bash" {
		t.Fatalf("third event should be tool_use Bash, got %+v", events[2])
	}
}

func TestStreamEmptyCase(t *testing.T) {
	env := streamFixture(t)

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should have tasks but no attempts
	if len(result.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result.Tasks))
	}
	for _, task := range result.Tasks {
		if len(task.Attempts) != 0 {
			t.Fatalf("task %s should have no attempts, got %d", task.TaskID, len(task.Attempts))
		}
	}

	// Render should show "no captured attempt streams"
	var buf bytes.Buffer
	RenderStream(&buf, result, RenderStreamOptions{})
	out := buf.String()

	if !strings.Contains(out, "no captured attempt streams for demo") {
		t.Fatalf("output should show no captured streams message:\n%s", out)
	}
}

func TestStreamRejectsInvalidTargets(t *testing.T) {
	env := streamFixture(t)

	cases := map[string]struct {
		target string
		code   int
	}{
		"unknown set":       {"missing", ExitNoRunnable},
		"unknown task file": {"demo/99-z.md", ExitNoRunnable},
		"bare filename":     {"01-a.md", ExitSetup},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := StreamWith(env.deps(), nil, nil, StreamOptions{
				ResolveInput: ResolveInput{CWD: env.root},
				Target:       tc.target,
			})
			assertExitCode(t, err, tc.code)
		})
	}
}

func TestRenderStreamShowsTimingHeaderAndEventReplay(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	result := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptStream{
					{
						Timing: AttemptTiming{
							Agent:       "claude",
							ActualModel: "claude-sonnet-4-20250514",
							Start:       base,
							Outcome:     "completed",
							Duration:    90 * time.Second,
							Tools: []ToolTiming{
								{Name: "Bash", Count: 2, Total: 10 * time.Second},
							},
							Model: 80 * time.Second,
						},
						Events: []StreamEvent{
							{AtMS: 5, Type: "system", Text: "system init (model: claude-sonnet-4-20250514)"},
							{AtMS: 100, Type: "assistant", Text: "Let me read the file"},
							{AtMS: 1500, Type: "tool_use", ToolName: "Read", ToolArgs: `{"file_path":"/tmp/test.go"}`},
							{AtMS: 2500, Type: "tool_result", Text: "file contents here"},
							{AtMS: 5000, Type: "assistant", Text: "Now I'll fix it"},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	RenderStream(&buf, result, RenderStreamOptions{})
	out := buf.String()

	// Should show timing header
	if !strings.Contains(out, "2026-06-10T12:00:00Z") {
		t.Fatalf("output missing timestamp:\n%s", out)
	}
	if !strings.Contains(out, "claude") {
		t.Fatalf("output missing agent:\n%s", out)
	}
	if !strings.Contains(out, "completed") {
		t.Fatalf("output missing outcome:\n%s", out)
	}

	// Should show tool timings
	if !strings.Contains(out, "Bash") {
		t.Fatalf("output missing tool name:\n%s", out)
	}
	if !strings.Contains(out, "×2") {
		t.Fatalf("output missing tool count:\n%s", out)
	}

	// Should show event replay with offsets
	if !strings.Contains(out, "+5ms") {
		t.Fatalf("output missing +5ms offset:\n%s", out)
	}
	if !strings.Contains(out, "+100ms") {
		t.Fatalf("output missing +100ms offset:\n%s", out)
	}
	if !strings.Contains(out, "+1s") {
		t.Fatalf("output missing +1s offset:\n%s", out)
	}
	if !strings.Contains(out, "+2s") {
		t.Fatalf("output missing +2s offset:\n%s", out)
	}
	if !strings.Contains(out, "+5s") {
		t.Fatalf("output missing +5s offset:\n%s", out)
	}

	// Should show assistant text
	if !strings.Contains(out, "Let me read the file") {
		t.Fatalf("output missing assistant text:\n%s", out)
	}

	// Should show tool use
	if !strings.Contains(out, "→ Read") {
		t.Fatalf("output missing tool use indicator:\n%s", out)
	}
}

func TestRenderStreamHandlesMixedOutcomes(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	result := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptStream{
					{
						Timing: AttemptTiming{
							Agent:   "claude",
							Start:   base,
							Outcome: "failed",
							Duration: 45 * time.Second,
						},
						Events: []StreamEvent{
							{AtMS: 100, Type: "assistant", Text: "Trying something"},
						},
					},
					{
						Timing: AttemptTiming{
							Agent:   "claude",
							Start:   base.Add(10 * time.Minute),
							Outcome: "completed",
							Duration: 30 * time.Second,
						},
						Events: []StreamEvent{
							{AtMS: 100, Type: "assistant", Text: "Fixed it"},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	RenderStream(&buf, result, RenderStreamOptions{})
	out := buf.String()

	// Should show both attempts
	if strings.Count(out, "failed") != 1 {
		t.Fatalf("output should show one failed attempt:\n%s", out)
	}
	if strings.Count(out, "completed") != 1 {
		t.Fatalf("output should show one completed attempt:\n%s", out)
	}

	// Should show both event replays
	if !strings.Contains(out, "Trying something") {
		t.Fatalf("output missing first attempt text:\n%s", out)
	}
	if !strings.Contains(out, "Fixed it") {
		t.Fatalf("output missing second attempt text:\n%s", out)
	}
}

func TestFormatOffset(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "+0ms"},
		{500, "+500ms"},
		{999, "+999ms"},
		{1000, "+1s"},
		{1500, "+1s"},
		{59000, "+59s"},
		{60000, "+1m0s"},
		{90000, "+1m30s"},
		{3661000, "+61m1s"},
	}
	for _, tc := range cases {
		got := formatOffset(tc.ms)
		if got != tc.want {
			t.Errorf("formatOffset(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func TestTruncatePayloadLineThreshold(t *testing.T) {
	// Build a 50-line payload that exceeds the default 40-line threshold.
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i+1))
	}
	payload := strings.Join(lines, "\n")

	truncated := truncatePayload(payload, false)
	if truncated == payload {
		t.Fatal("expected payload to be truncated")
	}
	if !strings.Contains(truncated, "elided") {
		t.Fatalf("expected elision marker, got:\n%s", truncated)
	}
	// Head and tail should be present.
	if !strings.Contains(truncated, "line 1") {
		t.Fatalf("missing head line:\n%s", truncated)
	}
	if !strings.Contains(truncated, "line 50") {
		t.Fatalf("missing tail line:\n%s", truncated)
	}
	// Middle lines should be elided.
	if strings.Contains(truncated, "line 25") {
		t.Fatalf("middle line should be elided:\n%s", truncated)
	}

	// Full mode returns the payload verbatim.
	full := truncatePayload(payload, true)
	if full != payload {
		t.Fatalf("--full should return payload verbatim")
	}
}

func TestTruncatePayloadByteThreshold(t *testing.T) {
	// Single-line payload longer than the default 4 KB byte threshold.
	payload := strings.Repeat("x", 5000)

	truncated := truncatePayload(payload, false)
	if truncated == payload {
		t.Fatal("expected payload to be truncated")
	}
	if !strings.Contains(truncated, "elided") {
		t.Fatalf("expected elision marker, got:\n%s", truncated)
	}
	if !strings.HasPrefix(truncated, strings.Repeat("x", streamTruncationLimits.HeadBytes)) {
		t.Fatal("missing head bytes")
	}
	if !strings.HasSuffix(truncated, strings.Repeat("x", streamTruncationLimits.TailBytes)) {
		t.Fatal("missing tail bytes")
	}

	full := truncatePayload(payload, true)
	if full != payload {
		t.Fatal("--full should return payload verbatim")
	}
}

func TestTruncatePayloadSmallPayloadUnchanged(t *testing.T) {
	payload := "small payload\nwith a few lines"

	if got := truncatePayload(payload, false); got != payload {
		t.Fatalf("small payload changed in default mode: %q", got)
	}
	if got := truncatePayload(payload, true); got != payload {
		t.Fatalf("small payload changed in full mode: %q", got)
	}
}

func TestRenderStreamTruncatesLargeToolResult(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("result line %d", i+1))
	}
	resultText := strings.Join(lines, "\n")

	res := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptStream{
					{
						Timing: AttemptTiming{
							Agent:    "claude",
							Start:    base,
							Outcome:  "completed",
							Duration: 10 * time.Second,
						},
						Events: []StreamEvent{
							{AtMS: 100, Type: "tool_result", Text: resultText},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	RenderStream(&buf, res, RenderStreamOptions{})
	out := buf.String()

	if !strings.Contains(out, "elided") {
		t.Fatalf("expected elision marker, got:\n%s", out)
	}
	if !strings.Contains(out, "result line 1") {
		t.Fatalf("missing head:\n%s", out)
	}
	if !strings.Contains(out, "result line 50") {
		t.Fatalf("missing tail:\n%s", out)
	}
	if strings.Contains(out, "result line 25") {
		t.Fatalf("middle should be elided:\n%s", out)
	}

	buf.Reset()
	RenderStream(&buf, res, RenderStreamOptions{Full: true})
	fullOut := buf.String()

	if strings.Contains(fullOut, "elided") {
		t.Fatalf("--full should not contain elision marker:\n%s", fullOut)
	}
	if !strings.Contains(fullOut, "result line 25") {
		t.Fatalf("--full should contain middle lines:\n%s", fullOut)
	}
}

func TestRenderStreamTruncatesLargeToolUseArgs(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("arg line %d", i+1))
	}
	args := strings.Join(lines, "\n")

	res := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptStream{
					{
						Timing: AttemptTiming{
							Agent:    "claude",
							Start:    base,
							Outcome:  "completed",
							Duration: 10 * time.Second,
						},
						Events: []StreamEvent{
							{AtMS: 100, Type: "tool_use", ToolName: "Write", ToolArgs: args},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	RenderStream(&buf, res, RenderStreamOptions{})
	out := buf.String()

	if !strings.Contains(out, "elided") {
		t.Fatalf("expected elision marker, got:\n%s", out)
	}

	buf.Reset()
	RenderStream(&buf, res, RenderStreamOptions{Full: true})
	fullOut := buf.String()

	if strings.Contains(fullOut, "elided") {
		t.Fatalf("--full should not contain elision marker:\n%s", fullOut)
	}
	if !strings.Contains(fullOut, "arg line 25") {
		t.Fatalf("--full should contain middle lines:\n%s", fullOut)
	}
}

func TestStreamRawSingleTaskSingleAttemptRoundTrip(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// Write one gzipped attempt stream for the first task.
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	writeTimingStreamWithEvents(t, streamDir, "attempt-001.jsonl.gz", "claude", "claude --model opus4.8", 1, base, "completed", 60_000, []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`},
	})

	// Read the raw gzip file to compare against.
	rawPath := filepath.Join(streamDir, "attempt-001.jsonl.gz")
	rawGz, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(rawGz))
	if err != nil {
		t.Fatal(err)
	}
	wantJSONL, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	_ = zr.Close()
	wantJSONL = bytes.TrimSpace(wantJSONL) // StreamRawWith trims trailing whitespace

	var buf bytes.Buffer
	err = StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(buf.String())
	if got != string(wantJSONL) {
		t.Fatalf("raw output mismatch:\ngot:\n%s\n\nwant:\n%s", got, string(wantJSONL))
	}
}

func TestStreamRawMultiAttemptDelimiter(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	// Write two attempts with different start times.
	writeTimingStreamWithEvents(t, streamDir, "attempt-001.jsonl.gz", "claude", "", 1, base, "failed", 45_000, []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
	})
	writeTimingStreamWithEvents(t, streamDir, "attempt-002.jsonl.gz", "claude", "", 2, base.Add(10*time.Minute), "completed", 30_000, []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
	})

	var buf bytes.Buffer
	err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Should contain both attempt files' JSONL content.
	if !strings.Contains(out, `"attempt":1`) {
		t.Fatalf("output missing attempt 1 header:\n%s", out)
	}
	if !strings.Contains(out, `"attempt":2`) {
		t.Fatalf("output missing attempt 2 header:\n%s", out)
	}

	// Should contain delimiter between the two attempts.
	if !strings.Contains(out, `{"type":"delimiter","file":"attempt-002.jsonl.gz"}`) {
		t.Fatalf("output missing delimiter:\n%s", out)
	}

	// Verify the delimiter appears after attempt 1 and before attempt 2.
	attempt1Pos := strings.Index(out, `"attempt":1`)
	delimPos := strings.Index(out, `"type":"delimiter"`)
	attempt2Pos := strings.Index(out, `"attempt":2`)
	if delimPos < attempt1Pos {
		t.Fatalf("delimiter before attempt 1")
	}
	if attempt2Pos < delimPos {
		t.Fatalf("attempt 2 before delimiter")
	}
}

func TestStreamRawWholeSetMultipleTasks(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// Write attempt for task 01-a.
	dirA := taskStreamDir(env.demoDir(), "01-a.md")
	writeTimingStream(t, dirA, "attempt-001.jsonl.gz", "claude", 1, base, "completed", 60_000)

	// Write attempt for task 02-b.
	dirB := taskStreamDir(env.demoDir(), "02-b.md")
	writeTimingStream(t, dirB, "attempt-001.jsonl.gz", "claude", 1, base.Add(5*time.Minute), "failed", 30_000)

	var buf bytes.Buffer
	err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Both tasks should appear.
	if !strings.Contains(out, `"attempt":1`) {
		t.Fatalf("output missing attempt data:\n%s", out)
	}

	// Count delimiters: one between the two tasks' attempts.
	delimCount := strings.Count(out, `"type":"delimiter"`)
	if delimCount != 1 {
		t.Fatalf("expected 1 delimiter between tasks, got %d:\n%s", delimCount, out)
	}
}

func TestStreamRawEmptyNoCapturedStreams(t *testing.T) {
	env := streamFixture(t)

	var buf bytes.Buffer
	err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "no captured attempt streams for demo") {
		t.Fatalf("expected 'no captured attempt streams' message, got:\n%s", out)
	}
}

func TestStreamRawRoundTripStoredGzip(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	writeTimingStream(t, streamDir, "attempt-001.jsonl.gz", "claude", 1, base, "completed", 60_000)

	// Read the stored gzip and decompress to get the original JSONL.
	rawPath := filepath.Join(streamDir, "attempt-001.jsonl.gz")
	rawGz, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(rawGz))
	if err != nil {
		t.Fatal(err)
	}
	originalJSONL, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	_ = zr.Close()

	// StreamRawWith should emit exactly the same decompressed bytes.
	var buf bytes.Buffer
	err = StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(buf.String())
	want := strings.TrimSpace(string(originalJSONL))
	if got != want {
		t.Fatalf("raw bytes round-trip mismatch:\ngot:  %q\nwant: %q", got, want)
	}

	// Re-compress the raw output and verify it decompresses back to the same bytes,
	// confirming the gzip round-trip is lossless.
	var reGz bytes.Buffer
	zw := gzip.NewWriter(&reGz)
	if _, err := zw.Write([]byte(buf.String())); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	zr2, err := gzip.NewReader(&reGz)
	if err != nil {
		t.Fatal(err)
	}
	reRead, err := io.ReadAll(zr2)
	if err != nil {
		t.Fatal(err)
	}
	_ = zr2.Close()

	if string(reRead) != buf.String() {
		t.Fatalf("re-compress/re-decompress round-trip mismatch:\ngot:  %q\nwant: %q", string(reRead), buf.String())
	}
}

func TestStreamRawRejectsInvalidTargets(t *testing.T) {
	env := streamFixture(t)

	tests := []struct {
		name   string
		target string
		code   int
	}{
		{"unknown set", "missing", ExitNoRunnable},
		{"unknown task file", "demo/99-z.md", ExitNoRunnable},
		{"bare filename", "01-a.md", ExitSetup},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
				ResolveInput: ResolveInput{CWD: env.root},
				Target:       tc.target,
			}, &buf)
			assertExitCode(t, err, tc.code)
		})
	}
}

func TestHumanizeBytes(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{2 * 1024 * 1024, "2.0 MB"},
	}
	for _, tc := range cases {
		got := humanizeBytes(tc.n)
		if got != tc.want {
			t.Errorf("humanizeBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
