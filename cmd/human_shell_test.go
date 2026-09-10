package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
)

func TestHumanShellResolutionOrder(t *testing.T) {
	t.Setenv("SHELL", "/environment/shell")

	if got := humanShell(&tmuxtest.Fake{Shell: " /tmux/shell\n"}); got != "/tmux/shell" {
		t.Fatalf("tmux shell = %q, want /tmux/shell", got)
	}
	if got := humanShell(&tmuxtest.Fake{ShellErr: errors.New("no server")}); got != "/environment/shell" {
		t.Fatalf("environment fallback = %q, want /environment/shell", got)
	}
	t.Setenv("SHELL", "")
	if got := humanShell(&tmuxtest.Fake{}); got != "/bin/sh" {
		t.Fatalf("final fallback = %q, want /bin/sh", got)
	}
}

func TestHumanAuthoredCommandsResolveConfiguredFunction(t *testing.T) {
	dir := t.TempDir()
	shellPath := filepath.Join(dir, "configured-shell")
	rcPath := filepath.Join(dir, "shellrc")
	resultPath := filepath.Join(dir, "result")

	shellBody := `#!/bin/sh
if [ "$1" = -i ]; then
  . "$POP_TEST_SHELL_RC"
  shift
fi
[ "$1" = -c ] || exit 97
eval "$2"
`
	rcBody := `configured_only() { printf '%s' "$1" > "$POP_TEST_RESULT"; }
`
	if err := os.WriteFile(shellPath, []byte(shellBody), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rcPath, []byte(rcBody), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POP_TEST_SHELL_RC", rcPath)
	t.Setenv("POP_TEST_RESULT", resultPath)
	mod := &tmuxtest.Fake{Shell: shellPath}

	checks := []struct {
		name string
		want string
		run  func()
	}{
		{
			name: "Workbench setup",
			want: "setup",
			run: func() {
				if err := runBeforeApplyCommand(mod, `configured_only setup`, dir); err != nil {
					t.Fatalf("setup command: %v", err)
				}
			},
		},
		{
			name: "project picker",
			want: "/projects/pop:pop",
			run: func() {
				executeProjectCustomCommandWith(mod, `configured_only "$POP_PATH:$POP_NAME"`, &ui.Item{Path: "/projects/pop", Name: "pop"})
			},
		},
		{
			name: "worktree picker",
			want: "/projects/pop/topic:topic:branch:/projects/pop",
			run: func() {
				executeCustomCommandWith(mod, `configured_only "$POP_WORKTREE_PATH:$POP_WORKTREE_NAME:$POP_BRANCH:$POP_REPO_ROOT"`, &ui.Item{Path: "/projects/pop/topic", Context: "branch"}, &project.RepoContext{GitRoot: "/projects/pop"})
			},
		},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			check.run()
			got, err := os.ReadFile(resultPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != check.want {
				t.Fatalf("configured function wrote %q, want %q", got, check.want)
			}
		})
	}
}
