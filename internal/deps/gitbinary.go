package deps

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// GitBinaryEnvVar names the operator's escape hatch. Whatever it holds is the
// binary pop forks, verbatim: resolution never second-guesses a deliberate
// choice, so a broken value fails loudly at the first fork rather than being
// quietly replaced.
const GitBinaryEnvVar = "POP_GIT_BINARY"

// appleGitStub is the shim macOS ships at this path. It is not git: it re-execs
// the developer-tools git behind it, and that indirection costs ~20ms of exec
// overhead on every fork — the sole reason this whole resolution exists.
const appleGitStub = "/usr/bin/git"

// gitCandidates are the places a real git lives on a machine whose PATH git is
// the Apple stub, in the order pop trusts them. An operator whose git lives
// anywhere else has GitBinaryEnvVar.
var gitCandidates = []string{
	"/opt/homebrew/bin/git",
	"/usr/local/bin/git",
	"/Applications/Xcode.app/Contents/Developer/usr/bin/git",
}

// gitBinaryMemo holds one resolution for the lifetime of whoever owns it. The
// answer describes the machine, not a moment, so the owner is the process.
type gitBinaryMemo struct {
	resolve func() string
	once    sync.Once
	path    string
}

func (m *gitBinaryMemo) get() string {
	m.once.Do(func() { m.path = m.resolve() })
	return m.path
}

var processGitBinary = &gitBinaryMemo{
	resolve: func() string {
		return resolveGitBinary(os.Getenv, exec.LookPath, isExecutableFile)
	},
}

// GitBinary is the git every fork pop makes goes through. It is resolved on
// first use and every later caller reads that same answer.
func GitBinary() string { return processGitBinary.get() }

// resolveGitBinary picks the binary without forking anything: it looks along
// PATH and stats candidates, where probing with `xcrun -f git` would spend the
// fork it is trying to save.
//
// The environment's own git wins unless it is the Apple stub. That ordering is
// the point: somebody who installed a git deliberately gets that git, and pop
// only goes looking when what PATH names is the shim nobody chose.
func resolveGitBinary(
	getenv func(string) string,
	lookPath func(string) (string, error),
	executable func(string) bool,
) string {
	if override := getenv(GitBinaryEnvVar); override != "" {
		return override
	}

	envGit, err := lookPath("git")
	if err != nil {
		// No git on PATH at all. Naming it anyway keeps the failure where a
		// reader expects it: at the fork, with git's own error.
		return "git"
	}
	if filepath.Clean(envGit) != appleGitStub {
		return envGit
	}

	for _, candidate := range gitCandidates {
		if executable(candidate) {
			return candidate
		}
	}
	return envGit
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}
