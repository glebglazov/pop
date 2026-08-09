package tmux

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// commandError attaches trimmed stderr to err, when there is any.
func commandError(err error, stderr []byte) error {
	if msg := strings.TrimSpace(string(stderr)); msg != "" {
		return fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

// outputError enriches an *exec.ExitError from cmd.Output() with the stderr
// tmux captured on that error.
func outputError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return commandError(err, exit.Stderr)
	}
	return err
}

// absentServer reports whether err is tmux saying no server is listening on
// the addressed socket. tmux itself does not start a server for list-* reads
// (ADR-0199 decision 8); the wording varies by version and by whether the
// socket file is missing ("error connecting to …") or the server has already
// exited ("no server running on …").
func absentServer(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no server") || strings.Contains(msg, "error connecting to")
}
