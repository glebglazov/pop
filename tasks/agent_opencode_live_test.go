package tasks

import (
	"strings"
	"testing"
)

func TestOpenCodeLineRendererText(t *testing.T) {
	render := openCodeLineRenderer(false)
	line := `{"type":"text","part":{"text":"Hello there\n"}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("text event should be handled")
	}
	if got != "Hello there\n" {
		t.Fatalf("got %q, want %q", got, "Hello there\n")
	}
}

func TestOpenCodeLineRendererToolTickCamelCaseFilePath(t *testing.T) {
	render := openCodeLineRenderer(false)
	line := `{"type":"tool_use","part":{"tool":"read","state":{"input":{"filePath":"project/git.go"}}}}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("tool_use event should be handled")
	}
	if got != "→ read project/git.go\n" {
		t.Fatalf("got %q, want %q", got, "→ read project/git.go\n")
	}
}

func TestOpenCodeLineRendererTitleFallback(t *testing.T) {
	render := openCodeLineRenderer(false)
	line := `{"type":"tool_use","part":{"tool":"mystery","title":"doing a thing","state":{"input":{"unknown":"x"}}}}`
	got, _ := render([]byte(line))
	if got != "→ mystery doing a thing\n" {
		t.Fatalf("got %q, want %q", got, "→ mystery doing a thing\n")
	}
}

func TestOpenCodeLineRendererBareToolTick(t *testing.T) {
	render := openCodeLineRenderer(false)
	line := `{"type":"tool_use","part":{"tool":"mystery","state":{"input":{"unknown":"x"}}}}`
	got, _ := render([]byte(line))
	if got != "→ mystery\n" {
		t.Fatalf("got %q, want %q", got, "→ mystery\n")
	}
}

func TestOpenCodeLineRendererTruncatesHint(t *testing.T) {
	render := openCodeLineRenderer(false)
	long := strings.Repeat("x", 200)
	line := `{"type":"tool_use","part":{"tool":"bash","state":{"input":{"command":"` + long + `"}}}}`
	got, _ := render([]byte(line))
	want := "→ bash " + strings.Repeat("x", 77) + "...\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestOpenCodeLineRendererTruncatesTitleFallback(t *testing.T) {
	render := openCodeLineRenderer(false)
	long := strings.Repeat("y", 200)
	line := `{"type":"tool_use","part":{"tool":"grep","title":"` + long + `","state":{"input":{}}}}`
	got, _ := render([]byte(line))
	want := "→ grep " + strings.Repeat("y", 77) + "...\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestOpenCodeLineRendererSkipsStepEvents(t *testing.T) {
	render := openCodeLineRenderer(false)
	for _, line := range []string{
		`{"type":"step_start","part":{}}`,
		`{"type":"step_finish","part":{}}`,
		`{"type":"error","error":{"data":{"message":"boom"}}}`,
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

func TestOpenCodeLineRendererNonJSONUnhandled(t *testing.T) {
	render := openCodeLineRenderer(false)
	got, handled := render([]byte("warning: something broke"))
	if handled {
		t.Fatal("non-JSON line should be unhandled")
	}
	if got != "" {
		t.Fatalf("unhandled line should render nothing, got %q", got)
	}
}

func TestOpenCodeLineRendererColorStylesToolTick(t *testing.T) {
	render := openCodeLineRenderer(true)
	line := `{"type":"tool_use","part":{"tool":"read","state":{"input":{"filePath":"a.go"}}}}`
	got, _ := render([]byte(line))
	want := ansiDim + "→ read a.go" + ansiReset + "\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
