package queue

import (
	"fmt"
	"os/exec"
	"strings"
)

// statusExecCommand builds `pop tasks <verb> <set>` for the dashboard
// ExecProcess path so whole-set multi-task selection runs in a real TTY.
func statusExecCommand(row DashboardRow, verb string) *exec.Cmd {
	args := []string{"tasks", verb, row.SetID}
	cmd := exec.Command("pop", args...)
	wd := strings.TrimSpace(row.ProjectPath)
	if wd == "" {
		wd = strings.TrimSpace(row.RuntimePath)
	}
	if wd != "" {
		cmd.Dir = wd
	}
	return cmd
}

func statusExecError(stderr string, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(stderr)
	if msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return err
}
