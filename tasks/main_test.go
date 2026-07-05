package tasks

import (
	"io"
	"os"
	"testing"
	"time"
)

// testRetryDelayWaitHook skips wall-clock retry sleeps in the package test run
// while preserving the retry notice tests assert on.
func testRetryDelayWaitHook(out io.Writer, delay time.Duration, _ retryWaiter) bool {
	if delay > 0 {
		outputFor(out).line(ansiYellow, "↻ Retrying with preserved changes...")
	}
	return false
}

// TestMain points the data dir (and thus the global execution-state store) at a
// throwaway temp dir for the whole package run, so registration tests never read
// or write the developer's real ~/.local/share/pop store. Tests that need their
// own isolated store still override XDG_DATA_HOME via t.Setenv; registration is
// keyed by definition path, and each test uses a unique temp root, so the shared
// default never cross-contaminates.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pop-tasks-test-xdg")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_DATA_HOME", dir)
	retryDelayWaitHook = testRetryDelayWaitHook
	code := m.Run()
	retryDelayWaitHook = nil
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
