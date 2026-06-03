package workload

import (
	"strings"
	"testing"
)

// Sample lines below follow the codex Thread Events schema (codex exec --json).
// The error/envelope path was captured from a real codex-cli 0.136.0 run; the
// success-path item events and field names come from the authoritative serde
// string literals compiled into that binary (auth was unavailable for a live
// success turn, and the installed codex is v0.7.0 which predates --json). See
// thoughts/research/agent-output-formats/codex.md.

func TestCodexLineRendererAgentMessageProse(t *testing.T) {
	render := codexLineRenderer(false)
	line := `{"type":"item.completed","item":{"type":"agent_message","text":"HELLO"}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("item.completed agent_message should be handled")
	}
	if got != "HELLO\n" {
		t.Fatalf("got %q, want %q", got, "HELLO\n")
	}
}

func TestCodexLineRendererAgentMessageTrimsTrailingNewlines(t *testing.T) {
	render := codexLineRenderer(false)
	line := `{"type":"item.completed","item":{"type":"agent_message","text":"line one\nline two\n"}}`
	got, _ := render([]byte(line))
	if got != "line one\nline two\n" {
		t.Fatalf("got %q, want %q", got, "line one\nline two\n")
	}
}

func TestCodexLineRendererCommandExecutionTick(t *testing.T) {
	render := codexLineRenderer(false)
	line := `{"type":"item.started","item":{"type":"command_execution","command":"go test ./...","status":"in_progress"}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("item.started command_execution should be handled")
	}
	if got != "→ command_execution go test ./...\n" {
		t.Fatalf("got %q, want %q", got, "→ command_execution go test ./...\n")
	}
}

func TestCodexLineRendererMcpToolCallTick(t *testing.T) {
	render := codexLineRenderer(false)
	line := `{"type":"item.started","item":{"type":"mcp_tool_call","server":"github","tool":"create_issue","arguments":{"title":"bug"}}}`
	got, _ := render([]byte(line))
	// command empty, so probe falls to tool (object arguments yield no hint).
	if got != "→ mcp_tool_call create_issue\n" {
		t.Fatalf("got %q, want %q", got, "→ mcp_tool_call create_issue\n")
	}
}

func TestCodexLineRendererMcpToolCallStringArguments(t *testing.T) {
	render := codexLineRenderer(false)
	// arguments as a JSON string: probe order is command, tool, server,
	// arguments — tool wins here, confirming probe precedence.
	line := `{"type":"item.started","item":{"type":"mcp_tool_call","tool":"search","arguments":"needle"}}`
	got, _ := render([]byte(line))
	if got != "→ mcp_tool_call search\n" {
		t.Fatalf("got %q, want %q", got, "→ mcp_tool_call search\n")
	}
}

func TestCodexLineRendererFileChangeTick(t *testing.T) {
	render := codexLineRenderer(false)
	line := `{"type":"item.started","item":{"type":"file_change","changes":[{"path":"workload/agent_codex.go","kind":"update"}]}}`
	got, _ := render([]byte(line))
	if got != "→ file_change workload/agent_codex.go\n" {
		t.Fatalf("got %q, want %q", got, "→ file_change workload/agent_codex.go\n")
	}
}

func TestCodexLineRendererWebSearchTick(t *testing.T) {
	render := codexLineRenderer(false)
	line := `{"type":"item.started","item":{"type":"web_search","query":"golang json streaming"}}`
	got, _ := render([]byte(line))
	if got != "→ web_search golang json streaming\n" {
		t.Fatalf("got %q, want %q", got, "→ web_search golang json streaming\n")
	}
}

func TestCodexLineRendererBareTick(t *testing.T) {
	render := codexLineRenderer(false)
	line := `{"type":"item.started","item":{"type":"command_execution"}}`
	got, _ := render([]byte(line))
	if got != "→ command_execution\n" {
		t.Fatalf("got %q, want %q", got, "→ command_execution\n")
	}
}

func TestCodexLineRendererHintProbeOrder(t *testing.T) {
	render := codexLineRenderer(false)
	// command present alongside other fields: command must win (first in order).
	line := `{"type":"item.started","item":{"type":"command_execution","command":"ls","query":"ignored","changes":[{"path":"ignored.go"}]}}`
	got, _ := render([]byte(line))
	if got != "→ command_execution ls\n" {
		t.Fatalf("got %q, want %q", got, "→ command_execution ls\n")
	}
}

func TestCodexLineRendererTruncatesHint(t *testing.T) {
	render := codexLineRenderer(false)
	long := strings.Repeat("x", 200)
	line := `{"type":"item.started","item":{"type":"command_execution","command":"` + long + `"}}`
	got, _ := render([]byte(line))
	want := "→ command_execution " + strings.Repeat("x", 77) + "...\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCodexLineRendererSkipsReasoningTodoLifecycleAndErrors(t *testing.T) {
	render := codexLineRenderer(false)
	for _, line := range []string{
		`{"type":"thread.started","thread_id":"abc"}`,
		`{"type":"turn.started"}`,
		`{"type":"turn.completed","usage":{"total_tokens":10}}`,
		`{"type":"item.started","item":{"type":"reasoning","text":"thinking"}}`,
		`{"type":"item.updated","item":{"type":"reasoning","text":"thinking more"}}`,
		`{"type":"item.completed","item":{"type":"reasoning","text":"done"}}`,
		`{"type":"item.started","item":{"type":"agent_message","text":"partial"}}`,
		`{"type":"item.updated","item":{"type":"agent_message","text":"partial text"}}`,
		`{"type":"item.started","item":{"type":"todo_list","items":[]}}`,
		`{"type":"item.completed","item":{"type":"todo_list","items":[]}}`,
		`{"type":"error","message":"Reconnecting... 1/5 (401 Unauthorized)"}`,
		`{"type":"turn.failed","error":{"message":"401 Unauthorized"}}`,
		`{"type":"item.completed","item":{"type":"error"}}`,
	} {
		got, handled := render([]byte(line))
		if !handled {
			t.Fatalf("structured event %q should be handled", line)
		}
		if got != "" {
			t.Fatalf("event %q should render nothing, got %q", line, got)
		}
	}
}

func TestCodexLineRendererNonJSONUnhandled(t *testing.T) {
	render := codexLineRenderer(false)
	got, handled := render([]byte("Reading additional input from stdin..."))
	if handled {
		t.Fatal("non-JSON line should be unhandled")
	}
	if got != "" {
		t.Fatalf("unhandled line should render nothing, got %q", got)
	}
}

func TestCodexLineRendererColorStylesTick(t *testing.T) {
	render := codexLineRenderer(true)
	line := `{"type":"item.started","item":{"type":"command_execution","command":"ls"}}`
	got, _ := render([]byte(line))
	want := ansiDim + "→ command_execution ls" + ansiReset + "\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
