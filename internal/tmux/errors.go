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
