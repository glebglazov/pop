package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExecRunnerSurfacesStderr proves the moved-in error mapping: a non-zero
// tmux exit surfaces the binary's stderr through outputError.
func TestExecRunnerSurfacesStderr(t *testing.T) {
	withFakeTmux(t, "tmux says nope")

	_, err := execRunner{}.output("list-sessions")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tmux says nope") {
		t.Fatalf("expected stderr in error, got %q", err.Error())
	}
}

func TestExecRunnerLeavesEmptyStderrUnchanged(t *testing.T) {
	withFakeTmux(t, "")

	_, err := execRunner{}.output("list-sessions")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), ": ") {
		t.Fatalf("expected bare error without stderr suffix, got %q", err.Error())
	}
}

// withFakeTmux puts a failing tmux stub earlier on PATH so exec.Command
// finds it. The stub writes stderr (when given) and exits non-zero.
func withFakeTmux(t *testing.T, stderr string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\n"
	if stderr != "" {
		script += "printf '%s\\n' '" + strings.ReplaceAll(stderr, "'", "'\\''") + "' >&2\n"
	}
	script += "exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
