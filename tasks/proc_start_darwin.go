//go:build darwin

package tasks

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// defaultProcStartToken reads the process's start time from the kernel via
// sysctl(KERN_PROC_PID). The token is stable for the life of the process and
// distinct after a PID is reused, so pairing it with the PID defeats reuse in
// drain liveness.
func defaultProcStartToken(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp == nil {
		return "", false
	}
	st := kp.Proc.P_starttime
	return strconv.FormatInt(st.Sec, 10) + "." + strconv.FormatInt(int64(st.Usec), 10), true
}
