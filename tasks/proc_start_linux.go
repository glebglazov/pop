//go:build linux

package tasks

import (
	"os"
	"strconv"
	"strings"
)

// procStartSupported: linux reads process start time from /proc/<pid>/stat.
const procStartSupported = true

// defaultProcStartToken reads the process's start time from /proc/<pid>/stat
// (field 22, starttime in clock ticks since boot). The token is stable for the
// life of the process and distinct after a PID is reused, so pairing it with the
// PID defeats reuse in drain liveness. The comm field (field 2) may itself
// contain spaces and parentheses, so fields are taken after the final ')'.
func defaultProcStartToken(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", false
	}
	stat := string(data)
	close := strings.LastIndexByte(stat, ')')
	if close < 0 || close+2 >= len(stat) {
		return "", false
	}
	// Fields after the final ')' start at field 3 (state); starttime is field 22,
	// i.e. index 19 in this tail.
	tail := strings.Fields(stat[close+2:])
	const starttimeIndex = 19
	if len(tail) <= starttimeIndex {
		return "", false
	}
	return tail[starttimeIndex], true
}
