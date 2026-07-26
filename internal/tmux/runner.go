package tmux

import (
	"os/exec"
	"strings"
)

// runner is the subprocess seam for the module. The real adapter shells out
// to tmux; the module's own tests substitute a recording fake. Argument
// vectors are asserted only against that fake, once per verb — no consumer
// test ever sees an argument array.
type runner interface {
	// output runs tmux with args and returns its trimmed stdout, mapping a
	// non-zero exit into an error that carries tmux's stderr.
	output(args ...string) (string, error)
}

// execRunner is the real adapter: it shells out to the tmux binary.
type execRunner struct{}

func (execRunner) output(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", outputError(err)
	}
	return strings.TrimSpace(string(out)), nil
}
