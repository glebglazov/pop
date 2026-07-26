package tmux

import (
	"bytes"
	"io"
	"os"
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
	// attach runs tmux with args wired to the process's own stdio, so an
	// interactive client (attach-session) takes over the terminal. It maps a
	// non-zero exit into an error carrying tmux's stderr.
	attach(args ...string) error
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

func (execRunner) attach(args ...string) error {
	cmd := exec.Command("tmux", args...)
	var stderr bytes.Buffer
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Run(); err != nil {
		return commandError(err, stderr.Bytes())
	}
	return nil
}
