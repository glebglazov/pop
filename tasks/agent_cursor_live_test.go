package tasks

import (
	"strings"
	"testing"
)

// Sample lines below are reconstructed from cursor-agent's bundled stream-json
// serializer (chat-f4pxghr6.js inside version 2025.09.12-4852336), the
// authoritative source for the wire shapes. A live authenticated capture was
// not possible in the research env: the installed binary is not logged in
// (drops into an interactive sign-in TUI instead of JSON) and rejects pop's
// preset flags --trust/--workspace, so it targets a newer build than the one
// available. The serializer was re-read to settle the framing question: each
// event is written as `${JSON.stringify(T)}\n` — the template literal embeds a
// trailing newline before its closing backtick — so events ARE \n-delimited
// NDJSON, contradicting the earlier "no trailing newline" note. The
// liveRenderWriter line-splitter therefore holds for this build.
//
// Assistant text is INCREMENTAL: each "assistant" event carries only D.text
// (the delta), accumulated server-side into B; result.result is the full B.

func TestCursorLineRendererTextDeltaNoNewline(t *testing.T) {
	render := cursorLineRenderer(false)
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"The file"}]},"session_id":"s"}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("assistant event should be handled")
	}
	if got != "The file" {
		t.Fatalf("got %q, want %q (no trailing newline)", got, "The file")
	}
}

func TestCursorLineRendererDeltaConcatenationWithoutFraming(t *testing.T) {
	render := cursorLineRenderer(false)
	deltas := []string{"The file", " `hello.txt`", " contains:\n\n>", " The quick brown fox"}
	var b strings.Builder
	for _, d := range deltas {
		line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":` + jsonString(d) + `}]},"session_id":"s"}`
		got, handled := render([]byte(line))
		if !handled {
			t.Fatalf("assistant delta %q should be handled", d)
		}
		b.WriteString(got)
	}
	want := "The file `hello.txt` contains:\n\n> The quick brown fox"
	if b.String() != want {
		t.Fatalf("concatenated deltas = %q, want %q", b.String(), want)
	}
}

func TestCursorLineRendererToolTickStartedPathHint(t *testing.T) {
	render := cursorLineRenderer(false)
	line := `{"type":"tool_call","subtype":"started","call_id":"c1","tool_call":{"tool":{"case":"readToolCall","value":{"args":{"path":"hello.txt"}}}},"session_id":"s"}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("tool_call started event should be handled")
	}
	if got != "→ readToolCall hello.txt\n" {
		t.Fatalf("got %q, want %q", got, "→ readToolCall hello.txt\n")
	}
}

func TestCursorLineRendererToolTickStartedKeyedLiveShape(t *testing.T) {
	render := cursorLineRenderer(false)
	line := `{"type":"tool_call","subtype":"started","call_id":"c1","tool_call":{"readToolCall":{"args":{"path":"/tmp/probe/hello.txt"}}},"session_id":"s"}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("tool_call started event should be handled")
	}
	if got != "→ readToolCall /tmp/probe/hello.txt\n" {
		t.Fatalf("got %q, want %q", got, "→ readToolCall /tmp/probe/hello.txt\n")
	}
}

func TestCursorLineRendererToolTickShellCommandHint(t *testing.T) {
	render := cursorLineRenderer(false)
	line := `{"type":"tool_call","subtype":"started","tool_call":{"tool":{"case":"shellToolCall","value":{"args":{"command":"ls -la"}}}}}`
	got, _ := render([]byte(line))
	if got != "→ shellToolCall ls -la\n" {
		t.Fatalf("got %q, want %q", got, "→ shellToolCall ls -la\n")
	}
}

func TestCursorLineRendererToolTickGlobPatternHint(t *testing.T) {
	render := cursorLineRenderer(false)
	line := `{"type":"tool_call","subtype":"started","tool_call":{"tool":{"case":"globToolCall","value":{"args":{"globPattern":"**/*.go"}}}}}`
	got, _ := render([]byte(line))
	if got != "→ globToolCall **/*.go\n" {
		t.Fatalf("got %q, want %q", got, "→ globToolCall **/*.go\n")
	}
}

func TestCursorLineRendererToolTickCompletedSkipped(t *testing.T) {
	render := cursorLineRenderer(false)
	line := `{"type":"tool_call","subtype":"completed","tool_call":{"tool":{"case":"readToolCall","value":{"args":{"path":"hello.txt"}}}}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("tool_call completed event should be handled")
	}
	if got != "" {
		t.Fatalf("completed should render nothing, got %q", got)
	}
}

func TestCursorLineRendererBareToolTick(t *testing.T) {
	render := cursorLineRenderer(false)
	line := `{"type":"tool_call","subtype":"started","tool_call":{"tool":{"case":"updateTodosToolCall","value":{"args":{"unknown":"x"}}}}}`
	got, _ := render([]byte(line))
	if got != "→ updateTodosToolCall\n" {
		t.Fatalf("got %q, want %q", got, "→ updateTodosToolCall\n")
	}
}

func TestCursorLineRendererTruncatesHint(t *testing.T) {
	render := cursorLineRenderer(false)
	long := strings.Repeat("x", 200)
	line := `{"type":"tool_call","subtype":"started","tool_call":{"tool":{"case":"shellToolCall","value":{"args":{"command":"` + long + `"}}}}}`
	got, _ := render([]byte(line))
	want := "→ shellToolCall " + strings.Repeat("x", 77) + "...\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCursorLineRendererSkipsSystemUserResult(t *testing.T) {
	render := cursorLineRenderer(false)
	for _, line := range []string{
		`{"type":"system","subtype":"init","apiKeySource":"login","cwd":"/p","session_id":"s","model":"m","permissionMode":"default"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"do a thing"}]},"session_id":"s"}`,
		`{"type":"result","subtype":"success","duration_ms":12,"is_error":false,"result":"all done","session_id":"s"}`,
		`{"type":"thinking-delta","delta":"hmm"}`,
		`{"type":"mystery"}`,
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

func TestCursorLineRendererNonJSONUnhandled(t *testing.T) {
	render := cursorLineRenderer(false)
	got, handled := render([]byte("Press any key to sign in..."))
	if handled {
		t.Fatal("non-JSON line should be unhandled")
	}
	if got != "" {
		t.Fatalf("unhandled line should render nothing, got %q", got)
	}
}

func TestCursorLineRendererColorStylesToolTick(t *testing.T) {
	render := cursorLineRenderer(true)
	line := `{"type":"tool_call","subtype":"started","tool_call":{"tool":{"case":"readToolCall","value":{"args":{"path":"a.txt"}}}}}`
	got, _ := render([]byte(line))
	want := ansiDim + "→ readToolCall a.txt" + ansiReset + "\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
