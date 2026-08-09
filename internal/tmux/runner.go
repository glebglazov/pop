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
	// input runs tmux with args, writing text to the subprocess's stdin —
	// for verbs that stream data into tmux rather than reading it out (e.g.
	// load-buffer). It maps a non-zero exit into an error carrying tmux's
	// stderr.
	input(text string, args ...string) error
}

// execRunner is the real adapter: it shells out to the tmux binary.
// socket is the -L name handed at construction (ADR-0199). Empty means emit
// no socket flag — identical argv to pre-socket-key pop.
type execRunner struct {
	socket string
}

// withSocket prepends -L <socket> when a socket name is configured. An empty
// socket returns args unchanged so an upgrade with the key unset changes no
// argv at all.
func (r execRunner) withSocket(args []string) []string {
	if r.socket == "" {
		return args
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, "-L", r.socket)
	return append(out, args...)
}

func (r execRunner) output(args ...string) (string, error) {
	cmd := exec.Command("tmux", r.withSocket(args)...)
	out, err := cmd.Output()
	if err != nil {
		return "", outputError(err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r execRunner) attach(args ...string) error {
	cmd := exec.Command("tmux", r.withSocket(args)...)
	var stderr bytes.Buffer
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Run(); err != nil {
		return commandError(err, stderr.Bytes())
	}
	return nil
}

func (r execRunner) input(text string, args ...string) error {
	cmd := exec.Command("tmux", r.withSocket(args)...)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return commandError(err, stderr.Bytes())
	}
	return nil
}
