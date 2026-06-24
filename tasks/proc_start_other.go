//go:build !linux && !darwin

package tasks

// defaultProcStartToken has no portable implementation outside linux and darwin.
// Returning ok=false makes drain liveness fall back to bare PID liveness, which
// is correct but cannot defeat PID reuse on these platforms.
func defaultProcStartToken(pid int) (string, bool) {
	return "", false
}
