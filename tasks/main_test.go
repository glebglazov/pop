package tasks

import (
	"os"
	"testing"
)

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
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
