package tasks

import (
	"fmt"
	"io"
)

// procStartSupported is set per platform (see proc_start_{linux,darwin,other}.go)
// and is the single documented decision point for PID-reuse defense: it is true
// only on platforms that can read a process's start time. On platforms where it
// is false, drain and supervisor-lock liveness degrade to bare PID liveness,
// which cannot distinguish a genuinely-live process from a different process
// that reused a crashed one's PID. That is a stated boundary, not a latent bug —
// callers surface it via WarnProcStartUnsupported at startup.

// ProcStartSupported reports whether this platform can read process start
// tokens (currently linux and darwin). When false, PID-reuse defense is
// unavailable and liveness falls back to bare PID checks.
func ProcStartSupported() bool { return procStartSupported }

// WarnProcStartUnsupported writes a one-line startup warning to w when the
// running platform cannot read process start times, making the degraded
// liveness guarantee explicit to operators. It is a no-op on supported
// platforms (and when w is nil).
func WarnProcStartUnsupported(w io.Writer) {
	if procStartSupported || w == nil {
		return
	}
	fmt.Fprintln(w, "warning: this platform cannot read process start times; "+
		"process liveness falls back to bare PID checks and cannot defeat PID reuse, "+
		"so a crashed drain whose PID is reused may read as live and not self-heal")
}
