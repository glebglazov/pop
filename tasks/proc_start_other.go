//go:build !linux && !darwin

package tasks

// procStartSupported is false here: no portable way to read process start time
// outside linux and darwin. This is the documented boundary where PID-reuse
// defense is unavailable; WarnProcStartUnsupported surfaces it at startup.
const procStartSupported = false

// defaultProcStartToken has no portable implementation outside linux and darwin.
// Returning ok=false makes drain liveness fall back to bare PID liveness, which
// is correct but cannot defeat PID reuse on these platforms.
func defaultProcStartToken(pid int) (string, bool) {
	return "", false
}
