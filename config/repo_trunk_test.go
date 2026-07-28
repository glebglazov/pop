package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistRepoTrunkWritesTrunkBlock(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfgPath := filepath.Join(root, "pop", "config.toml")
	checkout := filepath.Join(root, "repo", "main")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := PersistRepoTrunkWith(DefaultDeps(), cfgPath, checkout); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfgPath)
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
