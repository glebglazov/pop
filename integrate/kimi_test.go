package integrate

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const kimiHomeDir = "/h"

func kimiConfigPath(home string) string {
	return filepath.Join(home, ".kimi-code", "config.toml")
}

func kimiSkillLink(home, name string) string {
	return filepath.Join(home, ".kimi-code", "skills", name)
}

// kimiHookEntry mirrors kimi's HookDef schema ({event, matcher?, command,
// timeout?}), so decoding pop's output through it is also an assertion that what
// pop writes is TOML kimi's own config loader accepts.
type kimiHookEntry struct {
	Event   string `toml:"event"`
	Matcher string `toml:"matcher"`
	Command string `toml:"command"`
	Timeout int    `toml:"timeout"`
}

type kimiConfig struct {
	Hooks []kimiHookEntry `toml:"hooks"`
}

func decodeKimiConfig(t *testing.T, data []byte) kimiConfig {
	t.Helper()
	var cfg kimiConfig
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		t.Fatalf("pop wrote config.toml kimi cannot parse: %v\n---\n%s", err, data)
	}
	return cfg
}

func handAuthoredKimiConfig(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "kimi-config.toml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(data)
}

// TestKimiIntegrate_InstallsHooksAndSkillsThenNoOps walks the acceptance path:
// bare integrate merges the status hooks into config.toml, links the baseline
// skills into kimi's skills dir, reports one outcome line per component, and a
// second run changes nothing.
func TestKimiIntegrate_InstallsHooksAndSkillsThenNoOps(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()

	report, err := Install(fakeDeps(kimiHomeDir, fs, io.Discard),
		fullReq("kimi", defaultIntegrationBaseline(), nil, false, false, false))
	if err != nil {
		t.Fatalf("install kimi: %v", err)
	}

	config := fs.files[kimiConfigPath(kimiHomeDir)]
	if len(config) == 0 {
		t.Fatalf("status wiring not installed: %s missing", kimiConfigPath(kimiHomeDir))
	}
	got := decodeKimiConfig(t, config)
	if len(got.Hooks) != len(kimiPopHooks) {
		t.Fatalf("hooks = %d, want %d (%v)", len(got.Hooks), len(kimiPopHooks), got.Hooks)
	}
	for i, want := range kimiPopHooks {
		if got.Hooks[i].Event != want.event || got.Hooks[i].Command != want.command {
			t.Errorf("hooks[%d] = %+v, want event=%q command=%q", i, got.Hooks[i], want.event, want.command)
		}
	}

	for _, name := range []string{"pop-tmux-pane", "pop-grill-with-docs", "pop-to-tasks"} {
		link := kimiSkillLink(kimiHomeDir, name)
		if _, ok := fs.symlinks[link]; !ok {
			t.Errorf("skill %s not linked at %s", name, link)
		}
	}

	if !OutcomesInclude(report.Outcomes, statusWiringOutcomeName, "added") {
		t.Errorf("outcomes = %v, want status-wiring added", report.Outcomes)
	}
	if !OutcomesInclude(report.Outcomes, "pop-tmux-pane", "added") {
		t.Errorf("outcomes = %v, want pop-tmux-pane added", report.Outcomes)
	}

	before := append([]byte(nil), config...)
	second, err := Install(fakeDeps(kimiHomeDir, fs, io.Discard),
		fullReq("kimi", defaultIntegrationBaseline(), nil, false, false, false))
	if err != nil {
		t.Fatalf("re-install kimi: %v", err)
	}
	if after := fs.files[kimiConfigPath(kimiHomeDir)]; string(after) != string(before) {
		t.Errorf("repeat run rewrote config.toml:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
	for _, o := range second.Outcomes {
		if o.Label != "already current" {
			t.Errorf("repeat run outcome %v, want already current", o)
		}
	}
}

// TestKimiStatusWiring_PreservesHandAuthoredConfig pins the TOML merge against a
// fixture carrying comments, unrelated tables, a user's own hook, and a
// multi-line string that contains a table-header-looking line.
func TestKimiStatusWiring_PreservesHandAuthoredConfig(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	fixture := handAuthoredKimiConfig(t)
	path := kimiConfigPath(kimiHomeDir)
	fs.files[path] = []byte(fixture)

	if _, err := Install(fakeDeps(kimiHomeDir, fs, io.Discard), coreReq("kimi")); err != nil {
		t.Fatalf("install kimi: %v", err)
	}

	merged := string(fs.files[path])
	// The user's file is untouched, verbatim, and still first: pop only appends.
	if !strings.HasPrefix(merged, strings.TrimRight(fixture, "\n")) {
		t.Fatalf("hand-authored content was rewritten:\n%s", merged)
	}

	got := decodeKimiConfig(t, []byte(merged))
	if len(got.Hooks) != len(kimiPopHooks)+1 {
		t.Fatalf("hooks = %d, want %d (user's hook plus pop's)", len(got.Hooks), len(kimiPopHooks)+1)
	}
	user := got.Hooks[0]
	if user.Event != "Stop" || user.Command != "notify-send 'kimi finished'" || user.Timeout != 5 {
		t.Errorf("user hook = %+v, want the fixture's Stop hook intact", user)
	}
	if !strings.Contains(merged, tomlPopHookMarker) {
		t.Error("merged config missing pop's ownership marker comment")
	}
}

// TestKimiStatusWiring_PrunesPopBlocksOnUpdateAndRemove: an older pop hook set
// sitting in the middle of a hand-authored file is replaced, not duplicated, and
// removal restores the file to exactly what the user wrote.
func TestKimiStatusWiring_PrunesPopBlocksOnUpdateAndRemove(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	fixture := handAuthoredKimiConfig(t)
	path := kimiConfigPath(kimiHomeDir)

	// A stale pop block (old command spelling) ahead of the user's own tables.
	stale := "[[hooks]]\nevent = \"Stop\"\ncommand = \"~/.local/bin/pop-status unread\"\n\n"
	fs.files[path] = []byte(stale + fixture)

	if _, err := Install(fakeDeps(kimiHomeDir, fs, io.Discard), coreReq("kimi")); err != nil {
		t.Fatalf("install kimi: %v", err)
	}
	merged := string(fs.files[path])
	if strings.Contains(merged, "pop-status unread") {
		t.Errorf("stale pop hook survived the update:\n%s", merged)
	}
	got := decodeKimiConfig(t, []byte(merged))
	if len(got.Hooks) != len(kimiPopHooks)+1 {
		t.Fatalf("hooks = %d, want %d after pruning the stale block", len(got.Hooks), len(kimiPopHooks)+1)
	}

	if _, err := Remove(fakeDeps(kimiHomeDir, fs, io.Discard),
		Request{Agent: "kimi", RemoveComponents: []ComponentID{ComponentStatusWiring}}); err != nil {
		t.Fatalf("remove kimi status wiring: %v", err)
	}
	after := string(fs.files[path])
	if after != strings.TrimRight(fixture, "\n")+"\n" {
		t.Errorf("removal did not restore the hand-authored file:\n--- got ---\n%s\n--- want ---\n%s", after, fixture)
	}
	installed, err := statusWiringInstalled(fakeDeps(kimiHomeDir, fs, io.Discard), kimiHomeDir, "kimi")
	if err != nil {
		t.Fatalf("detect after remove: %v", err)
	}
	if installed {
		t.Error("status wiring still detected after removal")
	}
}

// TestKimiIntegrate_HonorsKimiCodeHome: a relocated kimi home moves both halves
// of the integration — the config.toml merge target and the skills directory.
func TestKimiIntegrate_HonorsKimiCodeHome(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	relocated := "/elsewhere/kimi"
	deps := func() *Deps {
		d := fakeDeps(kimiHomeDir, fs, io.Discard)
		d.getenv = func(key string) string {
			if key == "KIMI_CODE_HOME" {
				return relocated
			}
			return ""
		}
		return d
	}

	if _, err := Install(deps(), fullReq("kimi", defaultIntegrationBaseline(), nil, false, false, false)); err != nil {
		t.Fatalf("install kimi: %v", err)
	}

	if _, ok := fs.files[filepath.Join(relocated, "config.toml")]; !ok {
		t.Errorf("status wiring not installed under KIMI_CODE_HOME: %v", sortedKeys(fs.files))
	}
	if _, ok := fs.files[kimiConfigPath(kimiHomeDir)]; ok {
		t.Error("status wiring installed under the default home despite KIMI_CODE_HOME")
	}
	link := filepath.Join(relocated, "skills", "pop-tmux-pane")
	if _, ok := fs.symlinks[link]; !ok {
		t.Errorf("pane skill not linked under KIMI_CODE_HOME: %v", fs.symlinks)
	}

	// Detection, refresh, and removal all resolve the same relocated root.
	installed, err := statusWiringInstalled(deps(), kimiHomeDir, "kimi")
	if err != nil || !installed {
		t.Fatalf("statusWiringInstalled = (%v, %v), want (true, nil)", installed, err)
	}

	setupIntegrateConfigLayer(t)
	delete(fs.symlinks, link)
	result := updateStaleIntegrations(testConfigDeps(t), deps)
	if len(result.Warnings) != 0 {
		t.Errorf("refresh warnings = %v, want none", result.Warnings)
	}
	if _, ok := fs.symlinks[link]; !ok {
		t.Error("refresh did not re-link the pane skill under KIMI_CODE_HOME")
	}

	if _, err := Remove(deps(), Request{Agent: "kimi"}); err != nil {
		t.Fatalf("remove kimi: %v", err)
	}
	if _, ok := fs.symlinks[link]; ok {
		t.Error("remove left the relocated pane-skill link in place")
	}
	if remaining := string(fs.files[filepath.Join(relocated, "config.toml")]); strings.Contains(remaining, "pop pane set-status") {
		t.Errorf("remove left pop hooks in the relocated config.toml:\n%s", remaining)
	}
}

// TestKimiStatusWiring_ExistingConfigIsNotIntegrated: kimi writes config.toml on
// its own first launch, so file presence must never read as pop integration.
// Refresh has to leave such a machine completely alone.
func TestKimiStatusWiring_ExistingConfigIsNotIntegrated(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)
	fs := newFakeFS()
	fixture := handAuthoredKimiConfig(t)
	path := kimiConfigPath(kimiHomeDir)
	fs.files[path] = []byte(fixture)

	report, err := Install(fakeDeps(kimiHomeDir, fs, io.Discard), dryCoreReq("kimi"))
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if report.Installed {
		t.Error("a hand-authored config.toml with no pop hooks must not read as installed")
	}
	if !report.Changed {
		t.Error("dry-run should report the merge as a change")
	}

	_, real := fakeFactories(kimiHomeDir, fs)
	updateStaleIntegrations(testConfigDeps(t), real)
	if string(fs.files[path]) != fixture {
		t.Errorf("refresh touched a non-integrated kimi config:\n%s", fs.files[path])
	}
	for link := range fs.symlinks {
		if strings.Contains(link, ".kimi-code") {
			t.Errorf("refresh installed %s for a non-integrated kimi", link)
		}
	}
}

// TestKimiRefresh_ReconcilesIntegratedAgent: once kimi's status wiring is
// installed, Integration refresh adds the baseline skills like any other agent.
func TestKimiRefresh_ReconcilesIntegratedAgent(t *testing.T) {
	t.Parallel()
	setupIntegrateConfigLayer(t)
	fs := newFakeFS()
	installViaFake(t, fs, kimiHomeDir, "kimi")

	_, real := fakeFactories(kimiHomeDir, fs)
	result := updateStaleIntegrations(testConfigDeps(t), real)
	if len(result.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", result.Warnings)
	}
	if _, ok := fs.symlinks[kimiSkillLink(kimiHomeDir, "pop-tmux-pane")]; !ok {
		t.Error("refresh must install the missing baseline pane skill for kimi")
	}
	if !OutcomesInclude(result.Outcomes, "pop-tmux-pane", "added") {
		t.Errorf("outcomes = %v, want pop-tmux-pane added for kimi", result.Outcomes)
	}
}

// TestKimiIntegrate_ComponentOptOutRemovesSkills: --no-pane-skill and
// --no-task-skills reconcile kimi the same way they do the JSON-hook agents —
// the opted-out component's pop-owned artifacts go, the status wiring and the
// other component stay.
func TestKimiIntegrate_ComponentOptOutRemovesSkills(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		optOut ComponentID
		keep   ComponentID
		gone   string
		kept   string
	}{
		{"--no-pane-skill", ComponentPaneSkill, ComponentTaskSkills, "pop-tmux-pane", "pop-to-tasks"},
		{"--no-task-skills", ComponentTaskSkills, ComponentPaneSkill, "pop-to-tasks", "pop-tmux-pane"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := newFakeFS()
			if _, err := Install(fakeDeps(kimiHomeDir, fs, io.Discard),
				fullReq("kimi", defaultIntegrationBaseline(), nil, false, false, false)); err != nil {
				t.Fatalf("install kimi: %v", err)
			}

			optOuts := map[ComponentID]bool{tc.optOut: true}
			report, err := Install(fakeDeps(kimiHomeDir, fs, io.Discard),
				fullReq("kimi", []ComponentID{tc.keep}, optOuts, false, false, true))
			if err != nil {
				t.Fatalf("opt-out run: %v", err)
			}
			if _, ok := fs.symlinks[kimiSkillLink(kimiHomeDir, tc.gone)]; ok {
				t.Errorf("opted-out %s still linked for kimi", tc.gone)
			}
			if _, ok := fs.symlinks[kimiSkillLink(kimiHomeDir, tc.kept)]; !ok {
				t.Errorf("opting out of %s removed %s too", tc.optOut, tc.kept)
			}
			if !OutcomesInclude(report.Outcomes, tc.gone, "removed (opted out)") {
				t.Errorf("outcomes = %v, want %s removed (opted out)", report.Outcomes, tc.gone)
			}
			wired, err := tomlHasPopHooks(fakeDeps(kimiHomeDir, fs, io.Discard), kimiConfigPath(kimiHomeDir))
			if err != nil {
				t.Fatalf("detect after opt-out: %v", err)
			}
			if !wired {
				t.Error("opting out of a skill component dropped kimi's status wiring")
			}
		})
	}
}

// TestKimiDoctor_IntentInferredOnly: an installed kimi binary alone is a
// suggestion; pop-owned artifacts are what make kimi an intended agent, and only
// then does Doctor read its wiring state.
func TestKimiDoctor_IntentInferredOnly(t *testing.T) {
	t.Parallel()
	kimiAvailable := func(agent string) bool { return agent == "kimi" }

	fs := newFakeFS()
	d := fakeDeps(kimiHomeDir, fs, io.Discard)
	intent, err := DetectAgentIntent(d, kimiHomeDir, nil, nil, kimiAvailable)
	if err != nil {
		t.Fatalf("DetectAgentIntent: %v", err)
	}
	if len(intent.Intended) != 0 {
		t.Errorf("intended = %v, want none for a bare kimi binary", intent.Intended)
	}
	if len(intent.Suggestions) != 1 || intent.Suggestions[0].Agent != "kimi" {
		t.Fatalf("suggestions = %v, want a single kimi suggestion", intent.Suggestions)
	}
	state, err := ComponentState(d, kimiHomeDir, ComponentStatusWiring, "kimi")
	if err != nil {
		t.Fatalf("ComponentState: %v", err)
	}
	if state.Kind != StateNotInstalled {
		t.Errorf("status-wiring state = %v, want not installed", state.Kind)
	}

	installViaFake(t, fs, kimiHomeDir, "kimi")
	intent, err = DetectAgentIntent(fakeDeps(kimiHomeDir, fs, io.Discard), kimiHomeDir, nil, nil, kimiAvailable)
	if err != nil {
		t.Fatalf("DetectAgentIntent after install: %v", err)
	}
	if len(intent.Intended) != 1 || intent.Intended[0].Agent != "kimi" {
		t.Fatalf("intended = %v, want kimi", intent.Intended)
	}
	if !stringSliceContains(intent.Intended[0].Sources, "pop-owned integration artifacts") {
		t.Errorf("sources = %v, want pop-owned integration artifacts", intent.Intended[0].Sources)
	}
	if len(intent.Suggestions) != 0 {
		t.Errorf("suggestions = %v, want none once kimi is intended", intent.Suggestions)
	}
	state, err = ComponentState(fakeDeps(kimiHomeDir, fs, io.Discard), kimiHomeDir, ComponentStatusWiring, "kimi")
	if err != nil {
		t.Fatalf("ComponentState after install: %v", err)
	}
	if state.Kind != StateInstalledCurrent {
		t.Errorf("status-wiring state = %v, want installed-current", state.Kind)
	}
}

// TestStripPopHookBlocks covers the TOML splice itself: pop ownership is decided
// by the command, spacing variants of the array-of-tables header count, and
// bracketed lines inside a multi-line string are not table headers.
func TestStripPopHookBlocks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		in        string
		wantKept  string
		wantFound bool
	}{
		{
			name:      "no hooks at all",
			in:        "default_model = \"x\"\n",
			wantKept:  "default_model = \"x\"",
			wantFound: false,
		},
		{
			name:      "pop block with spaced header",
			in:        "a = 1\n\n[[ hooks ]]\nevent = \"Stop\"\ncommand = \"pop pane set-status unread\"\n",
			wantKept:  "a = 1",
			wantFound: true,
		},
		{
			name:      "literal-string command",
			in:        "[[hooks]]\nevent = \"Stop\"\ncommand = 'pop pane set-status unread'\n",
			wantKept:  "",
			wantFound: true,
		},
		{
			name:      "user hook kept, pop hook cut",
			in:        "[[hooks]]\nevent = \"Stop\"\ncommand = \"make lint\"\n\n[[hooks]]\nevent = \"Stop\"\ncommand = \"pop pane set-status unread\"\n",
			wantKept:  "[[hooks]]\nevent = \"Stop\"\ncommand = \"make lint\"",
			wantFound: true,
		},
		{
			name:      "table header inside a multi-line string is not a header",
			in:        "note = \"\"\"\n[[hooks]]\ncommand = \"pop pane set-status unread\"\n\"\"\"\n",
			wantKept:  "note = \"\"\"\n[[hooks]]\ncommand = \"pop pane set-status unread\"\n\"\"\"",
			wantFound: false,
		},
		{
			name:      "multi-line value in a user hook does not swallow the pop block after it",
			in:        "[[hooks]]\nevent = \"Stop\"\ncommand = \"\"\"\nmake lint\n\"\"\"\n\n[[hooks]]\nevent = \"Stop\"\ncommand = \"pop pane set-status unread\"\n",
			wantKept:  "[[hooks]]\nevent = \"Stop\"\ncommand = \"\"\"\nmake lint\n\"\"\"",
			wantFound: true,
		},
		{
			name:      "marker comment removed with its blocks",
			in:        "a = 1\n\n" + renderTOMLHookBlocks(kimiPopHooks),
			wantKept:  "a = 1",
			wantFound: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kept, found := stripPopHookBlocks(tc.in)
			if kept != tc.wantKept {
				t.Errorf("kept = %q, want %q", kept, tc.wantKept)
			}
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
		})
	}
}
