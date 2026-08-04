package cmd

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/spf13/cobra"
)

// TestAuthoringGuidesAreReadOnly runs both guide verbs against a filesystem that
// fails the test on any write, and with no store, no config and no repository
// behind them. The guides are text the binary carries, so they must answer in a
// virgin checkout and leave nothing behind.
func TestAuthoringGuidesAreReadOnly(t *testing.T) {
	t.Parallel()
	setCmdLayerDeps(t, refusingWriteDeps(t))

	for _, tc := range []struct {
		path []string
		want []string
	}{
		{
			path: []string{"map", "authoring-guide"},
			want: []string{"# Authoring a Map by hand", "## Storage layout", "## map.md", "## index.json"},
		},
		{
			path: []string{"tasks", "authoring-guide"},
			want: []string{"# Authoring a Task set by hand", "## Storage layout", "## Task markdown", "## index.json"},
		},
	} {
		cmd, _, err := rootCmd.Find(tc.path)
		if err != nil {
			t.Fatalf("Find(%v): %v", tc.path, err)
		}
		if cmd.CommandPath() != "pop "+strings.Join(tc.path, " ") {
			t.Fatalf("resolved %q, want %q", cmd.CommandPath(), "pop "+strings.Join(tc.path, " "))
		}
		if err := cmd.Args(cmd, []string{"extra"}); err == nil {
			t.Fatalf("%s accepts a positional argument; the guide takes none", cmd.CommandPath())
		}

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		t.Cleanup(func() { cmd.SetOut(nil) })
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("%s: %v", cmd.CommandPath(), err)
		}
		for _, want := range tc.want {
			if !strings.Contains(buf.String(), want) {
				t.Fatalf("%s output missing %q:\n%s", cmd.CommandPath(), want, buf.String())
			}
		}
	}
}

// TestRegisterHelpCarriesMechanicsNotDoctrine holds the split the guides exist
// for: -h is a human's flag reference, hit constantly, so authoring doctrine
// stays in the guide and the help points at it (ADR-0183).
func TestRegisterHelpCarriesMechanicsNotDoctrine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cmd   *cobra.Command
		guide string
	}{
		{cmd: taskRegisterCmd, guide: "pop tasks authoring-guide"},
		{cmd: mapRegisterCmd, guide: "pop map authoring-guide"},
	} {
		help := tc.cmd.Long
		if !strings.Contains(help, tc.guide) {
			t.Fatalf("%s help does not point at %q", tc.cmd.CommandPath(), tc.guide)
		}
		if !strings.Contains(help, "MALFORMED") {
			t.Fatalf("%s help does not describe the MALFORMED fix loop", tc.cmd.CommandPath())
		}
		// Naming a status condition is mechanics; teaching how to type, size or
		// slice the work is doctrine, and none of it belongs in a flag reference.
		for _, doctrine := range []string{
			"Prefer AFK", "Split the slice", "effort", "vertical slice",
			"Orientation", "Acceptance criteria",
		} {
			if strings.Contains(help, doctrine) {
				t.Fatalf("%s help carries authoring doctrine (%q); it belongs in the guide",
					tc.cmd.CommandPath(), doctrine)
			}
		}
	}
}

// refusingWriteDeps builds cmd deps whose filesystem answers reads and fails the
// test on every mutation, so "read-only" is asserted rather than assumed.
func refusingWriteDeps(t *testing.T) *Deps {
	t.Helper()
	real := deps.NewRealFileSystem()
	refuse := func(op string) {
		t.Helper()
		t.Fatalf("the authoring guide performed a %s", op)
	}
	fsx := &deps.MockFileSystem{
		GetenvFunc:      func(string) string { return filepath.Join(t.TempDir(), "absent") },
		GetwdFunc:       real.Getwd,
		UserHomeDirFunc: func() (string, error) { return filepath.Join(t.TempDir(), "absent"), nil },
		StatFunc:        real.Stat,
		ReadDirFunc:     real.ReadDir,
		ReadFileFunc:    real.ReadFile,
		WriteFileFunc: func(string, []byte, fs.FileMode) error {
			refuse("write")
			return nil
		},
		MkdirAllFunc: func(string, fs.FileMode) error {
			refuse("mkdir")
			return nil
		},
		RenameFunc: func(string, string) error {
			refuse("rename")
			return nil
		},
		RemoveAllFunc: func(string) error {
			refuse("remove")
			return nil
		},
		DirFSFunc:        real.DirFS,
		EvalSymlinksFunc: real.EvalSymlinks,
	}
	td := &tasks.Deps{FS: fsx}
	return &Deps{
		Dir:       t.TempDir(),
		FS:        fsx,
		Tasks:     td,
		Config:    &config.Deps{FS: fsx},
		Wayfinder: &wayfinder.Deps{FS: fsx, Tasks: td},
	}
}
