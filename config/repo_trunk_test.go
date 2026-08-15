package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Naming a trunk never touches the hand-authored config.toml (ADR-0150's
// locational rule, which ADR-0212 keeps): it states intent in the layer pop
// writes, as the path form the whole model now speaks.
func TestPersistRepoTrunkLeavesUserConfigAlone(t *testing.T) {
	t.Parallel()
	d, _ := runtimeTestDeps(t)
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
	if err := PersistRepoTrunkWith(d, checkout); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(userCfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != userBody {
		t.Fatalf("user config.toml changed:\nbefore:\n%s\nafter:\n%s", userBody, after)
	}

	data, err := os.ReadFile(DefaultOverrideConfigPathWith(d))
	if err != nil {
		t.Fatalf("read override layer: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "trunk = ") || !strings.Contains(body, canonicalPath(d, checkout)) {
		t.Fatalf("override layer does not state the trunk path:\n%s", body)
	}
	if strings.Contains(body, "trunk = true") {
		t.Fatalf("the retired boolean spelling was written:\n%s", body)
	}
}
