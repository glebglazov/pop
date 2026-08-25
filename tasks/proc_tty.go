package tasks

import (
	"os/exec"
	"strconv"
	"strings"
)

// processTTY names the controlling terminal of pid — "s003", "pts/3" — or "" when
// the process has none or cannot be inspected. It is the "where to reach them"
// half of an Admission wait line: a holder with a tty is a human sitting at a
// prompt somewhere, and naming the terminal is what turns an unbounded wait into
// an errand.
func processTTY(d *Deps, pid int) string {
	if d != nil && d.ProcessTTY != nil {
		return d.ProcessTTY(pid)
	}
	return defaultProcessTTY(pid)
}

// defaultProcessTTY reads the controlling terminal from ps, which reports it the
// same way on every platform pop runs on. A detached process prints one of the
// no-terminal markers below, which read as "unknown" rather than as a name.
func defaultProcessTTY(pid int) string {
	if pid <= 0 {
		return ""
	}
	out, err := exec.Command("ps", "-o", "tty=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(out))
	switch name {
	case "", "?", "??", "-":
		return ""
	}
	return name
}
