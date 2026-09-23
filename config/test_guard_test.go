package config

import (
	"os"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// processEnvFS resolves pop's paths from the test process's own environment, as
// the production edge does, but has no file access: its embedded FileSystem is
// nil, so a load or write that got past the guard fails on that instead of
// reaching the developer's files.
type processEnvFS struct{ deps.FileSystem }

func (processEnvFS) Getenv(key string) string     { return os.Getenv(key) }
func (processEnvFS) UserHomeDir() (string, error) { return os.UserHomeDir() }

func TestATestThatReachesTheRealPopConfigPanics(t *testing.T) {
	if prodConfigDirAtStartup == "" || prodDataDirAtStartup == "" {
		t.Skip("no home directory: the process has no real pop config to guard")
	}
	d := &Deps{FS: processEnvFS{}}
	for name, touch := range map[string]func(){
		"load":  func() { _, _ = LoadDefaultWith(d) },
		"write": func() { _ = SetOverrideValueWith(d, "work.implement.agents", []any{"codex"}) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				r := recover()
				if msg, _ := r.(string); !strings.Contains(msg, "test touched the real pop config file") {
					t.Fatalf("panic = %v, want the guard's panic", r)
				}
			}()
			touch()
		})
	}
}
