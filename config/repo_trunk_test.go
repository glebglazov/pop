package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistRepoTrunkWritesRuntimeFile(t *testing.T) {
	t.Parallel()
	d, runtimePath := runtimeTestDeps(t)
	checkout := filepath.Join(t.TempDir(), "repo", "main")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	userCfgPath := filepath.Join(t.TempDir(), "pop", "config.toml")
	if err := os.MkdirAll(filepath.Dir(userCfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	userBody := "# hand-authored\nprojects = [{ path = \"/foo\" }]\n"
	if err := os.WriteFile(userCfgPath, []byte(userBody), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(userCfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := PersistRepoTrunkWith(d, userCfgPath, checkout); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(userCfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("user config.toml changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	data, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "trunk = true") {
		t.Fatalf("missing trunk = true:\n%s", body)
	}
	if !strings.Contains(body, checkout) {
		t.Fatalf("missing checkout key %q:\n%s", checkout, body)
	}
}
