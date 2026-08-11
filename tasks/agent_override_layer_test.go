package tasks

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

// TestAgentFlagsBeatTheOverrideLayer pins the top of the agent-list ladder
// (ADR-0202 decision 9): repeated --agent flags are scoped to one invocation and
// typed on purpose, so they stay above the persisted override layer — which in
// turn stays above the hand-authored config.toml.
func TestAgentFlagsBeatTheOverrideLayer(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	userPath := filepath.Join(root, "config.toml")

	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(userPath, "[work.implement]\nagents = [\"config-agent\"]\n")
	write(filepath.Join(dataDir, "pop", "config.override.toml"),
		"[work.implement]\nagents = [\"override-agent\"]\n")

	d := &config.Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataDir
			}
			return ""
		},
		UserHomeDirFunc: func() (string, error) { return filepath.Join(root, "home"), nil },
		ReadFileFunc:    os.ReadFile,
		StatFunc:        os.Stat,
	}}
	cfg, err := config.LoadWith(d, userPath)
	if err != nil {
		t.Fatalf("config.LoadWith() error: %v", err)
	}

	if got := ResolveDefaultAgentPresets(nil, "", false, cfg); !reflect.DeepEqual(got, []string{"override-agent"}) {
		t.Fatalf("without flags = %#v, want the override layer's list", got)
	}
	flags := []string{"claude --model opus", "codex"}
	if got := ResolveDefaultAgentPresets(flags, "", true, cfg); !reflect.DeepEqual(got, flags) {
		t.Fatalf("with repeated --agent flags = %#v, want %#v", got, flags)
	}
}
