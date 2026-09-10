// Package shell constructs commands with the configuration-loading behaviour
// required by their output contract.
package shell

import "os/exec"

// Bare constructs a configuration-free shell command for output that Pop parses.
func Bare(command string) *exec.Cmd {
	argv := BareArgs(command)
	return exec.Command(argv[0], argv[1:]...)
}

// BareArgs returns the bare shell invocation for a caller that starts it later.
func BareArgs(command string) []string {
	return []string{"sh", "-c", command}
}

// Human constructs a command in the Human shell. Interactive mode makes the
// shell read the configuration that defines the human's functions and aliases.
func Human(name, command string) *exec.Cmd {
	return exec.Command(name, "-i", "-c", command)
}
