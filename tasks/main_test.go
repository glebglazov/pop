package tasks

import (
	"io"
	"os"
	"testing"
	"time"
)

// testRetryDelayWaitHook skips wall-clock retry sleeps in package tests while
// preserving the retry notice tests assert on. Wired onto Deps.RetryDelayWait
// by newTestDeps and the shared fixtures (ADR-0145).
func testRetryDelayWaitHook(out io.Writer, delay time.Duration) bool {
	if delay > 0 {
		outputFor(out).line(ansiYellow, "↻ Retrying with preserved changes...")
	}
	return false
}

// TestMain points the data dir (and thus the global execution-state store) at a
// throwaway temp dir for the whole package run, so registration tests never read
// or write the developer's real ~/.local/share/pop store. Tests that need their
// own isolated store still override XDG_DATA_HOME via t.Setenv (or, once
// migrated, via newTestDeps); registration is keyed by definition path, and each
// test uses a unique temp root, so the shared default never cross-contaminates.
// The env-set stays as a parallel-safe safety net during the ADR-0145 migration.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pop-tasks-test-xdg")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_DATA_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	if gitTemplateDir != "" {
		_ = os.RemoveAll(gitTemplateDir)
	}
	os.Exit(code)
}
