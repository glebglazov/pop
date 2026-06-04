package cmd

import (
	"os"
	"testing"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
)

func mockSwitchDeps() (*SwitchDeps, *history.History, *[]string) {
	hist := &history.History{}
	var tmuxCalls []string
	d := &SwitchDeps{
		FS: &deps.MockFileSystem{
			StatFunc: func(path string) (os.FileInfo, error) {
				return deps.MockFileInfo{NameVal: "dir", IsDirVal: true}, nil
			},
		},
		Tmux: &deps.MockTmux{
			HasSessionFunc: func(name string) bool { return false },
			NewSessionFunc: func(name, dir string) error {
				tmuxCalls = append(tmuxCalls, "new:"+name+":"+dir)
				return nil
			},
			SwitchClientFunc: func(name string) error {
				tmuxCalls = append(tmuxCalls, "switch:"+name)
				return nil
			},
			AttachSessionFunc: func(name string) error {
				tmuxCalls = append(tmuxCalls, "attach:"+name)
				return nil
			},
		},
		SessionName: func(path string) string { return "session-name" },
		LoadHistory: func() (*history.History, error) { return hist, nil },
		SaveHistory: func(h *history.History) error { return nil },
		InTmux:      func() bool { return true },
	}
	return d, hist, &tmuxCalls
}

func TestRunProjectSwitch(t *testing.T) {
	t.Run("records history and creates+switches session", func(t *testing.T) {
		d, hist, tmuxCalls := mockSwitchDeps()

		if err := RunProjectSwitch(d, "/repo/feature"); err != nil {
			t.Fatal(err)
		}

		if len(hist.Entries) != 1 || hist.Entries[0].Path != "/repo/feature" {
			t.Errorf("history entries = %+v, want single /repo/feature", hist.Entries)
		}
		want := []string{"new:session-name:/repo/feature", "switch:session-name"}
		if len(*tmuxCalls) != 2 || (*tmuxCalls)[0] != want[0] || (*tmuxCalls)[1] != want[1] {
			t.Errorf("tmux calls = %v, want %v", *tmuxCalls, want)
		}
	})

	t.Run("skips session creation when it exists", func(t *testing.T) {
		d, _, tmuxCalls := mockSwitchDeps()
		d.Tmux.(*deps.MockTmux).HasSessionFunc = func(name string) bool { return true }

		if err := RunProjectSwitch(d, "/repo/feature"); err != nil {
			t.Fatal(err)
		}

		if len(*tmuxCalls) != 1 || (*tmuxCalls)[0] != "switch:session-name" {
			t.Errorf("tmux calls = %v, want [switch:session-name]", *tmuxCalls)
		}
	})

	t.Run("attaches when outside tmux", func(t *testing.T) {
		d, _, tmuxCalls := mockSwitchDeps()
		d.InTmux = func() bool { return false }
		d.Tmux.(*deps.MockTmux).HasSessionFunc = func(name string) bool { return true }

		if err := RunProjectSwitch(d, "/repo/feature"); err != nil {
			t.Fatal(err)
		}

		if len(*tmuxCalls) != 1 || (*tmuxCalls)[0] != "attach:session-name" {
			t.Errorf("tmux calls = %v, want [attach:session-name]", *tmuxCalls)
		}
	})

	t.Run("missing directory errors without touching history", func(t *testing.T) {
		d, hist, tmuxCalls := mockSwitchDeps()
		d.FS.(*deps.MockFileSystem).StatFunc = func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		}

		if err := RunProjectSwitch(d, "/gone"); err == nil {
			t.Fatal("expected error for missing directory")
		}
		if len(hist.Entries) != 0 {
			t.Errorf("history entries = %+v, want none", hist.Entries)
		}
		if len(*tmuxCalls) != 0 {
			t.Errorf("tmux calls = %v, want none", *tmuxCalls)
		}
	})

	t.Run("file path errors", func(t *testing.T) {
		d, _, _ := mockSwitchDeps()
		d.FS.(*deps.MockFileSystem).StatFunc = func(path string) (os.FileInfo, error) {
			return deps.MockFileInfo{NameVal: "file", IsDirVal: false}, nil
		}

		if err := RunProjectSwitch(d, "/repo/file.txt"); err == nil {
			t.Fatal("expected error for non-directory path")
		}
	})

	t.Run("nil history from failed load is tolerated", func(t *testing.T) {
		d, _, _ := mockSwitchDeps()
		d.LoadHistory = func() (*history.History, error) { return nil, os.ErrPermission }
		var saved *history.History
		d.SaveHistory = func(h *history.History) error { saved = h; return nil }

		if err := RunProjectSwitch(d, "/repo/feature"); err != nil {
			t.Fatal(err)
		}
		if saved == nil || len(saved.Entries) != 1 {
			t.Errorf("saved history = %+v, want single entry", saved)
		}
	})
}

func TestCanonicalDir(t *testing.T) {
	t.Run("relative path joins cwd", func(t *testing.T) {
		fs := &deps.MockFileSystem{
			GetwdFunc: func() (string, error) { return "/home/user", nil },
			StatFunc: func(path string) (os.FileInfo, error) {
				return deps.MockFileInfo{NameVal: "dir", IsDirVal: true}, nil
			},
		}

		got, err := canonicalDir(fs, "projects/app")
		if err != nil {
			t.Fatal(err)
		}
		if got != "/home/user/projects/app" {
			t.Errorf("canonicalDir = %q, want /home/user/projects/app", got)
		}
	})

	t.Run("symlinks resolve", func(t *testing.T) {
		fs := &deps.MockFileSystem{
			EvalSymlinksFunc: func(path string) (string, error) {
				return "/real/app", nil
			},
			StatFunc: func(path string) (os.FileInfo, error) {
				return deps.MockFileInfo{NameVal: "dir", IsDirVal: true}, nil
			},
		}

		got, err := canonicalDir(fs, "/link/app")
		if err != nil {
			t.Fatal(err)
		}
		if got != "/real/app" {
			t.Errorf("canonicalDir = %q, want /real/app", got)
		}
	})

	t.Run("failed symlink resolution falls back to original", func(t *testing.T) {
		fs := &deps.MockFileSystem{
			EvalSymlinksFunc: func(path string) (string, error) {
				return "", os.ErrNotExist
			},
			StatFunc: func(path string) (os.FileInfo, error) {
				return deps.MockFileInfo{NameVal: "dir", IsDirVal: true}, nil
			},
		}

		got, err := canonicalDir(fs, "/repo/app")
		if err != nil {
			t.Fatal(err)
		}
		if got != "/repo/app" {
			t.Errorf("canonicalDir = %q, want /repo/app", got)
		}
	})
}

func TestProjectSwitchCommandTree(t *testing.T) {
	got, _, err := rootCmd.Find([]string{"project", "switch"})
	if err != nil {
		t.Fatal(err)
	}
	if got != projectSwitchCmd {
		t.Fatalf("Find([project switch]) = %q, want project switch command", got.CommandPath())
	}
}
