package workload

import (
	"bytes"
	"fmt"
	"io"
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
// cosmetic and never feeds the completion contract.
type liveRenderWriter struct {
	live    io.Writer
	capture io.Writer
	render  lineRenderer
	buf     []byte
}

func newLiveRenderWriter(live, capture io.Writer, render lineRenderer) *liveRenderWriter {
	return &liveRenderWriter{live: live, capture: capture, render: render}
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
		return
	}
	if rendered != "" {
		fmt.Fprint(w.live, rendered)
	}
}
