package deps

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealTmuxSurfacesStderr(t *testing.T) {
	withFakeCommand(t, "tmux", "tmux says nope")

	tmux := NewRealTmux()
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "command", run: func() error { _, err := tmux.Command("display-message"); return err }},
		{name: "new session", run: func() error { return tmux.NewSession("missing", t.TempDir()) }},
		{name: "switch client", run: func() error { return tmux.SwitchClient("missing") }},
		{name: "attach session", run: func() error { return tmux.AttachSession("missing") }},
		{name: "kill session", run: func() error { return tmux.KillSession("missing") }},
		{name: "list sessions", run: func() error { _, err := tmux.ListSessions(); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "tmux says nope") {
				t.Fatalf("expected stderr in error, got %q", err.Error())
			}
		})
	}
}

func TestRealTmuxAttachSessionForwardsStderr(t *testing.T) {
	withFakeCommand(t, "tmux", "tmux attach says nope")

	oldStderr := os.Stderr
	readStderr, writeStderr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writeStderr
	t.Cleanup(func() {
		os.Stderr = oldStderr
		readStderr.Close()
		writeStderr.Close()
	})

	err = NewRealTmux().AttachSession("missing")
	writeStderr.Close()
	forwarded, readErr := io.ReadAll(readStderr)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tmux attach says nope") {
		t.Fatalf("expected stderr in error, got %q", err.Error())
	}
	if !strings.Contains(string(forwarded), "tmux attach says nope") {
		t.Fatalf("expected stderr to be forwarded, got %q", string(forwarded))
	}
}

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
