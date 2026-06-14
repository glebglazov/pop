package deps

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func commandError(err error, stderr []byte) error {
	if msg := strings.TrimSpace(string(stderr)); msg != "" {
		return fmt.Errorf("%w: %s", err, msg)
	}
	return err
}

func outputError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return commandError(err, exit.Stderr)
	}
	return err
}
