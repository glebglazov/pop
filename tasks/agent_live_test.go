package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// staticClock freezes time so every rendered entry carries a "+0.0s" marker.
func staticClock() func() time.Time {
	at := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

// scriptedClock returns the given instants in order; the first is consumed by
// the writer's constructor as the attempt start.
func scriptedClock(t *testing.T, times ...time.Time) func() time.Time {
	i := 0
	return func() time.Time {
		if i >= len(times) {
			t.Fatalf("clock called %d times, scripted only %d", i+1, len(times))
		}
		at := times[i]
		i++
		return at
	}
}

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

func TestClaudeLineRendererPrintsInitModelOnce(t *testing.T) {
	render := claudeLineRenderer(false)
	line := `{"type":"system","subtype":"init","model":"claude-sonnet-4-20250514"}`
	got, handled := render([]byte(line))
	if !handled {
		t.Fatal("system init event should be handled")
	}
	if got != "model claude-sonnet-4-20250514\n" {
		t.Fatalf("got %q, want model line", got)
	}
	got, handled = render([]byte(line))
	if !handled || got != "" {
		t.Fatalf("second init model should be handled silently, got %q handled=%v", got, handled)
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

func TestClaudeLineRendererColorStylesInitModel(t *testing.T) {
	render := claudeLineRenderer(true)
	line := `{"type":"system","subtype":"init","model":"claude-opus-4-20250514"}`
	got, _ := render([]byte(line))
	want := ansiDim + "model claude-opus-4-20250514" + ansiReset + "\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLiveRenderWriterPrintsInitModel(t *testing.T) {
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false), staticClock())

	raw := `{"type":"system","subtype":"init","model":"claude-sonnet-4-20250514"}` + "\n"
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if capture.String() != raw {
		t.Fatalf("capture got %q, want exact raw %q", capture.String(), raw)
	}
	if live.String() != " +0.0s  model claude-sonnet-4-20250514\n" {
		t.Fatalf("live got %q", live.String())
	}
}

func TestLiveRenderWriterCapturesRawAndRendersLines(t *testing.T) {
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false), staticClock())

	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}` + "\n" +
		`{"type":"result","result":"done"}` + "\n"
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}

	if capture.String() != raw {
		t.Fatalf("capture got %q, want exact raw %q", capture.String(), raw)
	}
	if live.String() != " +0.0s  hi\n" {
		t.Fatalf("live got %q, want %q", live.String(), " +0.0s  hi\n")
	}
}

func TestLiveRenderWriterBuffersPartialLinesAcrossWrites(t *testing.T) {
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false), staticClock())

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
	if live.String() != " +0.0s  chunked\n" {
		t.Fatalf("live got %q, want %q", live.String(), " +0.0s  chunked\n")
	}
	if capture.String() != full {
		t.Fatalf("capture got %q, want %q", capture.String(), full)
	}
}

func TestLiveRenderWriterPassesThroughNonJSON(t *testing.T) {
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false), staticClock())

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
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false), staticClock())

	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"no newline"}]}}`
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if live.String() != "" {
		t.Fatalf("unterminated line should not render before flush, got %q", live.String())
	}
	w.Flush()
	if live.String() != " +0.0s  no newline\n" {
		t.Fatalf("live got %q, want %q", live.String(), " +0.0s  no newline\n")
	}
}

func TestLiveRenderWriterDeltaSincePreviousEntry(t *testing.T) {
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	var live, capture bytes.Buffer
	// Constructor consumes the first instant as attempt start; the first
	// entry's delta is measured from it, the second from the first entry.
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false), scriptedClock(t,
		start,
		start.Add(2300*time.Millisecond),
		start.Add(3000*time.Millisecond),
	))

	raw := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}` + "\n"
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}

	want := " +2.3s  → Bash go test ./...\n" +
		" +0.7s  done\n"
	if live.String() != want {
		t.Fatalf("live got %q, want %q", live.String(), want)
	}
	if capture.String() != raw {
		t.Fatalf("capture got %q, want exact raw %q", capture.String(), raw)
	}
}

func TestLiveRenderWriterSilentEventsDoNotResetDelta(t *testing.T) {
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false), scriptedClock(t,
		start,
		start.Add(4*time.Second),
	))

	// system/result events render nothing and must not consume the clock.
	raw := `{"type":"system","subtype":"init"}` + "\n" +
		`{"type":"result","result":"done"}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}` + "\n"
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if live.String() != " +4.0s  hi\n" {
		t.Fatalf("live got %q, want %q", live.String(), " +4.0s  hi\n")
	}
}

func TestLiveRenderWriterPassthroughCountsAsEmittedLine(t *testing.T) {
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false), scriptedClock(t,
		start,
		start.Add(1*time.Second),
		start.Add(3500*time.Millisecond),
	))

	raw := "warning: noise\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}` + "\n"
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	// The raw stderr line stays unprefixed but still advances the gap origin.
	want := "warning: noise\n +2.5s  hi\n"
	if live.String() != want {
		t.Fatalf("live got %q, want %q", live.String(), want)
	}
}

func TestLiveRenderWriterAlignsMultiLineEntry(t *testing.T) {
	var live, capture bytes.Buffer
	w := newLiveRenderWriter(&live, &capture, claudeLineRenderer(false), staticClock())

	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"Reading"},{"type":"tool_use","name":"Read","input":{"path":"a.go"}}]}}` + "\n"
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	want := " +0.0s  Reading\n" +
		"        → Read a.go\n"
	if live.String() != want {
		t.Fatalf("live got %q, want %q", live.String(), want)
	}
}

func TestFormatStreamDelta(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "+0.0s"},
		{0, "+0.0s"},
		{40 * time.Millisecond, "+0.0s"},
		{2300 * time.Millisecond, "+2.3s"},
		{2350 * time.Millisecond, "+2.4s"},
		{59800 * time.Millisecond, "+59.8s"},
		{59960 * time.Millisecond, "+1m00s"},
		{time.Minute, "+1m00s"},
		{75 * time.Second, "+1m15s"},
		{10*time.Minute + 5*time.Second, "+10m05s"},
	}
	for _, tc := range cases {
		if got := formatStreamDelta(tc.d); got != tc.want {
			t.Errorf("formatStreamDelta(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestPlainFormatHasNoLineRenderer(t *testing.T) {
	// Plain output never goes through the line-rendered path, so it can never
	// pick up a delta prefix.
	if render := lineRendererFor(AgentOutputPlain, false); render != nil {
		t.Fatal("plain format should have no live renderer")
	}
}
