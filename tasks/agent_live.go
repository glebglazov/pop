package tasks

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
)

// lineRenderer turns one complete output line into the text shown live.
// handled is false when the line is not a recognized structured event, in
// which case the raw line is passed through unchanged (almost always stderr).
// handled is true with an empty string for events that render nothing.
type lineRenderer func(line []byte) (rendered string, handled bool)

// lineRendererFor returns the live renderer for a structured format, or nil
// when the format has no renderer yet (its output is captured silently).
func lineRendererFor(format AgentOutputFormat, color bool) lineRenderer {
	switch format {
	case AgentOutputClaudeStreamJSON:
		return claudeLineRenderer(color)
	case AgentOutputCursorStreamJSON:
		return cursorLineRenderer(color)
	case AgentOutputCodexJSONL:
		return codexLineRenderer(color)
	case AgentOutputOpenCodeJSON:
		return openCodeLineRenderer(color)
	case AgentOutputPiJSONL:
		return piLineRenderer(color)
	default:
		return nil
	}
}

// liveRenderWriter tees an agent's structured output: raw bytes go to capture
// unchanged (the source of truth for assessment and quota detection), while
// each complete line is rendered to live as a progress view. Rendering is
// cosmetic and never feeds the completion contract. Each rendered entry is
// prefixed with the elapsed time since the previous live line — the first
// since attempt start (construction time).
type liveRenderWriter struct {
	live    io.Writer
	capture io.Writer
	render  lineRenderer
	now     func() time.Time
	last    time.Time
	buf     []byte
}

func newLiveRenderWriter(live, capture io.Writer, render lineRenderer, now func() time.Time) *liveRenderWriter {
	return &liveRenderWriter{live: live, capture: capture, render: render, now: now, last: now()}
}

func (w *liveRenderWriter) Write(p []byte) (int, error) {
	if _, err := w.capture.Write(p); err != nil {
		return 0, err
	}
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

// Flush renders any trailing line not terminated by a newline.
func (w *liveRenderWriter) Flush() {
	if len(w.buf) == 0 {
		return
	}
	w.emit(w.buf)
	w.buf = w.buf[:0]
}

func (w *liveRenderWriter) emit(line []byte) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return
	}
	rendered, handled := w.render(trimmed)
	if !handled {
		fmt.Fprintln(w.live, string(line))
		w.last = w.now()
		return
	}
	if rendered == "" {
		return
	}
	at := w.now()
	fmt.Fprint(w.live, prefixStreamDelta(rendered, at.Sub(w.last)))
	w.last = at
}

// streamDeltaWidth right-aligns markers up to "+59.9s" so entry columns line up.
const streamDeltaWidth = 6

// prefixStreamDelta prefixes a rendered entry with a "+Xs" gap marker
// ("+2.3s  → Bash go test ./..."). Continuation lines of a multi-line entry
// get matching blank padding so the entry column stays aligned.
func prefixStreamDelta(rendered string, delta time.Duration) string {
	marker := fmt.Sprintf("%*s", streamDeltaWidth, formatStreamDelta(delta))
	pad := strings.Repeat(" ", len(marker))
	var b strings.Builder
	for i, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
		if i == 0 {
			b.WriteString(marker)
		} else {
			b.WriteString(pad)
		}
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// formatStreamDelta renders a gap as "+2.3s" under a minute and "+1m05s" above.
func formatStreamDelta(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	tenths := (d + 50*time.Millisecond) / (100 * time.Millisecond)
	if tenths < 600 {
		return fmt.Sprintf("+%d.%ds", tenths/10, tenths%10)
	}
	secs := (d + 500*time.Millisecond) / time.Second
	return fmt.Sprintf("+%dm%02ds", secs/60, secs%60)
}
