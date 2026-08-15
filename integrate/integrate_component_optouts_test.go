package integrate

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/config"
)

func writeIntegrateRuntimeFile(t *testing.T, body string) {
	t.Helper()
	path := integrateRuntimePath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readIntegrateStatedSkills reads the skills list the decline stated in the
// override layer — the destination every writer of intent shares (ADR-0212
// decision 5).
func readIntegrateStatedSkills(t *testing.T) []string {
	t.Helper()
	path := integrateOverridePath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		t.Fatal(err)
	}
	integrations, _ := doc["integrations"].(map[string]any)
	if integrations == nil {
		return nil
	}
	raw, _ := integrations["skills"].([]any)
	skills := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			skills = append(skills, s)
		}
	}
	return skills
}

func TestIntegrateOptOut_NoPaneSkill_StatesTheListAndRemovesArtifacts(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)
	fs := newFakeFS()
	home := "/h"

	seedFileComponent(t, fs, home, ComponentPaneSkill, "claude")
	link := claudePaneLink(home)

	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := ApplyComponentOptOuts(testConfigDeps(t), false, optOuts); err != nil {
		t.Fatalf("ApplyComponentOptOuts: %v", err)
	}

	skills := readIntegrateStatedSkills(t)
	want := []string{"tasks"}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("stated skills = %#v, want %#v", skills, want)
	}

	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)
	baseline, err := BaselineLoader(testConfigDeps(t))
	if err != nil {
		t.Fatalf("BaselineLoader: %v", err)
	}
	if _, err := installReq(d, fullReq("claude", baseline, optOuts, false, false, false)); err != nil {
		t.Fatalf("RunComponents: %v", err)
	}
	if _, ok := fs.symlinks[link]; ok {
		t.Fatalf("pane-skill symlink still present at %s", link)
	}
	if !strings.Contains(out.String(), "removed (opted out)") {
		t.Fatalf("expected removed outcome, got %q", out.String())
	}
}

func TestIntegrateOptOut_NoTaskSkills_StatesTheListAndRemovesArtifacts(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)
	fs := newFakeFS()
	home := "/h"

	seedFileComponent(t, fs, home, ComponentTaskSkills, "claude")
	linksBefore := len(fs.symlinks)

	optOuts := map[ComponentID]bool{ComponentTaskSkills: true}
	if err := ApplyComponentOptOuts(testConfigDeps(t), false, optOuts); err != nil {
		t.Fatalf("ApplyComponentOptOuts: %v", err)
	}

	skills := readIntegrateStatedSkills(t)
	want := []string{"pane"}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("stated skills = %#v, want %#v", skills, want)
	}

	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)
	baseline, err := BaselineLoader(testConfigDeps(t))
	if err != nil {
		t.Fatalf("BaselineLoader: %v", err)
	}
	if _, err := installReq(d, fullReq("claude", baseline, optOuts, false, false, false)); err != nil {
		t.Fatalf("RunComponents: %v", err)
	}
	if len(fs.symlinks) >= linksBefore {
		t.Fatalf("expected task-skills symlinks removed, count still %d", len(fs.symlinks))
	}
	if !strings.Contains(out.String(), "removed (opted out)") {
		t.Fatalf("expected removed outcome, got %q", out.String())
	}
}

// TestIntegrateOptOut_BareIntegrateTakesTheDeclineBack pins the other half of
// the flag: re-asserting the full baseline removes what a decline stated, so the
// layers below answer for the skills list again.
func TestIntegrateOptOut_BareIntegrateTakesTheDeclineBack(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)

	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := ApplyComponentOptOuts(testConfigDeps(t), false, optOuts); err != nil {
		t.Fatalf("ApplyComponentOptOuts: %v", err)
	}
	if skills := readIntegrateStatedSkills(t); !reflect.DeepEqual(skills, []string{"tasks"}) {
		t.Fatalf("stated skills = %#v, want the pane skill declined", skills)
	}

	if err := ApplyComponentOptOuts(testConfigDeps(t), true, nil); err != nil {
		t.Fatalf("ApplyComponentOptOuts bare: %v", err)
	}
	if _, err := os.Stat(integrateOverridePath(t)); !os.IsNotExist(err) {
		data, _ := os.ReadFile(integrateOverridePath(t))
		t.Fatalf("expected the stated list removed after bare integrate, got %q", string(data))
	}
	baseline, err := BaselineLoader(testConfigDeps(t))
	if err != nil {
		t.Fatalf("BaselineLoader: %v", err)
	}
	if len(baseline) != len(config.DefaultIntegrationSkills) {
		t.Fatalf("baseline = %#v, want the full merged baseline back", baseline)
	}
}

// TestIntegrateOptOut_UserConfigWinsAfterBareClear checks what a cleared
// decline hands back to: the user's own hand-authored list, not the embedded
// defaults.
func TestIntegrateOptOut_UserConfigWinsAfterBareClear(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	configDir := filepath.Join(root, "config")
	userPath := filepath.Join(configDir, "pop", "config.toml")

	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
projects = [{ path = "/main" }]

[integrations]
skills = ["tasks"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cd := &config.Deps{FS: testFS(dataDir, configDir)}
	if err := ApplyComponentOptOuts(cd, false, map[ComponentID]bool{ComponentTaskSkills: true}); err != nil {
		t.Fatalf("ApplyComponentOptOuts: %v", err)
	}
	if err := ApplyComponentOptOuts(cd, true, nil); err != nil {
		t.Fatalf("ApplyComponentOptOuts bare: %v", err)
	}

	cfg, err := config.LoadWith(cd, userPath)
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills: %v", err)
	}
	want := []string{"tasks"}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("IntegrationsSkills() = %#v, want %#v", skills, want)
	}
}

// TestIntegrateOptOut_BeatsAHandAuthoredList is the behaviour the retargeting
// buys: a decline recorded below config.toml lost to a list that named the
// component, so declining it changed nothing. Stated, it wins.
func TestIntegrateOptOut_BeatsAHandAuthoredList(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	configDir := filepath.Join(root, "config")
	userPath := filepath.Join(configDir, "pop", "config.toml")

	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
projects = [{ path = "/main" }]

[integrations]
skills = ["tasks", "pane"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cd := &config.Deps{FS: testFS(dataDir, configDir)}
	if err := ApplyComponentOptOuts(cd, false, map[ComponentID]bool{ComponentPaneSkill: true}); err != nil {
		t.Fatalf("ApplyComponentOptOuts: %v", err)
	}

	cfg, err := config.LoadWith(cd, userPath)
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills: %v", err)
	}
	if !reflect.DeepEqual(skills, []string{"tasks"}) {
		t.Fatalf("IntegrationsSkills() = %#v, want the declined pane skill gone", skills)
	}
}

// TestIntegrateOptOut_VariadicNoFlagsOncePerInvocation pins that both flags in
// one invocation state one list, and that stating the same list twice is stable.
func TestIntegrateOptOut_VariadicNoFlagsOncePerInvocation(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)

	optOuts := map[ComponentID]bool{
		ComponentPaneSkill:  true,
		ComponentTaskSkills: true,
	}
	if err := ApplyComponentOptOuts(testConfigDeps(t), false, optOuts); err != nil {
		t.Fatalf("first ApplyComponentOptOuts: %v", err)
	}
	data, err := os.ReadFile(integrateOverridePath(t))
	if err != nil {
		t.Fatalf("override file missing: %v", err)
	}
	if !strings.Contains(string(data), "skills = []") {
		t.Fatalf("expected empty skills array when both opted out, got %q", string(data))
	}

	// Second call states the same list again rather than subtracting twice.
	if err := ApplyComponentOptOuts(testConfigDeps(t), false, optOuts); err != nil {
		t.Fatalf("second ApplyComponentOptOuts: %v", err)
	}
	if skills := readIntegrateStatedSkills(t); len(skills) != 0 {
		t.Fatalf("stated skills = %#v, want it unchanged by the second call", skills)
	}
}

// TestIntegrateOptOut_NoPaneSkillOverAnExistingRecord runs the decline on a
// machine that still holds the retired runtime record: the list it states is the
// merged one minus the declined alias, and the record itself is left alone.
func TestIntegrateOptOut_NoPaneSkillOverAnExistingRecord(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)
	writeIntegrateRuntimeFile(t, `
[integrations]
skills = ["tasks", "pane"]

[future]
enabled = true
`)

	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := ApplyComponentOptOuts(testConfigDeps(t), false, optOuts); err != nil {
		t.Fatalf("ApplyComponentOptOuts: %v", err)
	}

	if skills := readIntegrateStatedSkills(t); !reflect.DeepEqual(skills, []string{"tasks"}) {
		t.Fatalf("stated skills = %#v, want the pane skill declined", skills)
	}
	data, err := os.ReadFile(integrateRuntimePath(t))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `skills = ["tasks", "pane"]`) || !strings.Contains(body, "[future]") {
		t.Fatalf("the existing record was written to: %q", body)
	}
}
