package tasks

import (
	"bytes"
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
	RenderStream(&buf, result)
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
	RenderStream(&buf, result)
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
	RenderStream(&buf, result)
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
