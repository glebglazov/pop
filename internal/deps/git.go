package deps

import (
	"os/exec"
	"strings"
)

// Git defines operations for interacting with git repositories
type Git interface {
	// Command runs git with the given arguments in the current directory
	Command(args ...string) (string, error)
	// CommandInDir runs git with the given arguments in the specified directory
	CommandInDir(dir string, args ...string) (string, error)
}

// RealGit implements Git using actual git commands
type RealGit struct{}

func NewRealGit() *RealGit {
	return &RealGit{}
}

func (g *RealGit) Command(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *RealGit) CommandInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
