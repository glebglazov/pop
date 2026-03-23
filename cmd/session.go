package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/ui"
)

const (
	tmuxSessionPathPrefix = "tmux:"
	iconDirSession        = "■"
	iconStandaloneSession = "□"
	iconAttention         = "!"
)

func currentTmuxSession() string {
	out, err := exec.Command("tmux", "display-message", "-p", "#S").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func isStandaloneSession(item ui.Item) bool {
	return strings.HasPrefix(item.Path, tmuxSessionPathPrefix)
}

func standaloneSessionName(item ui.Item) string {
	return strings.TrimPrefix(item.Path, tmuxSessionPathPrefix)
}

func switchToTmuxSession(sessionName string) error {
	inTmux := os.Getenv("TMUX") != ""
	if inTmux {
		return exec.Command("tmux", "switch-client", "-t", sessionName).Run()
	}
	cmd := exec.Command("tmux", "attach-session", "-t", sessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// monitorAttentionSessions returns sessions needing attention,
// or nil if the daemon is not running
func monitorAttentionSessions() map[string]bool {
	pidPath := monitor.DefaultPIDPath()
	if !monitor.IsDaemonRunning(pidPath) {
		return nil
	}

	statePath := monitor.DefaultStatePath()
	state, err := monitor.Load(statePath)
	if err != nil {
		return nil
	}

	return state.SessionsNeedingAttention()
}

func killTmuxSessionByName(sessionName string) {
	cmd := exec.Command("tmux", "kill-session", "-t", sessionName)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to kill session: %s\n", sessionName)
	} else {
		fmt.Fprintf(os.Stderr, "Killed session: %s\n", sessionName)
	}
}
