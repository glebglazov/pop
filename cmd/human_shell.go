package cmd

import (
	"io"
	"os"
	"strings"

	shellcmd "github.com/glebglazov/pop/internal/shell"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
)

func humanShell(mod tmuxmod.Tmux) string {
	if name, err := mod.DefaultShell(); err == nil && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if name := strings.TrimSpace(os.Getenv("SHELL")); name != "" {
		return name
	}
	return "/bin/sh"
}

func runHumanShellCommand(mod tmuxmod.Tmux, command, dir string, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := shellcmd.Human(humanShell(mod), command)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
