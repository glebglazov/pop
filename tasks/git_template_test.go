package tasks

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Shared git-repo template (ADR-0144).
//
// Every drain-family fixture used to build its runtime checkout with a fresh
// `git init` + two `git config` + `git add` + `git commit` — five real git
// subprocesses (~0.16s) per test, and there are a couple hundred such fixtures.
// The repo those commands produce is identical every time, so it is built once
// and copied per test. The drain still runs real git for the operations under
// exercise (commit, diff, status); only the invariant setup is templated.
var (
	gitTemplateOnce sync.Once
	gitTemplateDir  string
	gitTemplateErr  error
)

// gitTemplatePath returns the path to the shared template repo, building it on
// first use. TestMain removes it at the end of the package run.
func gitTemplatePath() (string, error) {
	gitTemplateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "pop-tasks-git-template")
		if err != nil {
			gitTemplateErr = err
			return
		}
		run := func(args ...string) {
			if gitTemplateErr != nil {
				return
			}
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				gitTemplateErr = fmt.Errorf("git %v: %v: %s", args, err, out)
			}
		}
		write := func(name, content string) {
			if gitTemplateErr != nil {
				return
			}
			gitTemplateErr = os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
		}
		run("init")
		run("config", "user.email", "test@test")
		run("config", "user.name", "test")
		write(".gitignore", "thoughts/\n.agent/\n.xdg/\n")
		write("README.md", "# test\n")
		run("add", "-A")
		run("commit", "-m", "init")
		if gitTemplateErr != nil {
			_ = os.RemoveAll(dir)
			return
		}
		gitTemplateDir = dir
	})
	return gitTemplateDir, gitTemplateErr
}

// copyTemplateTree recursively copies the contents of src into an existing dst
// dir, preserving file modes. It faithfully reproduces the template repo
// (working tree plus .git) into a test's runtime checkout.
func copyTemplateTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if err := os.MkdirAll(dstPath, info.Mode().Perm()); err != nil {
				return err
			}
			if err := copyTemplateTree(srcPath, dstPath); err != nil {
				return err
			}
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.Symlink(target, dstPath); err != nil {
				return err
			}
		default:
			if err := copyFile(srcPath, dstPath, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
