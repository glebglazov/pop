package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/workload"
)

func TestWorkloadStatusExitSuccessWithMalformedRows(t *testing.T) {
	root := t.TempDir()
	prdPath := filepath.Join(root, "thoughts/prds/bad.md")
	if err := os.MkdirAll(filepath.Dir(prdPath), 0o755); err != nil {
		t.Fatal(err)
	}
	issueDir := filepath.Join(root, "thoughts/issues/bad")
	if err := os.MkdirAll(issueDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "index.json"), []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prdPath, []byte("# Bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	d := workload.DefaultDeps()
	var buf bytes.Buffer
	if err := runWorkloadStatusWith(d, &buf); err != nil {
		t.Fatalf("status should succeed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected output")
	}
}

func TestWorkloadStatusUnreadableDiscoveryFails(t *testing.T) {
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

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	err := runWorkloadStatusWith(workload.DefaultDeps(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected setup failure")
	}
}
