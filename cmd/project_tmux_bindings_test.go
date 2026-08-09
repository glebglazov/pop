package cmd

import (
	"bytes"
	"strings"
	"testing"

	tmuxmod "github.com/glebglazov/pop/internal/tmux"
)

func TestProjectTmuxBindingsPrintsFragment(t *testing.T) {
	t.Parallel()
	cmd, _, err := rootCmd.Find([]string{"project", "tmux-bindings"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if cmd.CommandPath() != "pop project tmux-bindings" {
		t.Fatalf("resolved %q, want pop project tmux-bindings", cmd.CommandPath())
	}
	if err := cmd.Args(cmd, []string{"extra"}); err == nil {
		t.Fatal("tmux-bindings must reject positional arguments")
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	t.Cleanup(func() { cmd.SetOut(nil) })
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	got := buf.String()
	want := tmuxmod.BindingFragment()
	if got != want {
		t.Fatalf("stdout must equal BindingFragment()\ngot:\n%s\nwant:\n%s", got, want)
	}
	for _, needle := range []string{
		"bind-key p display-popup",
		"pop project dashboard",
		"bind-key P display-popup",
		"pop worktree dashboard",
	} {
		if !strings.Contains(got, needle) {
			t.Errorf("fragment missing %q", needle)
		}
	}
}
