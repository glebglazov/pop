package workload

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverNonRecursive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "thoughts/prds/a.md"), "# A\n")
	writeFile(t, filepath.Join(root, "thoughts/prds/nested/b.md"), "# B\n")
	writeFile(t, filepath.Join(root, "thoughts/issues/a/index.json"), `{"issues":[]}`)
	writeFile(t, filepath.Join(root, "thoughts/issues/deep/nested/index.json"), `{"issues":[]}`)

	disc, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(disc.PRDs) != 1 || disc.PRDs["a"] == "" {
		t.Fatalf("PRDs = %#v, want only a", disc.PRDs)
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
	if len(disc.PRDs) != 0 || len(disc.Manifests) != 0 {
		t.Fatalf("expected empty discovery, got %#v %#v", disc.PRDs, disc.Manifests)
	}
}

func TestDiscoverUnreadableDirectory(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod tests unreliable as root")
	}
	root := t.TempDir()
	prdDir := filepath.Join(root, "thoughts/prds")
	if err := os.MkdirAll(prdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(prdDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(prdDir, 0o755) })

	disc, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if disc.PRDDirErr == nil {
		t.Fatal("expected PRDDirErr")
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
