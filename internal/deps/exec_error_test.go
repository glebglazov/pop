package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealGitSurfacesStderr(t *testing.T) {
	withFakeCommand(t, "git", "git says nope")

	git := NewRealGit()
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "command", run: func() error { _, err := git.Command("status"); return err }},
		{name: "command in dir", run: func() error { _, err := git.CommandInDir(t.TempDir(), "status"); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "git says nope") {
				t.Fatalf("expected stderr in error, got %q", err.Error())
			}
		})
	}
}

func TestCommandErrorLeavesEmptyStderrUnchanged(t *testing.T) {
	withFakeCommand(t, "git", "")

	_, err := NewRealGit().Command("status")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), ": ") {
		t.Fatalf("expected bare error without stderr suffix, got %q", err.Error())
	}
}

func withFakeCommand(t *testing.T, name, stderr string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n"
	if stderr != "" {
		script += "printf '%s\\n' " + shellQuote(stderr) + " >&2\n"
	}
	script += "exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
