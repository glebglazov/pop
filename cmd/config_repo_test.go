package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// configRepoFixture is a bare repo with two worktrees plus isolated XDG homes,
// so a set run in one worktree and a get run in the other exercise the same
// runtime file while the hand-authored config.toml stays observable.
type configRepoFixture struct {
	d           *Deps
	main        string
	feature     string
	identity    string
	runtimePath string
	configPath  string
}

func newConfigRepoFixture(t *testing.T, configBody string) configRepoFixture {
	t.Helper()
	root := t.TempDir()
	bareRoot := filepath.Join(root, "kestrel")
	main := filepath.Join(bareRoot, "main")
	feature := filepath.Join(bareRoot, "feature")
	for _, dir := range []string{filepath.Join(bareRoot, ".bare"), main, feature} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dataHome := filepath.Join(root, "xdg-data")
	configHome := filepath.Join(root, "xdg-config")
	configPath := filepath.Join(configHome, "pop", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}
	d := newTestCmdDeps(t, main, dataHome, configHome)
	setCmdLayerDeps(t, d)
	identity, err := filepath.EvalSymlinks(bareRoot)
	if err != nil {
		t.Fatal(err)
	}
	return configRepoFixture{
		d: d, main: main, feature: feature, identity: identity,
		runtimePath: filepath.Join(dataHome, "pop", "config.runtime.toml"),
		configPath:  configPath,
	}
}

func (fx configRepoFixture) set(t *testing.T, checkout, key, value string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runConfigRepoSetWith(fx.d.configDeps(), configRepoConfig(fx.d), &out, checkout, key, value)
	return out.String(), err
}

func (fx configRepoFixture) get(t *testing.T, checkout, key string) string {
	t.Helper()
	var out bytes.Buffer
	if err := runConfigRepoGetWith(fx.d.configDeps(), configRepoConfig(fx.d), &out, checkout, key); err != nil {
		t.Fatalf("config repo get: %v", err)
	}
	return out.String()
}

// TestConfigRepoSetReachesEveryWorktreeAndTheNextAttempt drives the slice end to
// end: a cap set from one worktree is read from another, the value lands in pop's
// runtime state with the hand-authored config.toml byte-identical, and the number
// the next implementation attempt would carry is the one that was set.
func TestConfigRepoSetReachesEveryWorktreeAndTheNextAttempt(t *testing.T) {
	fx := newConfigRepoFixture(t, "[worktree]\nauto_open = true\n")
	before, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}

	out, err := fx.set(t, fx.main, "turn_cap", "40")
	if err != nil {
		t.Fatalf("config repo set: %v", err)
	}
	if !strings.Contains(out, "turn_cap = 40") || !strings.Contains(out, fx.identity) {
		t.Errorf("set said %q, want it to name the value and the repository %s", out, fx.identity)
	}
	if !strings.Contains(out, fx.runtimePath) {
		t.Errorf("set said %q, want it to name the runtime file it wrote", out)
	}

	// The other worktree of the same repository reads the value, and says where
	// it came from.
	got := fx.get(t, fx.feature, "")
	if !strings.Contains(got, "turn_cap") || !strings.Contains(got, "40") {
		t.Errorf("get in the sibling worktree = %q, want turn_cap 40", got)
	}
	if !strings.Contains(got, string(config.RepoSettingRuntime)) {
		t.Errorf("get = %q, want it to name the layer that supplied the value", got)
	}

	after, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("hand-authored config.toml was modified:\n%s", after)
	}
	runtimeBody, err := os.ReadFile(fx.runtimePath)
	if err != nil {
		t.Fatalf("runtime file missing: %v", err)
	}
	if !strings.Contains(string(runtimeBody), "turn_cap = 40") {
		t.Errorf("runtime file does not hold the value:\n%s", runtimeBody)
	}

	// The cap the next implementation attempt in this repository carries is the
	// one just set — resolved the way the drain resolves it, then emitted into
	// claude's argv.
	repoCfg, err := configRepoConfig(fx.d).ResolveRepoConfig(fx.d.configDeps(), fx.feature)
	if err != nil {
		t.Fatal(err)
	}
	if repoCfg.TurnCap != 40 {
		t.Fatalf("resolved TurnCap = %d, want 40", repoCfg.TurnCap)
	}
	invocation, err := tasks.ResolveImplementAgentInvocation("claude", "", "PROMPT", fx.feature, tasks.AgentOutputAuto, repoCfg.TurnCap)
	if err != nil {
		t.Fatalf("resolve implement invocation: %v", err)
	}
	argv := strings.Join(append([]string{invocation.Name}, invocation.Args...), " ")
	if !strings.Contains(argv, "--max-turns 40") {
		t.Errorf("implement argv = %q, want --max-turns 40", argv)
	}
}

// TestConfigRepoSetSaysWhenAHandAuthoredBlockStillWins pins the layering at the
// command surface: pop writes its own layer, and says plainly that the block the
// human wrote is what remains in effect.
func TestConfigRepoSetSaysWhenAHandAuthoredBlockStillWins(t *testing.T) {
	fx := newConfigRepoFixture(t, "")
	body := "[repo.\"" + fx.main + "\"]\nturn_cap = 9\n"
	if err := os.WriteFile(fx.configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := fx.set(t, fx.feature, "turn_cap", "40")
	if err != nil {
		t.Fatalf("config repo set: %v", err)
	}
	if !strings.Contains(out, "still wins") || !strings.Contains(out, "turn_cap = 9") {
		t.Errorf("set said %q, want it to report that the hand-authored 9 still wins", out)
	}

	got := fx.get(t, fx.feature, "turn_cap")
	if !strings.Contains(got, "9") || !strings.Contains(got, string(config.RepoSettingOverride)) {
		t.Errorf("get = %q, want 9 from the hand-authored layer", got)
	}
	if strings.Contains(got, "40") {
		t.Errorf("get = %q, want the pop-written 40 not to be reported as in effect", got)
	}
}

// TestConfigRepoRefusesAnUnrecognisedKey proves the refusal names the keys that
// exist and leaves no runtime state behind, on both verbs.
func TestConfigRepoRefusesAnUnrecognisedKey(t *testing.T) {
	fx := newConfigRepoFixture(t, "")

	_, err := fx.set(t, fx.main, "max_turns", "40")
	if err == nil {
		t.Fatal("an unrecognised key was accepted")
	}
	if !strings.Contains(err.Error(), "max_turns") || !strings.Contains(err.Error(), "turn_cap") {
		t.Errorf("refusal %q should name the key and list the keys that exist", err)
	}
	if _, statErr := os.Stat(fx.runtimePath); !os.IsNotExist(statErr) {
		t.Errorf("a refused set wrote runtime state at %s", fx.runtimePath)
	}

	var out bytes.Buffer
	if err := runConfigRepoGetWith(fx.d.configDeps(), configRepoConfig(fx.d), &out, fx.main, "max_turns"); err == nil {
		t.Fatal("get accepted an unrecognised key")
	} else if !strings.Contains(err.Error(), "turn_cap") {
		t.Errorf("get refusal %q should list the keys that exist", err)
	}
}

// TestConfigRepoGetReportsUnsetKeys keeps the read honest before anything is
// set: every settable key is listed, with no value and no layer.
func TestConfigRepoGetReportsUnsetKeys(t *testing.T) {
	fx := newConfigRepoFixture(t, "")
	got := fx.get(t, fx.main, "")
	for _, key := range config.RepoSettableKeys() {
		if !strings.Contains(got, key) {
			t.Errorf("get = %q, want it to list %q", got, key)
		}
	}
	if !strings.Contains(got, string(config.RepoSettingUnset)) {
		t.Errorf("get = %q, want unset keys reported as unset", got)
	}
}
