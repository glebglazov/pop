package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTimingStream writes one gzipped Captured attempt stream file with the
// given header and footer, mirroring what persistAttemptStream stores.
func writeTimingStream(t *testing.T, dir, name, agent string, attempt int, start time.Time, outcome string, durationMS int64) {
	t.Helper()
	writeTimingStreamWithEvents(t, dir, name, agent, "", attempt, start, outcome, durationMS, []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
	})
}

// writeTimingStreamWithEvents is writeTimingStream with explicit stream events.
func writeTimingStreamWithEvents(t *testing.T, dir, name, agent, requestedAgent string, attempt int, start time.Time, outcome string, durationMS int64, events []streamEventRecord) {
	t.Helper()
	var jsonl bytes.Buffer
	enc := json.NewEncoder(&jsonl)
	if err := enc.Encode(streamHeaderRecord{Type: "header", Agent: agent, RequestedAgent: requestedAgent, Attempt: attempt, StartTime: start.UTC()}); err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Encode(streamFooterRecord{Type: "footer", Outcome: outcome, DurationMS: durationMS}); err != nil {
		t.Fatal(err)
	}

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(jsonl.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func timingsFixture(t *testing.T) *execFixture {
	t.Helper()
	return setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
}

func TestTimingsWholeSetSpansAttemptsOrderedByStartTime(t *testing.T) {
	env := timingsFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")

	// Attempts from separate invocations and reopens: numbering has gaps and
	// the on-disk order (by NNN) disagrees with start-time order.
	writeTimingStream(t, streamDir, "attempt-001.jsonl.gz", "claude", 1, base.Add(10*time.Minute), "failed", 45_000)
	writeTimingStream(t, streamDir, "attempt-002.jsonl.gz", "claude", 2, base, "timed_out", 83_000)
	writeTimingStream(t, streamDir, "attempt-007.jsonl.gz", "codex", 7, base.Add(20*time.Minute), "completed", 500)

	result, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
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
	if len(attempts) != 3 {
		t.Fatalf("attempts = %d, want all stored attempts", len(attempts))
	}
	wantOrder := []AttemptTiming{
		{Agent: "claude", RequestedAgent: "claude", Start: base, Outcome: "timed_out", Duration: 83 * time.Second},
		{Agent: "claude", RequestedAgent: "claude", Start: base.Add(10 * time.Minute), Outcome: "failed", Duration: 45 * time.Second},
		{Agent: "codex", RequestedAgent: "codex", Start: base.Add(20 * time.Minute), Outcome: "completed", Duration: 500 * time.Millisecond},
	}
	for i, want := range wantOrder {
		got := attempts[i]
		if got.Agent != want.Agent || got.RequestedAgent != want.RequestedAgent || !got.Start.Equal(want.Start) || got.Outcome != want.Outcome || got.Duration != want.Duration {
			t.Fatalf("attempt[%d] = %+v, want %+v", i, got, want)
		}
	}

	if len(result.Tasks[1].Attempts) != 0 {
		t.Fatalf("02-b attempts = %#v, want none", result.Tasks[1].Attempts)
	}
}

func TestTimingsSingleTaskTarget(t *testing.T) {
	env := timingsFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	writeTimingStream(t, taskStreamDir(env.demoDir(), "01-a.md"), "attempt-001.jsonl.gz", "claude", 1, base, "completed", 60_000)
	writeTimingStream(t, taskStreamDir(env.demoDir(), "02-b.md"), "attempt-001.jsonl.gz", "claude", 1, base, "failed", 30_000)

	result, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/02-b.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].TaskID != "02-b" {
		t.Fatalf("tasks = %#v, want only 02-b", result.Tasks)
	}
	if len(result.Tasks[0].Attempts) != 1 || result.Tasks[0].Attempts[0].Outcome != "failed" {
		t.Fatalf("attempts = %#v", result.Tasks[0].Attempts)
	}
}

func TestTimingsRejectsInvalidTargets(t *testing.T) {
	env := timingsFixture(t)

	cases := map[string]struct {
		target string
		code   int
	}{
		"unknown set":       {"missing", ExitNoRunnable},
		"unknown task file": {"demo/99-z.md", ExitNoRunnable},
		"bare filename":     {"01-a.md", ExitSetup},
		"absolute path":     {filepath.Join(env.demoDir(), "01-a.md"), ExitSetup},
		"relative path":     {"./demo", ExitSetup},
		"empty after trim":  {"   ", ExitSetup},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
				ResolveInput: ResolveInput{CWD: env.root},
				Target:       tc.target,
			})
			assertExitCode(t, err, tc.code)
		})
	}
}

func TestRenderTimingsShowsRowsWithoutOrdinals(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	result := &TimingsResult{
		TaskSetID: "demo",
		Tasks: []TaskTimings{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptTiming{
					{Agent: "claude", Start: base, Outcome: "timed_out", Duration: 83 * time.Second},
					{Agent: "codex", Start: base.Add(10 * time.Minute), Outcome: "completed", Duration: 500 * time.Millisecond},
				},
			},
			{TaskID: "02-b", File: "02-b.md", Title: "B"},
		},
	}

	var buf bytes.Buffer
	RenderTimings(&buf, result)
	out := buf.String()

	for _, want := range []string{
		"demo/01-a.md  A",
		"2026-06-10T12:00:00Z  claude  timed_out  1m23s",
		"2026-06-10T12:10:00Z  codex   completed  500ms",
		"demo/02-b.md  B",
		"no recorded attempts",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	// Rows are keyed by start time only: the earlier attempt renders first and
	// no ordinal appears that could contradict the executor's "Attempt N/max".
	if strings.Index(out, "12:00:00Z") > strings.Index(out, "12:10:00Z") {
		t.Fatalf("attempts not ordered by start time:\n%s", out)
	}
	for _, forbidden := range []string{"attempt-", "Attempt 1", "#1"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("output shows ordinal %q:\n%s", forbidden, out)
		}
	}
}

func TestTimingsShowsRequestedAgentAndFallsBackForOldStreams(t *testing.T) {
	env := timingsFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	writeTimingStreamWithEvents(t, streamDir, "attempt-001.jsonl.gz", "claude", "claude --model opus4.8", 1, base, "completed", 60_000, nil)
	writeTimingStream(t, streamDir, "attempt-002.jsonl.gz", "codex", 2, base.Add(time.Minute), "failed", 30_000)

	result, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	attempts := result.Tasks[0].Attempts
	if len(attempts) != 2 {
		t.Fatalf("attempts = %#v", attempts)
	}
	if attempts[0].Agent != "claude" || attempts[0].RequestedAgent != "claude --model opus4.8" {
		t.Fatalf("new stream attempt = %+v", attempts[0])
	}
	if attempts[1].Agent != "codex" || attempts[1].RequestedAgent != "codex" {
		t.Fatalf("old stream fallback = %+v", attempts[1])
	}

	var buf bytes.Buffer
	RenderTimings(&buf, result)
	out := buf.String()
	for _, want := range []string{
		"2026-06-10T12:00:00Z  claude --model opus4.8  completed  1m0s",
		"2026-06-10T12:01:00Z  codex                   failed     30s",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestTimingsExtractsActualModelFromClaudeInitOnly(t *testing.T) {
	env := timingsFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	claudeEvents := []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init","model":"claude-sonnet-4-20250514"}`},
	}
	missingModelEvents := []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
	}
	writeTimingStreamWithEvents(t, streamDir, "attempt-001.jsonl.gz", "claude", "claude --model sonnet", 1, base, "completed", 60_000, claudeEvents)
	writeTimingStreamWithEvents(t, streamDir, "attempt-002.jsonl.gz", "claude", "claude --model opus", 2, base.Add(time.Minute), "failed", 30_000, missingModelEvents)
	writeTimingStreamWithEvents(t, streamDir, "attempt-003.jsonl.gz", "codex", "codex --model gpt-5", 3, base.Add(2*time.Minute), "completed", 10_000, claudeEvents)

	result, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	attempts := result.Tasks[0].Attempts
	if len(attempts) != 3 {
		t.Fatalf("attempts = %#v", attempts)
	}
	if attempts[0].ActualModel != "claude-sonnet-4-20250514" {
		t.Fatalf("claude actual model = %q", attempts[0].ActualModel)
	}
	if attempts[1].ActualModel != "" {
		t.Fatalf("missing claude model should stay blank, got %q", attempts[1].ActualModel)
	}
	if attempts[2].ActualModel != "" {
		t.Fatalf("codex actual model should stay blank, got %q", attempts[2].ActualModel)
	}

	var buf bytes.Buffer
	RenderTimings(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "claude --model sonnet  claude-sonnet-4-20250514  completed") {
		t.Fatalf("output missing requested agent beside actual model:\n%s", out)
	}
	if strings.Contains(out, "claude --model opus  opus") || strings.Contains(out, "codex --model gpt-5  gpt-5") {
		t.Fatalf("output appears to copy requested model into actual model column:\n%s", out)
	}
}

// claudeUse renders one assistant event whose content is the given tool_use
// blocks, each pair of (id, name).
func claudeUse(pairs ...[2]string) string {
	blocks := make([]string, 0, len(pairs))
	for _, p := range pairs {
		blocks = append(blocks, `{"type":"tool_use","id":"`+p[0]+`","name":"`+p[1]+`","input":{"command":"ls"}}`)
	}
	return `{"type":"assistant","message":{"content":[` + strings.Join(blocks, ",") + `]}}`
}

func claudeResult(toolUseID string) string {
	return `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"` + toolUseID + `","content":"ok"}]}}`
}

func TestClaudeToolTimingsPairsUseWithResultByID(t *testing.T) {
	tools, _ := claudeToolTimings([]streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
		{Type: "event", AtMS: 100, Raw: claudeUse([2]string{"tu_1", "Bash"})},
		{Type: "event", AtMS: 1100, Raw: claudeResult("tu_1")},
		{Type: "event", AtMS: 2000, Raw: claudeUse([2]string{"tu_2", "Read"})},
		{Type: "event", AtMS: 2200, Raw: claudeResult("tu_2")},
		{Type: "event", AtMS: 3000, Raw: claudeUse([2]string{"tu_3", "Bash"})},
		{Type: "event", AtMS: 3500, Raw: claudeResult("tu_3")},
	})

	want := []ToolTiming{
		{Name: "Bash", Count: 2, Total: 1500 * time.Millisecond},
		{Name: "Read", Count: 1, Total: 200 * time.Millisecond},
	}
	if len(tools) != len(want) {
		t.Fatalf("tools = %#v, want %#v", tools, want)
	}
	for i := range want {
		if tools[i] != want[i] {
			t.Fatalf("tools[%d] = %+v, want %+v", i, tools[i], want[i])
		}
	}
}

func TestClaudeToolTimingsPairsParallelCallsByID(t *testing.T) {
	// One assistant turn issues two tool calls at once; results arrive in the
	// opposite order. Ids — not arrival order — must do the pairing.
	tools, _ := claudeToolTimings([]streamEventRecord{
		{Type: "event", AtMS: 100, Raw: claudeUse([2]string{"tu_a", "Read"}, [2]string{"tu_b", "Grep"})},
		{Type: "event", AtMS: 400, Raw: claudeResult("tu_b")},
		{Type: "event", AtMS: 900, Raw: claudeResult("tu_a")},
	})

	want := []ToolTiming{
		{Name: "Read", Count: 1, Total: 800 * time.Millisecond},
		{Name: "Grep", Count: 1, Total: 300 * time.Millisecond},
	}
	if len(tools) != len(want) {
		t.Fatalf("tools = %#v, want %#v", tools, want)
	}
	for i := range want {
		if tools[i] != want[i] {
			t.Fatalf("tools[%d] = %+v, want %+v", i, tools[i], want[i])
		}
	}
}

func TestClaudeToolTimingsSkipsUnpairedAndMalformedEvents(t *testing.T) {
	tools, windows := claudeToolTimings([]streamEventRecord{
		{Type: "event", AtMS: 10, Raw: `not json`},
		{Type: "event", AtMS: 100, Raw: claudeUse([2]string{"tu_killed", "Bash"})}, // no result: attempt died
		{Type: "event", AtMS: 200, Raw: claudeResult("tu_unknown")},                // result without a use
		{Type: "event", AtMS: 300, Raw: claudeUse([2]string{"tu_ok", "Edit"})},
		{Type: "event", AtMS: 550, Raw: claudeResult("tu_ok")},
	})

	if len(tools) != 1 || tools[0] != (ToolTiming{Name: "Edit", Count: 1, Total: 250 * time.Millisecond}) {
		t.Fatalf("tools = %#v, want only the paired Edit call", tools)
	}
	// The unpaired use is absent from the rows but present as an open window,
	// so Model time will not absorb the wait on the tool the attempt died in.
	want := map[toolWindow]bool{
		{StartMS: 300, EndMS: 550}:             true,
		{StartMS: 100, EndMS: openWindowEndMS}: true,
	}
	if len(windows) != len(want) {
		t.Fatalf("windows = %#v, want paired + open", windows)
	}
	for _, w := range windows {
		if !want[w] {
			t.Fatalf("unexpected window %+v in %#v", w, windows)
		}
	}
}

// codexStarted renders one item.started for a tool item of the given id, type,
// and optional server/tool (used only for mcp_tool_call).
func codexStarted(id, itemType string, serverTool ...string) string {
	return codexItemEvent("item.started", id, itemType, serverTool...)
}

// codexCompleted renders the matching item.completed for an item id.
func codexCompleted(id, itemType string, serverTool ...string) string {
	return codexItemEvent("item.completed", id, itemType, serverTool...)
}

func codexItemEvent(phase, id, itemType string, serverTool ...string) string {
	fields := `"id":"` + id + `","type":"` + itemType + `"`
	if len(serverTool) == 2 {
		fields += `,"server":"` + serverTool[0] + `","tool":"` + serverTool[1] + `"`
	}
	return `{"type":"` + phase + `","item":{` + fields + `}}`
}

func TestCodexToolTimingsPairsStartedWithCompletedByID(t *testing.T) {
	tools, _ := codexToolTimings([]streamEventRecord{
		{Type: "event", AtMS: 9761, Raw: `{"type":"turn.started"}`},
		{Type: "event", AtMS: 9878, Raw: codexStarted("item_1", "command_execution")},
		{Type: "event", AtMS: 10878, Raw: codexCompleted("item_1", "command_execution")},
		{Type: "event", AtMS: 11000, Raw: codexStarted("item_2", "file_change")},
		{Type: "event", AtMS: 11200, Raw: codexCompleted("item_2", "file_change")},
		{Type: "event", AtMS: 12000, Raw: codexStarted("item_3", "command_execution")},
		{Type: "event", AtMS: 12500, Raw: codexCompleted("item_3", "command_execution")},
	})

	want := []ToolTiming{
		{Name: "command_execution", Count: 2, Total: 1500 * time.Millisecond},
		{Name: "file_change", Count: 1, Total: 200 * time.Millisecond},
	}
	if len(tools) != len(want) {
		t.Fatalf("tools = %#v, want %#v", tools, want)
	}
	for i := range want {
		if tools[i] != want[i] {
			t.Fatalf("tools[%d] = %+v, want %+v", i, tools[i], want[i])
		}
	}
}

func TestCodexToolTimingsNamesMcpByServerAndTool(t *testing.T) {
	tools, _ := codexToolTimings([]streamEventRecord{
		{Type: "event", AtMS: 100, Raw: codexStarted("a", "mcp_tool_call", "github", "search")},
		{Type: "event", AtMS: 400, Raw: codexCompleted("a", "mcp_tool_call", "github", "search")},
		// tool present, server missing -> mcp:<tool>
		{Type: "event", AtMS: 500, Raw: codexStarted("b", "mcp_tool_call", "", "read_file")},
		{Type: "event", AtMS: 700, Raw: codexCompleted("b", "mcp_tool_call", "", "read_file")},
		// neither present -> bare mcp_tool_call
		{Type: "event", AtMS: 800, Raw: codexStarted("c", "mcp_tool_call")},
		{Type: "event", AtMS: 850, Raw: codexCompleted("c", "mcp_tool_call")},
	})

	want := []ToolTiming{
		{Name: "mcp:github/search", Count: 1, Total: 300 * time.Millisecond},
		{Name: "mcp:read_file", Count: 1, Total: 200 * time.Millisecond},
		{Name: "mcp_tool_call", Count: 1, Total: 50 * time.Millisecond},
	}
	if len(tools) != len(want) {
		t.Fatalf("tools = %#v, want %#v", tools, want)
	}
	for i := range want {
		if tools[i] != want[i] {
			t.Fatalf("tools[%d] = %+v, want %+v", i, tools[i], want[i])
		}
	}
}

func TestCodexToolTimingsTakesMcpNameFromCompletedWhenStartedLacksIt(t *testing.T) {
	// The mcp server/tool fields may only be present on the completed event.
	tools, _ := codexToolTimings([]streamEventRecord{
		{Type: "event", AtMS: 100, Raw: codexStarted("a", "mcp_tool_call")},
		{Type: "event", AtMS: 400, Raw: codexCompleted("a", "mcp_tool_call", "github", "search")},
	})
	if len(tools) != 1 || tools[0] != (ToolTiming{Name: "mcp:github/search", Count: 1, Total: 300 * time.Millisecond}) {
		t.Fatalf("tools = %#v, want mcp:github/search named from completed", tools)
	}
}

func TestCodexToolTimingsIgnoresNonToolItemsAndMalformed(t *testing.T) {
	tools, windows := codexToolTimings([]streamEventRecord{
		{Type: "event", AtMS: 10, Raw: `not json`},
		// agent_message and reasoning are not tools even if they bracket like one.
		{Type: "event", AtMS: 50, Raw: codexStarted("msg", "agent_message")},
		{Type: "event", AtMS: 60, Raw: codexCompleted("msg", "agent_message")},
		{Type: "event", AtMS: 70, Raw: codexStarted("rsn", "reasoning")},
		{Type: "event", AtMS: 90, Raw: codexCompleted("rsn", "reasoning")},
		// a tool that started but never completed: a killed attempt.
		{Type: "event", AtMS: 100, Raw: codexStarted("killed", "command_execution")},
		// a real paired tool.
		{Type: "event", AtMS: 300, Raw: codexStarted("ok", "file_change")},
		{Type: "event", AtMS: 550, Raw: codexCompleted("ok", "file_change")},
	})

	if len(tools) != 1 || tools[0] != (ToolTiming{Name: "file_change", Count: 1, Total: 250 * time.Millisecond}) {
		t.Fatalf("tools = %#v, want only the paired file_change", tools)
	}
	want := map[toolWindow]bool{
		{StartMS: 300, EndMS: 550}:             true,
		{StartMS: 100, EndMS: openWindowEndMS}: true,
	}
	if len(windows) != len(want) {
		t.Fatalf("windows = %#v, want paired + open", windows)
	}
	for _, w := range windows {
		if !want[w] {
			t.Fatalf("unexpected window %+v in %#v", w, windows)
		}
	}
}

func TestModelTimeSubtractsUnionOfToolWindows(t *testing.T) {
	cases := []struct {
		name    string
		windows []toolWindow
		totalMS int64
		want    time.Duration
	}{
		{
			name:    "disjoint windows",
			windows: []toolWindow{{0, 2_000}, {10_000, 14_000}},
			totalMS: 20_000,
			want:    14 * time.Second,
		},
		{
			name: "parallel calls overlap counted once",
			// Two tools in flight 100..900 and 100..400: union is 800ms, not 1.1s.
			windows: []toolWindow{{100, 900}, {100, 400}},
			totalMS: 1_000,
			want:    200 * time.Millisecond,
		},
		{
			name: "open window clamps to attempt end",
			// Killed at 60s while a tool launched at 20s was still running: the
			// 40s wait is tool time, not Model time.
			windows: []toolWindow{{0, 2_000}, {10_000, 14_000}, {20_000, openWindowEndMS}},
			totalMS: 60_000,
			want:    14 * time.Second,
		},
		{
			name:    "no windows means all model",
			windows: nil,
			totalMS: 5_000,
			want:    5 * time.Second,
		},
		{
			name:    "skew past the footer clamps to zero",
			windows: []toolWindow{{0, 99_000}},
			totalMS: 10_000,
			want:    0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelTime(tc.windows, tc.totalMS); got != tc.want {
				t.Fatalf("modelTime = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTimingsPerToolBreakdownForClaudeOnly(t *testing.T) {
	env := timingsFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")

	claudeEvents := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: claudeUse([2]string{"tu_1", "Bash"})},
		{Type: "event", AtMS: 1100, Raw: claudeResult("tu_1")},
		{Type: "event", AtMS: 2000, Raw: claudeUse([2]string{"tu_2", "Read"})},
		{Type: "event", AtMS: 2200, Raw: claudeResult("tu_2")},
	}
	writeTimingStreamWithEvents(t, streamDir, "attempt-001.jsonl.gz", "claude", "", 1, base, "completed", 60_000, claudeEvents)
	// A non-claude structured agent stores the same substrate but has no
	// pairing parser yet: outcome + total only, no per-tool rows, no error.
	writeTimingStreamWithEvents(t, streamDir, "attempt-002.jsonl.gz", "codex", "", 2, base.Add(10*time.Minute), "completed", 30_000, claudeEvents)

	result, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	attempts := result.Tasks[0].Attempts
	if len(attempts) != 2 {
		t.Fatalf("attempts = %#v", attempts)
	}

	wantTools := []ToolTiming{
		{Name: "Bash", Count: 1, Total: time.Second},
		{Name: "Read", Count: 1, Total: 200 * time.Millisecond},
	}
	if len(attempts[0].Tools) != len(wantTools) {
		t.Fatalf("claude tools = %#v, want %#v", attempts[0].Tools, wantTools)
	}
	for i := range wantTools {
		if attempts[0].Tools[i] != wantTools[i] {
			t.Fatalf("claude tools[%d] = %+v, want %+v", i, attempts[0].Tools[i], wantTools[i])
		}
	}
	// Model time: 60s total minus 1.2s of tool windows.
	if attempts[0].Model != 58_800*time.Millisecond {
		t.Fatalf("claude model = %v, want 58.8s", attempts[0].Model)
	}
	if len(attempts[1].Tools) != 0 {
		t.Fatalf("codex tools = %#v, want none", attempts[1].Tools)
	}
	if attempts[1].Model != 0 {
		t.Fatalf("codex model = %v, want zero without a pairing parser", attempts[1].Model)
	}
}

func TestRenderTimingsShowsToolRowsUnderTheirAttempt(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	result := &TimingsResult{
		TaskSetID: "demo",
		Tasks: []TaskTimings{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptTiming{
					{
						Agent: "claude", Start: base, Outcome: "completed", Duration: 90 * time.Second,
						Tools: []ToolTiming{
							{Name: "Bash", Count: 12, Total: 65 * time.Second},
							{Name: "Read", Count: 3, Total: 2 * time.Second},
						},
						Model: 23 * time.Second,
					},
					{Agent: "codex", Start: base.Add(10 * time.Minute), Outcome: "completed", Duration: 30 * time.Second},
				},
			},
		},
	}

	var buf bytes.Buffer
	RenderTimings(&buf, result)
	out := buf.String()

	for _, want := range []string{
		"    Bash   ×12  1m5s",
		"    Read   ×3   2s",
		"    model       23s",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	// Tool rows belong to the attempt — and so the agent — that ran them: they
	// sit between the claude row and the codex row, with the model row last.
	claudeRow, bashRow, modelRow, codexRow := strings.Index(out, "claude"), strings.Index(out, "Bash"), strings.Index(out, "model"), strings.Index(out, "codex")
	if !(claudeRow < bashRow && bashRow < modelRow && modelRow < codexRow) {
		t.Fatalf("tool rows not under their attempt:\n%s", out)
	}
}

func TestRenderTimingsOmitsModelRowWithoutToolRows(t *testing.T) {
	// An attempt with no tool rows (no pairing parser, or no paired calls)
	// shows outcome + total only: a model row equal to the total is noise.
	result := &TimingsResult{
		TaskSetID: "demo",
		Tasks: []TaskTimings{{
			TaskID: "01-a", File: "01-a.md", Title: "A",
			Attempts: []AttemptTiming{
				{Agent: "claude", Start: time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC), Outcome: "completed", Duration: 9 * time.Second},
			},
		}},
	}

	var buf bytes.Buffer
	RenderTimings(&buf, result)
	if strings.Contains(buf.String(), "model") {
		t.Fatalf("model row rendered without tool rows:\n%s", buf.String())
	}
}

func TestRunTaskPrintsInlineBreakdownOnDone(t *testing.T) {
	env := setupExecutorFixture(t, false)
	installClaudeStreamAgent(t, env.root, true)
	// A stream from a previous invocation: it must stay with `pop tasks
	// timings`, not reappear inline.
	writeTimingStream(t, taskStreamDir(env.demoDir(), "01-a.md"), "attempt-001.jsonl.gz",
		"codex", 1, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "timed_out", 9_000)

	var buf bytes.Buffer
	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.Output = &buf
	if _, err := RunTaskWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	summary, heading := strings.Index(out, "✓ Completed demo/01-a"), strings.Index(out, "Attempt timing")
	if summary == -1 || heading == -1 || heading < summary {
		t.Fatalf("breakdown not after completion summary:\n%s", out)
	}
	breakdown := out[heading:]
	if !strings.Contains(breakdown, "claude") || !strings.Contains(breakdown, "completed") {
		t.Fatalf("breakdown missing this invocation's attempt:\n%s", out)
	}
	for _, forbidden := range []string{"codex", "timed_out"} {
		if strings.Contains(breakdown, forbidden) {
			t.Fatalf("breakdown shows prior-invocation attempt (%q):\n%s", forbidden, out)
		}
	}

	// Full history — both invocations — stays with the timings reader.
	result, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks[0].Attempts) != 2 {
		t.Fatalf("reader attempts = %#v, want prior + this invocation", result.Tasks[0].Attempts)
	}
}

func TestRunTaskPrintsInlineBreakdownOnFailed(t *testing.T) {
	env := setupExecutorFixture(t, false)
	installClaudeStreamAgent(t, env.root, false)

	var buf bytes.Buffer
	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.MaxTries = 2
	opts.Output = &buf
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)

	out := buf.String()
	lastFail, heading := strings.Index(out, "✗ Attempt 2/2 failed"), strings.Index(out, "Attempt timing")
	if lastFail == -1 || heading == -1 || heading < lastFail {
		t.Fatalf("breakdown not after the terminal failure line:\n%s", out)
	}
	// Both of this invocation's attempts appear as failed rows.
	if got := strings.Count(out[heading:], "failed"); got != 2 {
		t.Fatalf("failed rows = %d, want 2:\n%s", got, out)
	}
}

func TestRunTaskCustomCommandPrintsNoBreakdown(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "custom run"})

	var buf bytes.Buffer
	opts := env.runOpts(true, agent)
	opts.Output = &buf
	if _, err := RunTaskWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatal(err)
	}
	assertTaskDone(t, env, "01-a")
	if strings.Contains(buf.String(), "Attempt timing") {
		t.Fatalf("custom-command run printed a breakdown:\n%s", buf.String())
	}
}

func TestRunTaskSetPrintsBreakdownPerTaskAsItTerminates(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	installClaudeStreamAgent(t, env.root, true)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPreset = "claude"
	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completed) != 2 {
		t.Fatalf("completed = %d", len(result.Completed))
	}

	out := buf.String()
	if got := strings.Count(out, "Attempt timing"); got != 2 {
		t.Fatalf("breakdown headings = %d, want one per task:\n%s", got, out)
	}
	// The first task's breakdown prints before the second task starts —
	// per task at termination, not batched at the end of the drain.
	firstBreakdown, secondTask := strings.Index(out, "Attempt timing"), strings.Index(out, "Running task demo/02-b")
	if secondTask == -1 || firstBreakdown > secondTask {
		t.Fatalf("first task's breakdown not printed before second task ran:\n%s", out)
	}
}

func TestReadAttemptTimingRejectsTruncatedStream(t *testing.T) {
	env := timingsFixture(t)
	dir := taskStreamDir(env.demoDir(), "01-a.md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Valid gzip whose JSONL lacks a footer.
	var jsonl bytes.Buffer
	enc := json.NewEncoder(&jsonl)
	if err := enc.Encode(streamHeaderRecord{Type: "header", Agent: "claude", Attempt: 1, StartTime: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(jsonl.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "attempt-001.jsonl.gz"), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := TimingsWith(env.deps(), nil, nil, TimingsOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "missing footer") {
		t.Fatalf("err = %v", err)
	}
}
