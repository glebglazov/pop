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

// realGit is the one process-wide instance. RealGit is stateless, so sharing it
// costs nothing and buys a fact the per-load memo reads: two dependency sets
// wired with the real git are the same seam, and one memo can serve both.
var realGit = &RealGit{}

func NewRealGit() *RealGit {
	return realGit
}

func (g *RealGit) Command(args ...string) (string, error) {
	cmd := exec.Command(GitBinary(), args...)
	out, err := cmd.Output()
	if err != nil {
		return "", outputError(err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *RealGit) CommandInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command(GitBinary(), append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", outputError(err)
	}
	return strings.TrimSpace(string(out)), nil
}
