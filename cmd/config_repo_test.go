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
// override layer while the hand-authored config.toml stays observable.
type configRepoFixture struct {
	d            *Deps
	main         string
	feature      string
	identity     string
	runtimePath  string
	overridePath string
	configPath   string
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
		runtimePath:  filepath.Join(dataHome, "pop", "config.runtime.toml"),
		overridePath: filepath.Join(dataHome, "pop", "config.override.toml"),
		configPath:   configPath,
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

// TestConfigRepoSetReachesEveryWorktreeAndTheNextAttempt drives the command end
// to end: a cap set from one worktree is read from another, the value lands in
// the override layer with the hand-authored config.toml byte-identical, and the
// number the next implementation attempt would carry is the one that was set.
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
	if !strings.Contains(out, fx.overridePath) {
		t.Errorf("set said %q, want it to name the override file it wrote", out)
	}

	// The other worktree of the same repository reads the value, and says where
	// it came from.
	got := fx.get(t, fx.feature, "")
	if !strings.Contains(got, "turn_cap") || !strings.Contains(got, "40") {
		t.Errorf("get in the sibling worktree = %q, want turn_cap 40", got)
	}
	if !strings.Contains(got, string(config.RepoSettingOverrideLayer)) {
		t.Errorf("get = %q, want it to name the layer that supplied the value", got)
	}

	after, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("hand-authored config.toml was modified:\n%s", after)
	}
	overrideBody, err := os.ReadFile(fx.overridePath)
	if err != nil {
		t.Fatalf("override file missing: %v", err)
	}
	if !strings.Contains(string(overrideBody), "turn_cap = 40") {
		t.Errorf("override layer does not hold the value:\n%s", overrideBody)
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

// TestConfigRepoSetOverridesAHandAuthoredBlock pins the layering at the command
// surface: the value states intent, so it wins over the block the human wrote,
// and the reply names what it now stands over.
func TestConfigRepoSetOverridesAHandAuthoredBlock(t *testing.T) {
	fx := newConfigRepoFixture(t, "")
	body := "[repo.\"" + fx.main + "\"]\nturn_cap = 9\n"
	if err := os.WriteFile(fx.configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := fx.set(t, fx.feature, "turn_cap", "40")
	if err != nil {
		t.Fatalf("config repo set: %v", err)
	}
	if !strings.Contains(out, "overrides") || !strings.Contains(out, "turn_cap = 9") {
		t.Errorf("set said %q, want it to name the hand-authored 9 it stands over", out)
	}

	got := fx.get(t, fx.feature, "turn_cap")
	if !strings.Contains(got, string(config.RepoSettingOverrideLayer)) {
		t.Errorf("get = %q, want the override layer named as the source", got)
	}
	// The VALUE cell is the second tab-separated field of the turn_cap row —
	// do not search the whole buffer for "40", because the temp path or a reach
	// line can contain those digits without the stated value being in effect.
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "  turn_cap") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Fatalf("turn_cap row %q has no value cell", line)
		}
		if fields[1] != "40" {
			t.Errorf("turn_cap value = %q, want the stated 40 over the hand-authored 9", fields[1])
		}
	}
}

// TestConfigRepoRefusesAnUnrecognisedKey proves the refusal names the keys that
// exist and writes nothing, on both verbs.
func TestConfigRepoRefusesAnUnrecognisedKey(t *testing.T) {
	fx := newConfigRepoFixture(t, "")

	_, err := fx.set(t, fx.main, "max_turns", "40")
	if err == nil {
		t.Fatal("an unrecognised key was accepted")
	}
	if !strings.Contains(err.Error(), "max_turns") || !strings.Contains(err.Error(), "turn_cap") {
		t.Errorf("refusal %q should name the key and list the keys that exist", err)
	}
	if _, statErr := os.Stat(fx.overridePath); !os.IsNotExist(statErr) {
		t.Errorf("a refused set wrote the override layer at %s", fx.overridePath)
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

// TestConfigRepoGetShowsTurnCapReach pins ADR-0198 at the command surface: the
// effective value and source are joined by what the key actually reaches —
// every registered actor line appears beside the set value.
func TestConfigRepoGetShowsTurnCapReach(t *testing.T) {
	fx := newConfigRepoFixture(t, "")
	if _, err := fx.set(t, fx.main, "turn_cap", "40"); err != nil {
		t.Fatalf("config repo set: %v", err)
	}
	got := fx.get(t, fx.feature, "turn_cap")
	if !strings.Contains(got, "turn_cap") || !strings.Contains(got, "40") {
		t.Errorf("get = %q, want turn_cap 40", got)
	}
	reach, ok := config.ConfigKeyReachFor("turn_cap")
	if !ok {
		t.Fatal("turn_cap reach is not registered")
	}
	for _, line := range reach.Lines {
		if !strings.Contains(got, line.Actor) || !strings.Contains(got, line.Detail) {
			t.Errorf("get = %q, want reach line %s / %s", got, line.Actor, line.Detail)
		}
	}
	if !strings.Contains(got, "--max-turns N") {
		t.Errorf("get = %q, want claude's argv shape with the bound as N", got)
	}
}

// TestConfigRepoGetWithoutReachKeepsPriorRowShape proves a key that declares no
// reach still prints only KEY / VALUE / SOURCE — the pre-reach row, unchanged.
func TestConfigRepoGetWithoutReachKeepsPriorRowShape(t *testing.T) {
	prior, had := config.ConfigKeyReachFor("turn_cap")
	config.ClearConfigKeyReach("turn_cap")
	t.Cleanup(func() {
		if had {
			config.RegisterConfigKeyReach("turn_cap", prior)
		}
	})

	fx := newConfigRepoFixture(t, "")
	got := fx.get(t, fx.main, "turn_cap")
	if strings.Contains(got, "--max-turns") || strings.Contains(got, "claude") {
		t.Errorf("get = %q, want no reach lines when turn_cap declares none", got)
	}
	if !strings.Contains(got, "turn_cap") || !strings.Contains(got, string(config.RepoSettingUnset)) {
		t.Errorf("get = %q, want the unset KEY/VALUE/SOURCE row", got)
	}
	// Exactly one data row under the header: the key itself, not actor lines.
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	dataRows := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "  KEY") {
			dataRows++
		}
	}
	if dataRows != 1 {
		t.Errorf("get has %d data rows, want 1 (no reach continuations):\n%s", dataRows, got)
	}
}
