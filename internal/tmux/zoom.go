package tmux

import (
	"strings"

	"github.com/glebglazov/pop/debug"
)

// Window-zoom primitives and the switch-and-zoom composite behind the
// dashboard "jump to pane" action.

// WindowZoomed reports whether target's window is currently zoomed.
func (t *realTmux) WindowZoomed(target string) (bool, error) {
	out, err := t.run.output("display-message", "-t", target, "-p", "#{window_zoomed_flag}")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "1", nil
}

// ZoomPane toggles the zoom state of target's window (resize-pane -Z).
func (t *realTmux) ZoomPane(target string) error {
	_, err := t.run.output("resize-pane", "-Z", "-t", target)
	return err
}

// SwitchAndZoom jumps the client to target and maximizes target's window,
// preserving the zoom-only-if-not-zoomed behaviour: an already-zoomed window is
// left maximized rather than toggled back to a split layout. Inside tmux it
// switches first, then zooms; outside tmux it must zoom before attaching, since
// attach-session takes over the terminal and no further command can run after
// it. A pre-attach zoom failure is best-effort (logged, not fatal).
func SwitchAndZoom(t Tmux, target string) error {
	zoomed, err := t.WindowZoomed(target)
	if err != nil {
		// An unreadable flag is treated as not-zoomed, so we still ensure zoom.
		zoomed = false
	}
	if t.InTmux() {
		if err := t.SwitchClient(target); err != nil {
			return err
		}
		if zoomed {
			return nil
		}
		return t.ZoomPane(target)
	}
	if err := refuseIfForeignServer(t); err != nil {
		return err
	}
	if !zoomed {
		if err := t.ZoomPane(target); err != nil {
			debug.Error("SwitchAndZoom: pre-attach zoom: %v", err)
		}
	}
	return t.AttachSession(target)
}
