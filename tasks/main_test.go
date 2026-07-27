package tasks

import (
	"io"
	"os"
	"strings"
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

// TestMain cleans up shared git template state after the package run.
func TestMain(m *testing.M) {
	code := m.Run()
	if gitTemplateDir != "" {
		_ = os.RemoveAll(gitTemplateDir)
	}
	os.Exit(code)
}

// TestGuardTestStorePathPanicsWithoutIsolation confirms the store-open guard
// still trips when a test reaches the real machine-global data dir (ADR-0145).
func TestGuardTestStorePathPanicsWithoutIsolation(t *testing.T) {
	if prodDataDirAtStartup == "" {
		t.Skip("no production data dir detected")
	}
	d := DefaultDeps()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when opening store without test isolation")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "real pop store") {
			t.Fatalf("panic = %#v, want guard message about real pop store", r)
		}
	}()
	_, _, _ = d.Store(true)
}
