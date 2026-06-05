package workload

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverNonRecursive(t *testing.T) {
	root := t.TempDir()
	// A stray non-manifest file beside the Issue sets is irrelevant to discovery.
	writeFile(t, filepath.Join(root, "notes.md"), "# A\n")
	writeFile(t, filepath.Join(root, "a/index.json"), `{"tasks":[]}`)
	writeFile(t, filepath.Join(root, "deep/nested/index.json"), `{"tasks":[]}`)

	disc, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(disc.Manifests) != 1 || disc.Manifests["a"] == "" {
		t.Fatalf("Manifests = %#v, want only a", disc.Manifests)
	}
}

func TestDiscoverAbsentDirectories(t *testing.T) {
	root := t.TempDir()
	disc, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(disc.Manifests) != 0 {
		t.Fatalf("expected empty discovery, got %#v", disc.Manifests)
	}
}

func TestDiscoverUnreadableDirectory(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod tests unreliable as root")
	}
	root := t.TempDir()
	issueDir := filepath.Join(root, "tasks")
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(issueDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(issueDir, 0o755) })

	disc, err := Discover(issueDir)
	if err != nil {
		t.Fatal(err)
	}
	if disc.IssueDirErr == nil {
		t.Fatal("expected IssueDirErr")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
