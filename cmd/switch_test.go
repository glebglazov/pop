package cmd

import (
	"os"
	"testing"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

func mockSwitchDeps() (*SwitchDeps, *history.History, *tmuxtest.Fake) {
	hist := &history.History{}
	fake := &tmuxtest.Fake{Inside: true}
	d := &SwitchDeps{
		FS: &deps.MockFileSystem{
			StatFunc: func(path string) (os.FileInfo, error) {
				return deps.MockFileInfo{NameVal: "dir", IsDirVal: true}, nil
			},
		},
		Tmux:        fake,
		SessionName: func(path string) string { return "session-name" },
		LoadHistory: func() (*history.History, error) { return hist, nil },
		SaveHistory: func(h *history.History) error { return nil },
	}
	return d, hist, fake
}

func TestRunProjectSwitch(t *testing.T) {
	t.Run("records history and creates+switches session", func(t *testing.T) {
		d, hist, fake := mockSwitchDeps()

		if err := RunProjectSwitch(d, "/repo/feature"); err != nil {
			t.Fatal(err)
		}

		if len(hist.Entries) != 1 || hist.Entries[0].Path != "/repo/feature" {
			t.Errorf("history entries = %+v, want single /repo/feature", hist.Entries)
		}
		if fake.Live["session-name"] != "/repo/feature" {
			t.Errorf("created sessions = %v, want session-name -> /repo/feature", fake.Live)
		}
		if len(fake.Switched) != 1 || fake.Switched[0] != "session-name" {
			t.Errorf("switched = %v, want [session-name]", fake.Switched)
		}
		if len(fake.Attached) != 0 {
			t.Errorf("attached = %v, want none", fake.Attached)
		}
	})

	t.Run("skips session creation when it exists", func(t *testing.T) {
		d, _, fake := mockSwitchDeps()
		fake.Live = map[string]string{"session-name": "/old"}

		if err := RunProjectSwitch(d, "/repo/feature"); err != nil {
			t.Fatal(err)
		}

		if fake.Live["session-name"] != "/old" {
			t.Errorf("session dir = %q, want /old (not recreated)", fake.Live["session-name"])
		}
		if len(fake.Switched) != 1 || fake.Switched[0] != "session-name" {
			t.Errorf("switched = %v, want [session-name]", fake.Switched)
		}
	})

	t.Run("attaches when outside tmux", func(t *testing.T) {
		d, _, fake := mockSwitchDeps()
		fake.Inside = false
		fake.Live = map[string]string{"session-name": "/old"}

		if err := RunProjectSwitch(d, "/repo/feature"); err != nil {
			t.Fatal(err)
		}

		if len(fake.Attached) != 1 || fake.Attached[0] != "session-name" {
			t.Errorf("attached = %v, want [session-name]", fake.Attached)
		}
		if len(fake.Switched) != 0 {
			t.Errorf("switched = %v, want none", fake.Switched)
		}
	})

	t.Run("missing directory errors without touching history", func(t *testing.T) {
		d, hist, fake := mockSwitchDeps()
		d.FS.(*deps.MockFileSystem).StatFunc = func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		}

		if err := RunProjectSwitch(d, "/gone"); err == nil {
			t.Fatal("expected error for missing directory")
		}
		if len(hist.Entries) != 0 {
			t.Errorf("history entries = %+v, want none", hist.Entries)
		}
		if len(fake.Live) != 0 || len(fake.Switched) != 0 || len(fake.Attached) != 0 {
			t.Errorf("tmux touched: live=%v switched=%v attached=%v, want none", fake.Live, fake.Switched, fake.Attached)
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
