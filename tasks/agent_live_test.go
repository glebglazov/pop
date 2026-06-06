package tasks

import (
	"bytes"
	"strings"
	"testing"
)

func TestClaudeLineRendererAssistantText(t *testing.T) {
	render := claudeLineRenderer(false)
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello there\n"}]}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("assistant event should be handled")
	}
	if got != "Hello there\n" {
		t.Fatalf("got %q, want %q", got, "Hello there\n")
	}
}

func TestClaudeLineRendererToolTickWithHint(t *testing.T) {
	render := claudeLineRenderer(false)
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"project/git.go"}}]}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("tool_use event should be handled")
	}
	if got != "→ Read project/git.go\n" {
		t.Fatalf("got %q, want %q", got, "→ Read project/git.go\n")
	}
}

func TestClaudeLineRendererBareToolTick(t *testing.T) {
	render := claudeLineRenderer(false)
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Mystery","input":{"unknown":"x"}}]}}`
	got, _ := render([]byte(line))
	if got != "→ Mystery\n" {
		t.Fatalf("got %q, want %q", got, "→ Mystery\n")
	}
}

func TestClaudeLineRendererTruncatesHint(t *testing.T) {
	render := claudeLineRenderer(false)
	long := strings.Repeat("x", 200)
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"` + long + `"}}]}}`
	got, _ := render([]byte(line))
	want := "→ Bash " + strings.Repeat("x", 77) + "...\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestClaudeLineRendererTextThenTool(t *testing.T) {
	render := claudeLineRenderer(false)
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Reading"},{"type":"tool_use","name":"Read","input":{"path":"a.go"}}]}}`
	got, _ := render([]byte(line))
	want := "Reading\n→ Read a.go\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestClaudeLineRendererSkipsNonAssistantEvents(t *testing.T) {
	render := claudeLineRenderer(false)
	for _, line := range []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"user","message":{"content":[]}}`,
		`{"type":"result","result":"done"}`,
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

func TestClaudeLineRendererNonJSONUnhandled(t *testing.T) {
	render := claudeLineRenderer(false)
	got, handled := render([]byte("warning: something broke"))
	if handled {
		t.Fatal("non-JSON line should be unhandled")
	}
	if got != "" {
		t.Fatalf("unhandled line should render nothing, got %q", got)
	}
}

func TestClaudeLineRendererColorStylesToolTick(t *testing.T) {
	render := claudeLineRenderer(true)
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"a.go"}}]}}`
	got, _ := render([]byte(line))
	want := ansiDim + "→ Read a.go" + ansiReset + "\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLiveRenderWriterCapturesRawAndRendersLines(t *testing.T) {
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false))

	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}` + "\n" +
		`{"type":"result","result":"done"}` + "\n"
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}

	if capture.String() != raw {
		t.Fatalf("capture got %q, want exact raw %q", capture.String(), raw)
	}
	if live.String() != "hi\n" {
		t.Fatalf("live got %q, want %q", live.String(), "hi\n")
	}
}

func TestLiveRenderWriterBuffersPartialLinesAcrossWrites(t *testing.T) {
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false))

	full := `{"type":"assistant","message":{"content":[{"type":"text","text":"chunked"}]}}` + "\n"
	mid := len(full) / 2
	if _, err := w.Write([]byte(full[:mid])); err != nil {
		t.Fatal(err)
	}
	if live.String() != "" {
		t.Fatalf("partial line should not render yet, got %q", live.String())
	}
	if _, err := w.Write([]byte(full[mid:])); err != nil {
		t.Fatal(err)
	}
	if live.String() != "chunked\n" {
		t.Fatalf("live got %q, want %q", live.String(), "chunked\n")
	}
	if capture.String() != full {
		t.Fatalf("capture got %q, want %q", capture.String(), full)
	}
}

func TestLiveRenderWriterPassesThroughNonJSON(t *testing.T) {
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false))

	raw := "panic: runtime error\n"
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if live.String() != raw {
		t.Fatalf("live got %q, want raw passthrough %q", live.String(), raw)
	}
	if capture.String() != raw {
		t.Fatalf("capture got %q, want %q", capture.String(), raw)
	}
}

func TestLiveRenderWriterFlushRendersTrailingLine(t *testing.T) {
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false))

	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"no newline"}]}}`
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if live.String() != "" {
		t.Fatalf("unterminated line should not render before flush, got %q", live.String())
	}
	w.Flush()
	if live.String() != "no newline\n" {
		t.Fatalf("live got %q, want %q", live.String(), "no newline\n")
	}
}
