package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestClaudeToolDetailPairsArgsAndResults(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"ls"}}]}}`},
		{Type: "event", AtMS: 200, Raw: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"text","text":"ok"}]}]}}`},
		{Type: "event", AtMS: 300, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_2","name":"Bash","input":{"command":"ls"}}]}}`},
		{Type: "event", AtMS: 400, Raw: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_2","content":[{"type":"text","text":"ok again"}]}]}}`},
	}
	report := extractToolDetail("claude", events)
	if report.Refused {
		t.Fatalf("claude should not refuse tool detail: %s", report.RefusalReason)
	}
	if len(report.Invocations) != 2 {
		t.Fatalf("invocations = %d, want 2", len(report.Invocations))
	}
	if report.Invocations[0].ArgsHint != "ls" || report.Invocations[0].ResultBytes != 2 {
		t.Fatalf("first invocation = %#v", report.Invocations[0])
	}
}

func TestClaudeToolDetailDetectsUnboundedAndImageReads(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"README.md"}}]}}`},
		{Type: "event", AtMS: 200, Raw: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"text","text":"all"}]}]}}`},
		{Type: "event", AtMS: 300, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_2","name":"Read","input":{"path":"assets/pic.png","limit":100}}]}}`},
		{Type: "event", AtMS: 400, Raw: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_2","content":[{"type":"text","text":"img"}]}]}}`},
	}
	report := extractToolDetail("claude", events)
	if len(report.Invocations) != 2 {
		t.Fatalf("invocations = %d, want 2", len(report.Invocations))
	}
	if !report.Invocations[0].IsUnboundedRead {
		t.Fatalf("README read should be unbounded: %#v", report.Invocations[0])
	}
	if report.Invocations[1].IsUnboundedRead {
		t.Fatalf("limited read should not be unbounded: %#v", report.Invocations[1])
	}
	if !report.Invocations[1].IsImageRead {
		t.Fatalf("png read should be image read: %#v", report.Invocations[1])
	}
}

func TestClaudeToolDetailHandlesStringToolResultContent(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"go build"}}]}}`},
		{Type: "event", AtMS: 200, Raw: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_1","content":"Go build: Success","is_error":false}]}}`},
	}
	report := extractToolDetail("claude", events)
	if len(report.Invocations) != 1 {
		t.Fatalf("invocations = %#v", report.Invocations)
	}
	if report.Invocations[0].ResultBytes != len("Go build: Success") {
		t.Fatalf("result bytes = %d, want %d", report.Invocations[0].ResultBytes, len("Go build: Success"))
	}
}

func TestClaudeToolDetailDetectsErrors(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"false"}}]}}`},
		{Type: "event", AtMS: 200, Raw: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_1","is_error":true,"content":[{"type":"text","text":"exit 1"}]}]}}`},
	}
	report := extractToolDetail("claude", events)
	if len(report.Invocations) != 1 || !report.Invocations[0].IsError {
		t.Fatalf("invocations = %#v, want one error", report.Invocations)
	}
}

func TestExtractToolDetailRenderBlindRefuses(t *testing.T) {
	report := extractToolDetail("codex", []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"item.started","item":{"id":"i1","type":"command_execution"}}`},
	})
	if !report.Refused {
		t.Fatal("codex should refuse tool detail")
	}
	cap, _ := ResolveAgentAdapter("codex")
	if !strings.Contains(report.RefusalReason, cap.StreamRenderCapability().Reason) {
		t.Fatalf("refusal = %q, want declared reason %q", report.RefusalReason, cap.StreamRenderCapability().Reason)
	}
}

func TestGroupToolDetailInvocationsRepeated(t *testing.T) {
	invocations := []toolInvocationRecord{
		{Name: "Bash", ArgsKey: `{"command":"ls"}`, ArgsHint: "ls"},
		{Name: "Bash", ArgsKey: `{"command":"ls"}`, ArgsHint: "ls"},
		{Name: "Read", ArgsKey: `{"path":"a.go"}`, ArgsHint: "a.go"},
	}
	groups := groupToolDetailInvocations(invocations, false)
	if len(groups) != 1 || groups[0].Count != 2 || groups[0].Label != "Bash" {
		t.Fatalf("groups = %#v", groups)
	}
}

func TestSuspectReasonsPeakAndTurns(t *testing.T) {
	reasons := suspectReasons(
		TurnCount{Count: 11, HasTurn: true},
		PeakInput{Tokens: 250_000, HasPeak: true},
		5, true,
	)
	if len(reasons) != 2 {
		t.Fatalf("reasons = %#v, want peak-in and turns", reasons)
	}
	if !strings.Contains(reasons[0], "peak-in") || !strings.Contains(reasons[1], "turns") {
		t.Fatalf("reasons = %#v", reasons)
	}
}

func TestMedianTurnCount(t *testing.T) {
	attempts := []AttemptStream{
		{Timing: AttemptTiming{Turns: TurnCount{Count: 3, HasTurn: true}}},
		{Timing: AttemptTiming{Turns: TurnCount{Count: 7, HasTurn: true}}},
		{Timing: AttemptTiming{Turns: TurnCount{Count: 5, HasTurn: true}}},
	}
	median, ok := medianTurnCount(attempts)
	if !ok || median != 5 {
		t.Fatalf("median = %d ok=%v, want 5 true", median, ok)
	}
}

func TestRenderStreamToolDetailShowsFactsAndSuspects(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	bigResult := strings.Repeat("x", 5000)
	res := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{{
			TaskID: "01-a",
			File:   "01-a.md",
			Title:  "A",
			Attempts: []AttemptStream{{
				Timing: AttemptTiming{
					Agent:     "claude",
					Start:     base,
					Outcome:   "completed",
					Duration:  60 * time.Second,
					Turns:     TurnCount{Count: 20, HasTurn: true},
					PeakInput: PeakInput{Tokens: 250_000, HasPeak: true},
					Tools:     []ToolTiming{{Name: "Bash", Count: 3, Total: 3 * time.Second}},
					Model:     57 * time.Second,
				},
				ToolDetail: ToolDetailReport{Invocations: []toolInvocationRecord{
					{Name: "Bash", ArgsKey: `{"command":"ls"}`, ArgsHint: "ls", ResultBytes: 2},
					{Name: "Bash", ArgsKey: `{"command":"ls"}`, ArgsHint: "ls", ResultBytes: 2, IsError: true},
					{Name: "Bash", ArgsKey: `{"command":"ls"}`, ArgsHint: "ls", ResultBytes: 2, IsError: true},
					{Name: "Read", ArgsKey: `{"path":"README.md"}`, ArgsHint: "README.md", ResultBytes: 100, IsUnboundedRead: true},
					{Name: "Read", ArgsKey: `{"path":"pic.png"}`, ArgsHint: "pic.png", ResultBytes: 50, IsImageRead: true},
					{Name: "Read", ArgsKey: `{"path":"big.go"}`, ArgsHint: "big.go", ResultBytes: len(bigResult)},
				}},
			}, {
				Timing: AttemptTiming{
					Agent:    "claude",
					Start:    base.Add(time.Minute),
					Outcome:  "completed",
					Duration: 30 * time.Second,
					Turns:    TurnCount{Count: 3, HasTurn: true},
				},
			}, {
				Timing: AttemptTiming{
					Agent:    "claude",
					Start:    base.Add(2 * time.Minute),
					Outcome:  "completed",
					Duration: 20 * time.Second,
					Turns:    TurnCount{Count: 5, HasTurn: true},
				},
			}},
		}},
	}

	var withDetail bytes.Buffer
	RenderStream(&withDetail, res, RenderStreamOptions{ToolDetail: true})
	detailOut := withDetail.String()

	for _, want := range []string{
		"repeated", "Bash ls", "unbounded reads", "README.md",
		"largest payloads", "big.go", "errors", "image reads", "pic.png",
		"suspect:", "peak-in 250000", "turns 20 > 2×median 5",
	} {
		if !strings.Contains(detailOut, want) {
			t.Fatalf("tool-detail output missing %q:\n%s", want, detailOut)
		}
	}
	for _, forbid := range []string{"search thrash", "missing repo"} {
		if strings.Contains(detailOut, forbid) {
			t.Fatalf("tool-detail must not classify buckets, found %q:\n%s", forbid, detailOut)
		}
	}

	var withoutDetail bytes.Buffer
	RenderStream(&withoutDetail, res, RenderStreamOptions{})
	plainOut := withoutDetail.String()
	for _, forbid := range []string{"repeated", "unbounded reads", "suspect:", "tool detail unavailable"} {
		if strings.Contains(plainOut, forbid) {
			t.Fatalf("default output must not include tool-detail lines %q:\n%s", forbid, plainOut)
		}
	}
}

func TestRenderStreamToolDetailRenderBlindRefuses(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cap, _ := ResolveAgentAdapter("codex")
	res := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{{
			File:  "01-a.md",
			Title: "A",
			Attempts: []AttemptStream{{
				Timing: AttemptTiming{
					Agent:    "codex",
					Start:    base,
					Outcome:  "completed",
					Duration: 10 * time.Second,
				},
				ToolDetail: extractToolDetail("codex", nil),
			}},
		}},
	}

	var buf bytes.Buffer
	RenderStream(&buf, res, RenderStreamOptions{ToolDetail: true})
	out := buf.String()
	if !strings.Contains(out, "tool detail unavailable") {
		t.Fatalf("expected refusal, got:\n%s", out)
	}
	if !strings.Contains(out, cap.StreamRenderCapability().Reason) {
		t.Fatalf("expected declared reason %q, got:\n%s", cap.StreamRenderCapability().Reason, out)
	}
}
