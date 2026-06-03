package workload

import (
	"strings"
	"testing"
)

// Sample lines below are taken verbatim from a real pi run captured with
//   pi -p --mode json --model mimo-v2.5-pro 'Read the file hello.txt and tell me its contents.'
// (pi v0.77.0). The bare pop preset `pi -p --mode json` fails in the research
// env because pi's default thinking:medium is rejected by the only local
// provider; --model mimo-v2.5-pro selects a non-thinking model that streams
// cleanly. Event shapes are pi's provider-independent normalization layer.

func TestPiLineRendererTextDeltaNoNewline(t *testing.T) {
	render := piLineRenderer(false)
	line := `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"The file"}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("text_delta event should be handled")
	}
	if got != "The file" {
		t.Fatalf("got %q, want %q (no trailing newline)", got, "The file")
	}
}

func TestPiLineRendererTextDeltaConcatenationWithoutFraming(t *testing.T) {
	render := piLineRenderer(false)
	deltas := []string{"The file", " `hello.txt`", " contains:\n\n>", " The quick brown fox"}
	var b strings.Builder
	for _, d := range deltas {
		// json-encode the delta so embedded quotes/newlines are valid JSON
		line := `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":` + jsonString(d) + `}}`
		got, handled := render([]byte(line))
		if !handled {
			t.Fatalf("text_delta %q should be handled", d)
		}
		b.WriteString(got)
	}
	want := "The file `hello.txt` contains:\n\n> The quick brown fox"
	if b.String() != want {
		t.Fatalf("concatenated deltas = %q, want %q", b.String(), want)
	}
}

func TestPiLineRendererTextEndNewline(t *testing.T) {
	render := piLineRenderer(false)
	line := `{"type":"message_update","assistantMessageEvent":{"type":"text_end","contentIndex":1,"content":"The file ` + "`hello.txt`" + ` contains:"}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("text_end event should be handled")
	}
	if got != "\n" {
		t.Fatalf("got %q, want %q", got, "\n")
	}
}

func TestPiLineRendererToolTickPathHint(t *testing.T) {
	render := piLineRenderer(false)
	line := `{"type":"tool_execution_start","toolCallId":"call_c811e63a39b145f791e8677c","toolName":"read","args":{"path":"hello.txt"}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("tool_execution_start event should be handled")
	}
	if got != "→ read hello.txt\n" {
		t.Fatalf("got %q, want %q", got, "→ read hello.txt\n")
	}
}

func TestPiLineRendererToolTickFileFallback(t *testing.T) {
	render := piLineRenderer(false)
	line := `{"type":"tool_execution_start","toolName":"edit","args":{"file_path":"project/git.go"}}`
	got, _ := render([]byte(line))
	if got != "→ edit project/git.go\n" {
		t.Fatalf("got %q, want %q", got, "→ edit project/git.go\n")
	}
}

func TestPiLineRendererBareToolTick(t *testing.T) {
	render := piLineRenderer(false)
	line := `{"type":"tool_execution_start","toolName":"mystery","args":{"unknown":"x"}}`
	got, _ := render([]byte(line))
	if got != "→ mystery\n" {
		t.Fatalf("got %q, want %q", got, "→ mystery\n")
	}
}

func TestPiLineRendererTruncatesHint(t *testing.T) {
	render := piLineRenderer(false)
	long := strings.Repeat("x", 200)
	line := `{"type":"tool_execution_start","toolName":"bash","args":{"command":"` + long + `"}}`
	got, _ := render([]byte(line))
	want := "→ bash " + strings.Repeat("x", 77) + "...\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPiLineRendererSkipsThinkingAndLifecycle(t *testing.T) {
	render := piLineRenderer(false)
	for _, line := range []string{
		`{"type":"session","version":3,"id":"x"}`,
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_start","contentIndex":0}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"The user wants"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","contentIndex":0}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_start","contentIndex":1}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_start","contentIndex":1}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":1,"delta":"{\"path\": "}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[]}}`,
		`{"type":"tool_execution_end","toolName":"read","isError":false}`,
		`{"type":"turn_end","message":{"role":"assistant"}}`,
		`{"type":"agent_end","messages":[]}`,
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

func TestPiLineRendererAssistantErrorMessage(t *testing.T) {
	render := piLineRenderer(false)
	line := `{"type":"message_end","message":{"role":"assistant","errorMessage":"400 Error from provider"}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("assistant error message should be handled")
	}
	if got != "400 Error from provider\n" {
		t.Fatalf("got %q, want error line", got)
	}
}

func TestPiLineRendererNonJSONUnhandled(t *testing.T) {
	render := piLineRenderer(false)
	got, handled := render([]byte("zoxide: detected a possible configuration issue."))
	if handled {
		t.Fatal("non-JSON line should be unhandled")
	}
	if got != "" {
		t.Fatalf("unhandled line should render nothing, got %q", got)
	}
}

func TestPiLineRendererColorStylesToolTick(t *testing.T) {
	render := piLineRenderer(true)
	line := `{"type":"tool_execution_start","toolName":"read","args":{"path":"a.txt"}}`
	got, _ := render([]byte(line))
	want := ansiDim + "→ read a.txt" + ansiReset + "\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// jsonString quotes a Go string as a JSON string literal for use in test lines.
func jsonString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}
