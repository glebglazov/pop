package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

// TestCacheClearRemovesTheDatabaseAndLeavesPopWorking drives `pop cache clear`
// through cobra against a redirected cache directory: absent, present, and
// present again after the next access.
func TestCacheClearRemovesTheDatabaseAndLeavesPopWorking(t *testing.T) {
	// Serial: drives the package-level cacheClearCmd.
	dataHome := t.TempDir()
	cd := newTestCmdDeps(t, "", dataHome, "")
	setCmdLayerDeps(t, cd)
	td := cd.tasksDeps()
	path := tasks.CacheDBPathWith(td)
	if !strings.HasPrefix(path, dataHome) {
		t.Fatalf("cache path under test = %q, want it inside the redirected %q", path, dataHome)
	}
	t.Cleanup(func() { _ = td.CloseCacheDB() })

	if absent := runCacheClearCmd(t); !strings.Contains(absent, "No cache database at "+path) {
		t.Fatalf("clearing an absent cache printed %q", absent)
	}

	if td.CacheDB() == nil {
		t.Fatalf("no cache database opened at %s", path)
	}
	if removed := runCacheClearCmd(t); !strings.Contains(removed, "Removed "+path) {
		t.Fatalf("clearing an open cache printed %q", removed)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat %s after clear = %v, want the file to be gone", path, err)
	}

	if td.CacheDB() == nil {
		t.Fatalf("the next access after a clear built no cache database")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat %s after the next access: %v", path, err)
	}
}

// runCacheClearCmd drives `pop cache clear` through cobra and returns what it
// printed.
func runCacheClearCmd(t *testing.T) string {
	t.Helper()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"cache", "clear"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("pop cache clear: %v", err)
	}
	return out.String()
}
