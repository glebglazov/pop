package cmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/integrate"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/release"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// readOnlyDoctorDeps wires a doctorDeps over a fakeFS with injectable core
// checks. Every write operation fails the test because doctor is strictly
// read-only.
func readOnlyDoctorDeps(t *testing.T, fs *fakeFS, tmux, cfgOK, daemon bool) *doctorDeps {
	t.Helper()
	base := fakeDeps(installerHome, fs, nil)
	integrate.GuardReadOnly(t, base)
	return &doctorDeps{
		integrate:     base,
		tmuxAvailable: func() bool { return tmux },
		loadProjectConfig: func() (*config.Config, error) {
			if cfgOK {
				return &config.Config{}, nil
			}
			return nil, errors.New("/cfg/config.toml: not found")
		},
		projectConfigureAvailable: func() bool { return true },
		expandProjectConfig: func(*config.Config) ([]config.ExpandedPath, error) {
			return []config.ExpandedPath{{Path: "/repo/app", Explicit: true}}, nil
		},
		expandProjects: func([]config.ExpandedPath) ([]project.ExpandedProject, []string) {
			return []project.ExpandedProject{{Name: "app", Path: "/repo/app", SessionName: "app"}}, nil
		},
		projectSessionActivity: func() map[string]int64 { return nil },
		detectRepoContext: func() (*project.RepoContext, error) {
			return &project.RepoContext{GitRoot: "/repo/app", RepoName: "app"}, nil
		},
		listWorktrees: func(*project.RepoContext) ([]project.Worktree, error) {
			return []project.Worktree{{Name: "feature", Path: "/repo/app-feature"}}, nil
		},
		daemonRunning:          func() bool { return daemon },
		monitorDaemonStartable: func() bool { return true },
		loadMonitorState: func() (*monitor.State, error) {
			return &monitor.State{Panes: map[string]*monitor.PaneEntry{}}, nil
		},
		paneSessionAddressable: func() (string, error) {
			return "current tmux session \"app\" is addressable", nil
		},
		agentIntent: func() (*doctorAgentIntentReport, error) {
			return &doctorAgentIntentReport{}, nil
		},
		explicitAgentContext:     func() []string { return nil },
		agentExecutableAvailable: func(string) bool { return false },
		taskStorageWritable: func() (string, error) {
			return "/data/pop/repos", nil
		},
		scanMaps:            func() (int, error) { return 0, nil },
		legacyTaskSets:      func() ([]string, error) { return nil, nil },
		orphanedTaskStorage: func() ([]tasks.OrphanedStorage, error) { return nil, nil },
		legacyLayoutStorage: func() ([]string, error) { return nil, nil },
		updateCheck:         func() release.Result { return release.Result{Current: "dev", State: release.StateDev} },
		agentCatalog:        func() []tasks.AgentCatalogRow { return nil },
		probeAgentAuthentication: func(string) tasks.AgentAuthenticationProbe {
			return tasks.AgentAuthenticationProbe{}
		},
	}
}

func setDoctorIntent(d *doctorDeps, agents ...string) {
	d.agentIntent = func() (*doctorAgentIntentReport, error) {
		report := &doctorAgentIntentReport{}
		for _, agent := range agents {
			report.Intended = append(report.Intended, doctorAgentIntent{Agent: agent, Sources: []string{"test intent"}})
		}
		return report, nil
	}
}

func claudeStatusWired(fs *fakeFS) {
	settings := filepath.Join(installerHome, ".claude", "settings.json")
	fs.files[settings] = []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"pop pane set-status unread 2>/dev/null || true"}]}]}}`)
}

func familyByCommand(report *doctorReport, command string) (doctorFamilyReport, bool) {
	for _, family := range report.families {
		if family.command == command {
			return family, true
		}
	}
	return doctorFamilyReport{}, false
}

func checkByLabel(family doctorFamilyReport, label string) (doctorCheck, bool) {
	for _, check := range family.checks {
		if check.label == label {
			return check, true
		}
	}
	return doctorCheck{}, false
}

func initDoctorGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runDoctorGit(t, root, "init")
	return root
}

func runDoctorGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func taskCheck(t *testing.T, report *doctorReport, label string) doctorCheck {
	t.Helper()
	family, ok := familyByCommand(report, "pop tasks")
	if !ok {
		t.Fatalf("missing pop tasks family")
	}
	check, ok := checkByLabel(family, label)
	if !ok {
		t.Fatalf("missing %q check", label)
	}
	return check
}

func mapFamilyCheck(t *testing.T, report *doctorReport, label string) doctorCheck {
	t.Helper()
	family, ok := familyByCommand(report, "pop map")
	if !ok {
		t.Fatalf("missing pop map family")
	}
	check, ok := checkByLabel(family, label)
	if !ok {
		t.Fatalf("missing %q check", label)
	}
	return check
}

func doctorIntentByAgent(report *doctorAgentIntentReport, agent string) (doctorAgentIntent, bool) {
	for _, intent := range report.Intended {
		if intent.Agent == agent {
			return intent, true
		}
	}
	return doctorAgentIntent{}, false
}

func doctorSuggestionByAgent(report *doctorAgentIntentReport, agent string) (doctorAgentSuggestion, bool) {
	for _, suggestion := range report.Suggestions {
		if suggestion.Agent == agent {
			return suggestion, true
		}
	}
	return doctorAgentSuggestion{}, false
}

func TestDoctorReportsCanonicalCommandFamilies(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, true)
	setDoctorIntent(d, "claude")
	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}

	if len(report.families) != len(canonicalDoctorCommands) {
		t.Fatalf("family count = %d, want %d", len(report.families), len(canonicalDoctorCommands))
	}
	for i, want := range canonicalDoctorCommands {
		if got := report.families[i].command; got != want {
			t.Fatalf("family[%d] = %q, want %q", i, got, want)
		}
	}
	for _, family := range report.families {
		if family.command == "pop work" {
			t.Fatalf("pop work should not be a top-level doctor family; it rides tasks/queue readiness")
		}
	}
}

func TestDoctorNestedChecksAreCommandFamilyScopedAndActionable(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.loadProjectConfig = func() (*config.Config, error) { return nil, os.ErrNotExist }
	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}

	project, ok := familyByCommand(report, "pop project")
	if !ok {
		t.Fatalf("missing pop project family")
	}
	check, ok := checkByLabel(project, "project config")
	if !ok {
		t.Fatalf("missing project config check")
	}
	if check.status != doctorStatusPartial {
		t.Fatalf("check status = %s, want %s", check.status, doctorStatusPartial)
	}
	if check.detail == "" {
		t.Fatalf("non-OK check must carry detail")
	}
	if check.nextAction != "pop configure" {
		t.Fatalf("nextAction = %q, want pop configure", check.nextAction)
	}
}

func TestDoctorDoesNotRenderPaneSkillConflictAsPrimaryIntegrateRow(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	conflictPath := filepath.Join(installerHome, ".claude", "skills", "tmux-pane")
	fs.files[conflictPath] = []byte("my own skill")

	d := readOnlyDoctorDeps(t, fs, true, true, true)
	setDoctorIntent(d, "claude")
	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}

	integrate, ok := familyByCommand(report, "pop integrate")
	if !ok {
		t.Fatalf("missing pop integrate family")
	}
	if _, ok := checkByLabel(integrate, "claude pane-skill"); ok {
		t.Fatalf("pane-skill conflict should not be rendered as a primary integrate row: %+v", integrate.checks)
	}
	check, ok := checkByLabel(integrate, "intended agent setup repair path")
	if !ok {
		t.Fatalf("missing intended agent setup repair path")
	}
	if check.status != doctorStatusOK || !strings.Contains(check.detail, "claude") {
		t.Fatalf("repair path check = %+v, want OK detail naming intended agent", check)
	}
	if string(fs.files[conflictPath]) != "my own skill" {
		t.Fatalf("doctor modified the user's own file")
	}
}

func TestDoctorDoesNotRenderStalePaneSkillAsPrimaryIntegrateRow(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	setup := fakeDeps(installerHome, fs, nil)
	if err := integrate.InstallFileComponent(setup, installerHome, integrate.ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("pre-install: %v", err)
	}
	renderFile, _, _ := paneSkillPaths()
	fs.files[renderFile] = []byte("drifted content not matching the embedded source")

	d := readOnlyDoctorDeps(t, fs, true, true, true)
	setDoctorIntent(d, "claude")
	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}

	integrate, ok := familyByCommand(report, "pop integrate")
	if !ok {
		t.Fatalf("missing pop integrate family")
	}
	if _, ok := checkByLabel(integrate, "claude pane-skill"); ok {
		t.Fatalf("stale pane-skill should not be rendered as a primary integrate row: %+v", integrate.checks)
	}
	if integrate.status != doctorStatusOK {
		t.Fatalf("stale optional pane-skill should not drive integrate family readiness: %+v", integrate)
	}
}

func TestDoctorDerivesIntendedAgentsFromTaskConfiguration(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	intent, err := integrate.DetectAgentIntent(d, installerHome, func(string) (*config.Config, error) {
		return &config.Config{Task: &config.TasksConfig{Presets: map[string]config.TaskAgentConfig{
			"cursor": {Output: "text"},
		}}}, nil
	}, nil, func(string) bool { return false })
	if err != nil {
		t.Fatalf("DetectAgentIntent: %v", err)
	}
	got, ok := doctorIntentByAgent(intent, "cursor")
	if !ok {
		t.Fatalf("configured task agent was not intended: %+v", intent)
	}
	if !stringSliceContains(got.Sources, "task config") {
		t.Fatalf("intent sources = %v, want task config", got.Sources)
	}
}

func TestDoctorDerivesIntendedAgentsFromInstalledPopArtifactsAndHooks(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	claudeStatusWired(fs)
	if err := integrate.InstallFileComponent(fakeDeps(installerHome, fs, nil), installerHome, integrate.ComponentPaneSkill, "pi"); err != nil {
		t.Fatalf("install pi pane skill: %v", err)
	}

	intent, err := integrate.DetectAgentIntent(fakeDeps(installerHome, fs, nil), installerHome, func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	}, nil, func(string) bool { return false })
	if err != nil {
		t.Fatalf("DetectAgentIntent: %v", err)
	}
	for _, agent := range []string{"claude", "pi"} {
		got, ok := doctorIntentByAgent(intent, agent)
		if !ok {
			t.Fatalf("%s was not intended from Pop artifacts/hooks: %+v", agent, intent)
		}
		if !stringSliceContains(got.Sources, "pop-owned integration artifacts") {
			t.Fatalf("%s sources = %v, want pop-owned integration artifacts", agent, got.Sources)
		}
	}
}

func TestDoctorDerivesIntendedAgentsFromExplicitCommandContext(t *testing.T) {
	t.Parallel()
	intent, err := integrate.DetectAgentIntent(fakeDeps(installerHome, newFakeFS(), nil), installerHome, func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	}, []string{"opencode"}, func(string) bool { return false })
	if err != nil {
		t.Fatalf("DetectAgentIntent: %v", err)
	}
	got, ok := doctorIntentByAgent(intent, "opencode")
	if !ok {
		t.Fatalf("explicit command context agent was not intended: %+v", intent)
	}
	if !stringSliceContains(got.Sources, "explicit command context") {
		t.Fatalf("intent sources = %v, want explicit command context", got.Sources)
	}
}

func TestDoctorComponentStateStatusWiringInstalled(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	claudeStatusWired(fs)
	d := fakeDeps(installerHome, fs, nil)

	state, err := integrate.ComponentState(d, installerHome, integrate.ComponentStatusWiring, "claude")
	if err != nil {
		t.Fatalf("ComponentState: %v", err)
	}
	if state.Kind != integrate.StateInstalledCurrent {
		t.Fatalf("state = %v, want installed-current", state)
	}
}

func TestDoctorComponentStateStatusWiringMissing(t *testing.T) {
	t.Parallel()
	d := fakeDeps(installerHome, newFakeFS(), nil)

	state, err := integrate.ComponentState(d, installerHome, integrate.ComponentStatusWiring, "claude")
	if err != nil {
		t.Fatalf("ComponentState: %v", err)
	}
	if state.Kind != integrate.StateNotInstalled {
		t.Fatalf("state = %v, want not-installed", state)
	}
}

func TestDoctorComponentStateFileComponentInstalled(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	if err := integrate.InstallFileComponent(d, installerHome, integrate.ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("install: %v", err)
	}

	state, err := integrate.ComponentState(d, installerHome, integrate.ComponentPaneSkill, "claude")
	if err != nil {
		t.Fatalf("ComponentState: %v", err)
	}
	if state.Kind != integrate.StateInstalledCurrent {
		t.Fatalf("state = %v, want installed-current", state)
	}
}

func TestDoctorComponentStateFileComponentStale(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	if err := integrate.InstallFileComponent(d, installerHome, integrate.ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("install: %v", err)
	}
	renderFile, _, _ := paneSkillPaths()
	fs.files[renderFile] = []byte("drifted content not matching the embedded source")

	state, err := integrate.ComponentState(d, installerHome, integrate.ComponentPaneSkill, "claude")
	if err != nil {
		t.Fatalf("ComponentState: %v", err)
	}
	if state.Kind != integrate.StateStale {
		t.Fatalf("state = %v, want stale", state)
	}
}

func TestDoctorComponentStateFileComponentConflict(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	conflictPath := filepath.Join(installerHome, ".claude", "skills", "pop-tmux-pane")
	fs.files[conflictPath] = []byte("user-owned skill")
	d := fakeDeps(installerHome, fs, nil)

	state, err := integrate.ComponentState(d, installerHome, integrate.ComponentPaneSkill, "claude")
	if err != nil {
		t.Fatalf("ComponentState: %v", err)
	}
	if state.Kind != integrate.StateConflict {
		t.Fatalf("state = %v, want conflict", state)
	}
	if state.ConflictPath != conflictPath {
		t.Fatalf("ConflictPath = %q, want %q", state.ConflictPath, conflictPath)
	}
}

func TestDoctorIntendedAgentComponentStates(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	claudeStatusWired(fs)
	d := fakeDeps(installerHome, fs, nil)
	intent := &integrate.AgentIntentReport{
		Intended: []integrate.AgentIntent{
			{Agent: "claude"},
			{Agent: "codex"},
		},
	}

	states, err := integrate.IntendedAgentComponentStates(d, installerHome, intent, integrate.ComponentStatusWiring)
	if err != nil {
		t.Fatalf("IntendedAgentComponentStates: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("states len = %d, want 2", len(states))
	}
	if states[0].Agent != "claude" || states[0].State.Kind != integrate.StateInstalledCurrent {
		t.Fatalf("claude state = %+v, want installed-current", states[0])
	}
	if states[1].Agent != "codex" || states[1].State.Kind != integrate.StateNotInstalled {
		t.Fatalf("codex state = %+v, want not-installed", states[1])
	}
}

func TestDoctorExportedAgentAndComponentListings(t *testing.T) {
	t.Parallel()
	if len(integrate.Agents) == 0 {
		t.Fatal("Agents listing is empty")
	}
	components := integrate.Components()
	if len(components) == 0 {
		t.Fatal("Components listing is empty")
	}
	for _, id := range components {
		if _, ok := integrate.LookupComponent(id); !ok {
			t.Fatalf("Components() returned unknown id %q", id)
		}
	}
}

func TestDoctorPathOnlyAgentsAreSuggestionsAndDoNotAffectReadiness(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	detectDeps := fakeDeps(installerHome, fs, nil)
	intent, err := integrate.DetectAgentIntent(detectDeps, installerHome, func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	}, nil, func(agent string) bool { return agent == "codex" })
	if err != nil {
		t.Fatalf("DetectAgentIntent: %v", err)
	}
	if _, ok := doctorIntentByAgent(intent, "codex"); ok {
		t.Fatalf("PATH-only codex should not be intended: %+v", intent)
	}
	if _, ok := doctorSuggestionByAgent(intent, "codex"); !ok {
		t.Fatalf("PATH-only codex should be a suggestion: %+v", intent)
	}

	d := readOnlyDoctorDeps(t, fs, true, true, true)
	d.agentIntent = func() (*doctorAgentIntentReport, error) { return intent, nil }
	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	integrate, ok := familyByCommand(report, "pop integrate")
	if !ok {
		t.Fatalf("missing pop integrate family")
	}
	if integrate.status != doctorStatusOK {
		t.Fatalf("PATH-only suggestion changed integrate status to %s: %+v", integrate.status, integrate)
	}
	suggestion, ok := checkByLabel(integrate, "codex available agent suggestion")
	if !ok {
		t.Fatalf("missing PATH-only suggestion check")
	}
	if suggestion.status != doctorStatusNA || suggestion.nextAction != "pop integrate codex" {
		t.Fatalf("suggestion = %+v, want N/A with integrate command", suggestion)
	}
}

func TestDoctorPathOnlyConflictIsNotReportedWithoutIntent(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	conflictPath := filepath.Join(installerHome, ".claude", "skills", "tmux-pane")
	fs.files[conflictPath] = []byte("user-owned skill")
	intent, err := integrate.DetectAgentIntent(fakeDeps(installerHome, fs, nil), installerHome, func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	}, nil, func(agent string) bool { return agent == "claude" })
	if err != nil {
		t.Fatalf("DetectAgentIntent: %v", err)
	}
	d := readOnlyDoctorDeps(t, fs, true, true, true)
	d.agentIntent = func() (*doctorAgentIntentReport, error) { return intent, nil }
	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	integrate, _ := familyByCommand(report, "pop integrate")
	if _, ok := checkByLabel(integrate, "claude pane-skill"); ok {
		t.Fatalf("PATH-only conflict should not produce agent-specific readiness check: %+v", integrate.checks)
	}
	if integrate.status != doctorStatusOK {
		t.Fatalf("PATH-only conflict changed integrate status to %s: %+v", integrate.status, integrate)
	}
}

func TestDoctorUsesAgentComponentStateOnlyAsSupportingEvidence(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	claudeStatusWired(fs)
	d := readOnlyDoctorDeps(t, fs, true, true, true)
	setDoctorIntent(d, "claude", "codex")

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}

	monitorFamily, ok := familyByCommand(report, "pop monitor")
	if !ok {
		t.Fatalf("missing pop monitor family")
	}
	wiring, ok := checkByLabel(monitorFamily, "intended agent status wiring")
	if !ok {
		t.Fatalf("missing intended agent status wiring check")
	}
	if wiring.status != doctorStatusPartial || !strings.Contains(wiring.detail, "wired: claude") || !strings.Contains(wiring.detail, "codex (missing)") {
		t.Fatalf("wiring check = %+v, want partial supporting evidence for intended agents", wiring)
	}

	integrate, ok := familyByCommand(report, "pop integrate")
	if !ok {
		t.Fatalf("missing pop integrate family")
	}
	for _, oldLabel := range []string{"claude status-wiring", "claude pane-skill", "codex task-skills"} {
		if _, ok := checkByLabel(integrate, oldLabel); ok {
			t.Fatalf("old component-grid row %q should not be rendered in integrate family: %+v", oldLabel, integrate.checks)
		}
	}
}

func TestDoctorHealthyCoreFamiliesRenderOK(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	claudeStatusWired(fs)
	d := readOnlyDoctorDeps(t, fs, true, true, true)
	out := &bytes.Buffer{}
	if err := runDoctorWith(d, out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	s := out.String()

	for _, line := range []string{
		"OK        pop project    ready",
		"OK        pop monitor    ready",
		"OK        pop pane       ready",
		"OK        pop map        ready",
	} {
		if !strings.Contains(s, line) {
			t.Fatalf("output missing %q:\n%s", line, s)
		}
	}
	if strings.Contains(s, "Blocked   tmux available") {
		t.Fatalf("healthy tmux check should not be blocked:\n%s", s)
	}
}

func TestDoctorProjectReadinessReportsSelectableSourcesOK(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		paths    []config.ExpandedPath
		expanded []project.ExpandedProject
		sessions map[string]int64
	}{
		{
			name:  "configured project",
			paths: []config.ExpandedPath{{Path: "/repo/app", Explicit: true}},
			expanded: []project.ExpandedProject{{
				Name:        "app",
				Path:        "/repo/app",
				ProjectName: "app",
				SessionName: "app",
			}},
		},
		{
			name:  "configured worktree",
			paths: []config.ExpandedPath{{Path: "/repo/app", Explicit: true}},
			expanded: []project.ExpandedProject{{
				Name:        "app/feature",
				Path:        "/repo/app-feature",
				ProjectName: "app",
				IsWorktree:  true,
				SessionName: "app/feature",
			}},
		},
		{
			name:     "standalone tmux session",
			sessions: map[string]int64{"scratch": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
			d.expandProjectConfig = func(*config.Config) ([]config.ExpandedPath, error) { return tt.paths, nil }
			d.expandProjects = func([]config.ExpandedPath) ([]project.ExpandedProject, []string) { return tt.expanded, nil }
			d.projectSessionActivity = func() map[string]int64 { return tt.sessions }

			report, err := buildDoctorReport(d)
			if err != nil {
				t.Fatalf("buildDoctorReport: %v", err)
			}
			family, ok := familyByCommand(report, "pop project")
			if !ok {
				t.Fatalf("missing pop project family")
			}
			if family.status != doctorStatusOK {
				t.Fatalf("project status = %s, want %s (%s)", family.status, doctorStatusOK, family.reason)
			}
		})
	}
}

func TestDoctorProjectMissingConfigUsesFirstRunConfigurePath(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.loadProjectConfig = func() (*config.Config, error) { return nil, os.ErrNotExist }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop project")
	if !ok {
		t.Fatalf("missing pop project family")
	}
	if family.status == doctorStatusBlocked {
		t.Fatalf("missing config should not block when configure is available: %+v", family)
	}
	check, ok := checkByLabel(family, "project config")
	if !ok {
		t.Fatalf("missing project config check")
	}
	if check.status != doctorStatusPartial || check.nextAction != "pop configure" {
		t.Fatalf("config check = %+v, want Partial with pop configure next action", check)
	}
}

func TestDoctorProjectInvalidConfigBlocks(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.loadProjectConfig = func() (*config.Config, error) { return nil, errors.New("invalid TOML") }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop project")
	if !ok {
		t.Fatalf("missing pop project family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("project status = %s, want %s", family.status, doctorStatusBlocked)
	}
	if !strings.Contains(family.reason, "invalid TOML") {
		t.Fatalf("blocked reason should name invalid config: %q", family.reason)
	}
}

func TestDoctorProjectNoConfiguredProjectsBlocks(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.expandProjectConfig = func(*config.Config) ([]config.ExpandedPath, error) { return nil, nil }
	d.expandProjects = func([]config.ExpandedPath) ([]project.ExpandedProject, []string) { return nil, nil }
	d.projectSessionActivity = func() map[string]int64 { return nil }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop project")
	if !ok {
		t.Fatalf("missing pop project family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("project status = %s, want %s", family.status, doctorStatusBlocked)
	}
	if !strings.Contains(family.reason, "no configured projects") {
		t.Fatalf("blocked reason should explain no selectable paths: %q", family.reason)
	}
}

func TestDoctorProjectAllConfiguredExpansionFailuresBlock(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.expandProjectConfig = func(*config.Config) ([]config.ExpandedPath, error) {
		return []config.ExpandedPath{{Path: "/repo/app", Explicit: true}}, nil
	}
	d.expandProjects = func([]config.ExpandedPath) ([]project.ExpandedProject, []string) {
		return nil, []string{"app"}
	}
	d.projectSessionActivity = func() map[string]int64 { return nil }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop project")
	if !ok {
		t.Fatalf("missing pop project family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("project status = %s, want %s", family.status, doctorStatusBlocked)
	}
	if !strings.Contains(family.reason, "failed to discover any selectable") {
		t.Fatalf("blocked reason should explain expansion failure: %q", family.reason)
	}
}

func TestDoctorMapReadinessOKWithZeroMaps(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.scanMaps = func() (int, error) { return 0, nil }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop map")
	if !ok {
		t.Fatalf("missing pop map family")
	}
	if family.status != doctorStatusOK {
		t.Fatalf("map family status = %s, want %s (%s)", family.status, doctorStatusOK, family.reason)
	}
	maps := mapFamilyCheck(t, report, "maps listed")
	if maps.status != doctorStatusOK || !strings.Contains(maps.detail, "0 map") {
		t.Fatalf("maps check = %+v, want OK zero-map detail", maps)
	}
}

func TestDoctorMapReadinessOKWithMapsPresent(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, true)
	setDoctorIntent(d, "claude")
	if err := integrate.InstallFileComponent(fakeDeps(installerHome, fs, nil), installerHome, integrate.ComponentTaskSkills, "claude"); err != nil {
		t.Fatalf("install task-skills: %v", err)
	}
	d.scanMaps = func() (int, error) { return 2, nil }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop map")
	if !ok {
		t.Fatalf("missing pop map family")
	}
	if family.status != doctorStatusOK {
		t.Fatalf("map family status = %s, want %s (%s)", family.status, doctorStatusOK, family.reason)
	}
	maps := mapFamilyCheck(t, report, "maps listed")
	if maps.status != doctorStatusOK || !strings.Contains(maps.detail, "2 map") {
		t.Fatalf("maps check = %+v, want OK two-map detail", maps)
	}
	skill := mapFamilyCheck(t, report, "wayfinder planning skill installed")
	if skill.status != doctorStatusOK || !strings.Contains(skill.detail, "claude") {
		t.Fatalf("skill check = %+v, want OK with claude installed", skill)
	}
}

func TestDoctorMapDegradedWhenMapsExistWithoutTaskSkills(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, true)
	setDoctorIntent(d, "claude")
	d.scanMaps = func() (int, error) { return 1, nil }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop map")
	if !ok {
		t.Fatalf("missing pop map family")
	}
	if family.status != doctorStatusDegraded {
		t.Fatalf("map family status = %s, want %s (%s)", family.status, doctorStatusDegraded, family.reason)
	}
	skill := mapFamilyCheck(t, report, "wayfinder planning skill installed")
	if skill.status != doctorStatusDegraded {
		t.Fatalf("skill check status = %s, want %s", skill.status, doctorStatusDegraded)
	}
	if !strings.Contains(skill.detail, "maps exist") || !strings.Contains(skill.detail, "claude (missing)") {
		t.Fatalf("skill detail = %q, want maps-exist missing-agent detail", skill.detail)
	}
	if skill.nextAction != "pop integrate claude --task-skills" {
		t.Fatalf("nextAction = %q, want pop integrate claude --task-skills", skill.nextAction)
	}
}

func TestDoctorMapZeroMapsSkipsTaskSkillsCheck(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	setDoctorIntent(d, "claude")
	d.scanMaps = func() (int, error) { return 0, nil }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop map")
	if !ok {
		t.Fatalf("missing pop map family")
	}
	if family.status != doctorStatusOK {
		t.Fatalf("map family status = %s, want %s (%s)", family.status, doctorStatusOK, family.reason)
	}
	if _, ok := checkByLabel(family, "wayfinder planning skill installed"); ok {
		t.Fatalf("zero maps should not add wayfinder planning skill check")
	}
}

func TestDoctorMapPathOnlyAgentDoesNotDegradeTaskSkills(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	detectDeps := fakeDeps(installerHome, fs, nil)
	intent, err := integrate.DetectAgentIntent(detectDeps, installerHome, func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	}, nil, func(agent string) bool { return agent == "codex" })
	if err != nil {
		t.Fatalf("DetectAgentIntent: %v", err)
	}

	d := readOnlyDoctorDeps(t, fs, true, true, true)
	d.agentIntent = func() (*doctorAgentIntentReport, error) { return intent, nil }
	d.scanMaps = func() (int, error) { return 1, nil }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop map")
	if !ok {
		t.Fatalf("missing pop map family")
	}
	if family.status != doctorStatusOK {
		t.Fatalf("PATH-only codex should not degrade the map family: status = %s (%s)", family.status, family.reason)
	}
	skill := mapFamilyCheck(t, report, "wayfinder planning skill installed")
	if skill.status != doctorStatusOK || skill.detail != "no intended agents detected" {
		t.Fatalf("skill check = %+v, want OK with no intended agents", skill)
	}
}

func TestDoctorMapReadinessBlocksOutsideGit(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.detectRepoContext = func() (*project.RepoContext, error) { return nil, errors.New("not git") }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop map")
	if !ok {
		t.Fatalf("missing pop map family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("map family status = %s, want %s", family.status, doctorStatusBlocked)
	}
	if !strings.Contains(family.reason, "not in a git repository") {
		t.Fatalf("blocked reason should explain outside-git context: %q", family.reason)
	}
}

func TestDoctorMapReadinessBlocksWhenStorageUnwritable(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.taskStorageWritable = func() (string, error) {
		return "", errors.New("write beneath workloads data dir /data/pop/workloads: permission denied")
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop map")
	if !ok {
		t.Fatalf("missing pop map family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("map family status = %s, want %s", family.status, doctorStatusBlocked)
	}
	writable := mapFamilyCheck(t, report, "task storage writable")
	if writable.status != doctorStatusBlocked {
		t.Fatalf("writable status = %s, want %s", writable.status, doctorStatusBlocked)
	}
}

func TestDoctorMapMapListingFailureBlocks(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.scanMaps = func() (int, error) {
		return 0, errors.New("read wayfinder dir: permission denied")
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop map")
	if !ok {
		t.Fatalf("missing pop map family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("map family status = %s, want %s (%s)", family.status, doctorStatusBlocked, family.reason)
	}
}

func TestDoctorWorktreeReadinessOKWithListedWorktrees(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.detectRepoContext = func() (*project.RepoContext, error) {
		return &project.RepoContext{GitRoot: "/repo/app", RepoName: "app"}, nil
	}
	d.listWorktrees = func(*project.RepoContext) ([]project.Worktree, error) {
		return []project.Worktree{{Name: "feature", Path: "/repo/app-feature", Branch: "feature"}}, nil
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop worktree")
	if !ok {
		t.Fatalf("missing pop worktree family")
	}
	if family.status != doctorStatusOK {
		t.Fatalf("worktree status = %s, want %s (%s)", family.status, doctorStatusOK, family.reason)
	}
}

func TestDoctorWorktreeReadinessOKWithZeroLinkedWorktrees(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.detectRepoContext = func() (*project.RepoContext, error) {
		return &project.RepoContext{GitRoot: "/repo/app", RepoName: "app"}, nil
	}
	d.listWorktrees = func(*project.RepoContext) ([]project.Worktree, error) { return nil, nil }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop worktree")
	if !ok {
		t.Fatalf("missing pop worktree family")
	}
	if family.status != doctorStatusOK {
		t.Fatalf("worktree status = %s, want %s (%s)", family.status, doctorStatusOK, family.reason)
	}
	check, ok := checkByLabel(family, "worktrees listed")
	if !ok {
		t.Fatalf("missing worktrees listed check")
	}
	if check.status != doctorStatusOK || !strings.Contains(check.detail, "0 linked") {
		t.Fatalf("list check = %+v, want OK zero-linked detail", check)
	}
}

func TestDoctorWorktreeReadinessBlocksOutsideGit(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.detectRepoContext = func() (*project.RepoContext, error) { return nil, errors.New("not git") }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop worktree")
	if !ok {
		t.Fatalf("missing pop worktree family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("worktree status = %s, want %s", family.status, doctorStatusBlocked)
	}
	if !strings.Contains(family.reason, "not in a git repository") {
		t.Fatalf("blocked reason should explain outside-git context: %q", family.reason)
	}
}

func TestDoctorTaskHealthyStorageIsOK(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop tasks")
	if !ok {
		t.Fatalf("missing pop tasks family")
	}
	if family.status != doctorStatusOK {
		t.Fatalf("task status = %s, want %s (%s)", family.status, doctorStatusOK, family.reason)
	}

	writable := taskCheck(t, report, "task storage writable")
	if writable.status != doctorStatusOK {
		t.Fatalf("writable status = %s, want %s", writable.status, doctorStatusOK)
	}
	if !strings.Contains(writable.detail, "/data/pop/repos") {
		t.Fatalf("writable detail should name the data dir: %q", writable.detail)
	}

	legacy := taskCheck(t, report, "legacy in-tree task sets")
	if legacy.status != doctorStatusOK {
		t.Fatalf("legacy status = %s, want %s", legacy.status, doctorStatusOK)
	}

	orphan := taskCheck(t, report, "orphaned task storage")
	if orphan.status != doctorStatusOK {
		t.Fatalf("orphan status = %s, want %s", orphan.status, doctorStatusOK)
	}
}

func TestDoctorTaskStorageUnwritableBlocks(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.taskStorageWritable = func() (string, error) {
		return "", errors.New("write beneath workloads data dir /data/pop/workloads: permission denied")
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop tasks")
	if !ok {
		t.Fatalf("missing pop tasks family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("task status = %s, want %s", family.status, doctorStatusBlocked)
	}
	check := taskCheck(t, report, "task storage writable")
	if check.status != doctorStatusBlocked {
		t.Fatalf("writable status = %s, want %s", check.status, doctorStatusBlocked)
	}
	if !strings.Contains(check.detail, "cannot create or write") || !strings.Contains(check.detail, "permission denied") {
		t.Fatalf("writable failure should explain the cause: %q", check.detail)
	}
}

func TestDoctorTaskLegacyTaskSetsAdviseMigrate(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.legacyTaskSets = func() ([]string, error) {
		return []string{"2026-01-01-old-set", "2026-02-02-other"}, nil
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop tasks")
	if !ok {
		t.Fatalf("missing pop tasks family")
	}
	if family.status != doctorStatusPartial {
		t.Fatalf("task status = %s, want %s", family.status, doctorStatusPartial)
	}
	check := taskCheck(t, report, "legacy in-tree task sets")
	if check.status != doctorStatusPartial {
		t.Fatalf("legacy status = %s, want %s", check.status, doctorStatusPartial)
	}
	if !strings.Contains(check.detail, "2026-01-01-old-set") || !strings.Contains(check.detail, "thoughts/issues") {
		t.Fatalf("legacy detail should list sets and location: %q", check.detail)
	}
	if check.nextAction != "pop tasks migrate" {
		t.Fatalf("legacy nextAction = %q, want exact migrate command", check.nextAction)
	}
}

func TestDoctorTaskLegacyInspectionFailureIsNA(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.legacyTaskSets = func() ([]string, error) {
		return nil, errors.New("resolve worktree root: not a git repository")
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop tasks")
	if !ok {
		t.Fatalf("missing pop tasks family")
	}
	if family.status != doctorStatusOK {
		t.Fatalf("task status = %s, want %s (legacy inspection failure must not block)", family.status, doctorStatusOK)
	}
	check := taskCheck(t, report, "legacy in-tree task sets")
	if check.status != doctorStatusNA {
		t.Fatalf("legacy status = %s, want %s", check.status, doctorStatusNA)
	}
	if !strings.Contains(check.detail, "not assessed") {
		t.Fatalf("legacy failure detail should mark it not assessed: %q", check.detail)
	}
}

func TestDoctorTaskOrphanStorageIsReportOnly(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.orphanedTaskStorage = func() ([]tasks.OrphanedStorage, error) {
		return []tasks.OrphanedStorage{
			{StorageDir: "/data/pop/workloads/gone-abc123", RepositoryPath: "/vanished/repo/.git"},
		}, nil
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop tasks")
	if !ok {
		t.Fatalf("missing pop tasks family")
	}
	// Orphans are informational: a healthy repository's family stays OK.
	if family.status != doctorStatusOK {
		t.Fatalf("task status = %s, want %s (orphans must not affect healthy status)", family.status, doctorStatusOK)
	}
	check := taskCheck(t, report, "orphaned task storage")
	if check.status != doctorStatusNA {
		t.Fatalf("orphan status = %s, want %s", check.status, doctorStatusNA)
	}
	if !strings.Contains(check.detail, "/data/pop/workloads/gone-abc123") || !strings.Contains(check.detail, "/vanished/repo/.git") {
		t.Fatalf("orphan detail should list storage dir and repository path: %q", check.detail)
	}
	if !strings.Contains(check.detail, "report-only") {
		t.Fatalf("orphan detail should mark it report-only: %q", check.detail)
	}
	if check.nextAction != "" {
		t.Fatalf("orphan check must carry no destructive next action, got %q", check.nextAction)
	}
}

func TestDoctorTaskLegacyLayoutIsReportOnly(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.legacyLayoutStorage = func() ([]string, error) {
		return []string{"/data/pop/workloads/repo-abc123"}, nil
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop tasks")
	if !ok {
		t.Fatalf("missing pop tasks family")
	}
	// An un-migrated layout is a finding, never a readiness failure.
	if family.status != doctorStatusOK {
		t.Fatalf("task status = %s, want %s (legacy layout must not block)", family.status, doctorStatusOK)
	}
	check := taskCheck(t, report, "legacy storage layout")
	if check.status != doctorStatusNA {
		t.Fatalf("legacy layout status = %s, want %s", check.status, doctorStatusNA)
	}
	if !strings.Contains(check.detail, "/data/pop/workloads/repo-abc123") || !strings.Contains(check.detail, "auto-migrated") {
		t.Fatalf("legacy layout detail should list the dir and note auto-migration: %q", check.detail)
	}
}

func TestDoctorDaemonStoppedButStartableIsOKAndExitsZero(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, false)
	out := &bytes.Buffer{}
	if err := runDoctorWith(d, out); err != nil {
		t.Fatalf("doctor must exit 0 on render success even when unhealthy: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "OK        pop monitor    ready") {
		t.Fatalf("startable daemon-down should report OK monitor family:\n%s", s)
	}
	if !strings.Contains(s, "daemon is stopped; normal pop startup can start it") {
		t.Fatalf("daemon-down should explain normal startup path:\n%s", s)
	}
	if !strings.Contains(s, "OK        pop pane       ready") {
		t.Fatalf("daemon-down should not block pane readiness:\n%s", s)
	}
}

func TestDoctorPaneBlocksWhenTmuxUnavailable(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), false, true, true)

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop pane")
	if !ok {
		t.Fatalf("missing pop pane family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("pane status = %s, want %s", family.status, doctorStatusBlocked)
	}
	check, ok := checkByLabel(family, "tmux available")
	if !ok {
		t.Fatalf("missing tmux available check")
	}
	if check.status != doctorStatusBlocked || !strings.Contains(check.detail, "tmux executable") {
		t.Fatalf("tmux check = %+v, want blocked unavailable detail", check)
	}
}

func TestDoctorPaneOKWhenProjectTargetSessionAddressable(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, false)
	d.paneSessionAddressable = func() (string, error) {
		return "target project sessions can be addressed with --project", nil
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop pane")
	if !ok {
		t.Fatalf("missing pop pane family")
	}
	if family.status != doctorStatusOK {
		t.Fatalf("pane status = %s, want %s (%s)", family.status, doctorStatusOK, family.reason)
	}
	check, ok := checkByLabel(family, "pane target session addressable")
	if !ok {
		t.Fatalf("missing pane target session addressable check")
	}
	if check.status != doctorStatusOK || !strings.Contains(check.detail, "--project") {
		t.Fatalf("session check = %+v, want OK project target detail", check)
	}
}

func TestDoctorPaneNotBlockedByStoppedUnstartableMonitorDaemon(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, false)
	d.monitorDaemonStartable = func() bool { return false }

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	pane, ok := familyByCommand(report, "pop pane")
	if !ok {
		t.Fatalf("missing pop pane family")
	}
	if pane.status != doctorStatusOK {
		t.Fatalf("pane status = %s, want %s (%s)", pane.status, doctorStatusOK, pane.reason)
	}
	monitorFamily, ok := familyByCommand(report, "pop monitor")
	if !ok {
		t.Fatalf("missing pop monitor family")
	}
	if monitorFamily.status != doctorStatusBlocked {
		t.Fatalf("monitor status = %s, want %s", monitorFamily.status, doctorStatusBlocked)
	}
}

func TestDoctorMonitorBlocksWhenStateUnreadable(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.loadMonitorState = func() (*monitor.State, error) {
		return nil, errors.New("permission denied")
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop monitor")
	if !ok {
		t.Fatalf("missing pop monitor family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("monitor status = %s, want %s", family.status, doctorStatusBlocked)
	}
	if !strings.Contains(family.reason, "permission denied") {
		t.Fatalf("blocked reason should name unreadable state: %q", family.reason)
	}
}

func TestDoctorMonitorDegradedWhenAutomaticTrackingQualityReduced(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), false, true, true)

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop monitor")
	if !ok {
		t.Fatalf("missing pop monitor family")
	}
	if family.status != doctorStatusDegraded {
		t.Fatalf("monitor status = %s, want %s (%s)", family.status, doctorStatusDegraded, family.reason)
	}
	check, ok := checkByLabel(family, "automatic visit/status quality")
	if !ok {
		t.Fatalf("missing automatic visit/status quality check")
	}
	if check.status != doctorStatusDegraded || !strings.Contains(check.detail, "automatic pane tracking quality is reduced") {
		t.Fatalf("quality check = %+v, want degraded tracking detail", check)
	}
}

func TestDoctorMonitorPartialOnlyForMixedIntendedAgentStatusWiring(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		wiring     []doctorAgentStatusWiring
		wantStatus doctorStatus
	}{
		{
			name: "all intended agents wired",
			wiring: []doctorAgentStatusWiring{
				{Agent: "claude", State: integrate.ComponentStateInfo{Kind: integrate.StateInstalledCurrent}},
				{Agent: "codex", State: integrate.ComponentStateInfo{Kind: integrate.StateInstalledCurrent}},
			},
			wantStatus: doctorStatusOK,
		},
		{
			name: "no intended agents wired",
			wiring: []doctorAgentStatusWiring{
				{Agent: "claude", State: integrate.ComponentStateInfo{Kind: integrate.StateNotInstalled}},
				{Agent: "codex", State: integrate.ComponentStateInfo{Kind: integrate.StateNotInstalled}},
			},
			wantStatus: doctorStatusDegraded,
		},
		{
			name: "mixed intended agents",
			wiring: []doctorAgentStatusWiring{
				{Agent: "claude", State: integrate.ComponentStateInfo{Kind: integrate.StateInstalledCurrent}},
				{Agent: "codex", State: integrate.ComponentStateInfo{Kind: integrate.StateNotInstalled}},
			},
			wantStatus: doctorStatusPartial,
		},
		{
			name: "mixed intended agents with stale wiring",
			wiring: []doctorAgentStatusWiring{
				{Agent: "claude", State: integrate.ComponentStateInfo{Kind: integrate.StateInstalledCurrent}},
				{Agent: "codex", State: integrate.ComponentStateInfo{Kind: integrate.StateStale}},
			},
			wantStatus: doctorStatusPartial,
		},
		{
			name: "mixed intended agents with conflicting wiring",
			wiring: []doctorAgentStatusWiring{
				{Agent: "claude", State: integrate.ComponentStateInfo{Kind: integrate.StateInstalledCurrent}},
				{Agent: "codex", State: integrate.ComponentStateInfo{Kind: integrate.StateConflict, ConflictPath: "/home/me/.codex/hooks.json"}},
			},
			wantStatus: doctorStatusPartial,
		},
		{
			name: "no usable intended agents because wiring conflicts",
			wiring: []doctorAgentStatusWiring{
				{Agent: "claude", State: integrate.ComponentStateInfo{Kind: integrate.StateConflict, ConflictPath: "/home/me/.claude/settings.json"}},
				{Agent: "codex", State: integrate.ComponentStateInfo{Kind: integrate.StateConflict, ConflictPath: "/home/me/.codex/hooks.json"}},
			},
			wantStatus: doctorStatusDegraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check, ok := doctorIntendedAgentStatusWiringCheck(tt.wiring)
			if !ok {
				t.Fatalf("missing intended agent status wiring check")
			}
			if check.status != tt.wantStatus {
				t.Fatalf("wiring check status = %s, want %s", check.status, tt.wantStatus)
			}
		})
	}
}

func TestRenderDoctorReportPinsRepresentativeFamilyOutput(t *testing.T) {
	t.Parallel()
	report := &doctorReport{update: release.Result{Current: "2026.6.0", Latest: "2026.6.0", State: release.StateCurrent}, families: []doctorFamilyReport{
		familyReport("pop project", []doctorCheck{
			{label: "config loads", status: doctorStatusOK, detail: "/cfg/config.toml"},
		}),
		familyReport("pop pane", []doctorCheck{
			{label: "tmux available", status: doctorStatusPartial, detail: "tmux executable was not found", nextAction: "brew install tmux"},
		}),
		familyReport("pop tasks", []doctorCheck{
			{label: "task manifest readable", status: doctorStatusDegraded, detail: "manifest read returned inconsistent state"},
		}),
		familyReport("pop integrate", []doctorCheck{
			{label: "intended agent setup repair path", status: doctorStatusOK, detail: "can inspect and repair intended agent setup through pop integrate for: claude (task config)"},
			{label: "codex available agent suggestion", status: doctorStatusNA, detail: "agent executable is available on PATH but no Pop intent was detected", nextAction: "pop integrate codex"},
		}),
	}}

	out := &bytes.Buffer{}
	renderDoctorReport(out, report)

	want := `pop 2026.6.0 (latest)

Command-family readiness

STATUS    COMMAND        SUMMARY
OK        pop project    ready
  OK        config loads - /cfg/config.toml
Partial   pop pane       tmux executable was not found
  Partial   tmux available - tmux executable was not found (next: brew install tmux)
Degraded  pop tasks      manifest read returned inconsistent state
  Degraded  task manifest readable - manifest read returned inconsistent state
OK        pop integrate  ready
  OK        intended agent setup repair path - can inspect and repair intended agent setup through pop integrate for: claude (task config)
  N/A       codex available agent suggestion - agent executable is available on PATH but no Pop intent was detected (next: pop integrate codex)
`
	if out.String() != want {
		t.Fatalf("rendered doctor report mismatch:\nwant:\n%s\ngot:\n%s", want, out.String())
	}
}

func TestDoctorUpdateHeaderStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		res  release.Result
		want string
	}{
		{"outdated", release.Result{Current: "2026.6.0", Latest: "2026.6.1", State: release.StateOutdated}, "pop 2026.6.0 (latest: 2026.6.1 — update available)"},
		{"current", release.Result{Current: "2026.6.0", Latest: "2026.6.0", State: release.StateCurrent}, "pop 2026.6.0 (latest)"},
		{"dev", release.Result{Current: "2026.6.0-5-gabc123-dirty", State: release.StateDev}, "pop 2026.6.0-5-gabc123-dirty (dev build)"},
		{"failed", release.Result{Current: "2026.6.0", State: release.StateFailed}, "pop 2026.6.0 (update check failed)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// color=false so dim notes render as plain text for assertion.
			if got := doctorUpdateHeader(false, c.res); got != c.want {
				t.Errorf("doctorUpdateHeader = %q, want %q", got, c.want)
			}
		})
	}
}

// TestDoctorUpdateCheckNeverAffectsFamilyStatus asserts the four Update-check
// states leave every family's status untouched (CONTEXT.md "Update notice").
func TestDoctorUpdateCheckNeverAffectsFamilyStatus(t *testing.T) {
	t.Parallel()
	states := []release.Result{
		{Current: "2026.6.0", Latest: "2026.6.1", State: release.StateOutdated},
		{Current: "2026.6.0", Latest: "2026.6.0", State: release.StateCurrent},
		{Current: "2026.6.0-5-gabc123-dirty", State: release.StateDev},
		{Current: "2026.6.0", State: release.StateFailed},
	}

	var baseline []doctorFamilyReport
	for i, st := range states {
		fs := newFakeFS()
		d := readOnlyDoctorDeps(t, fs, true, true, true)
		st := st
		d.updateCheck = func() release.Result { return st }
		report, err := buildDoctorReport(d)
		if err != nil {
			t.Fatalf("buildDoctorReport: %v", err)
		}
		if report.update.State != st.State {
			t.Errorf("report.update.State = %v, want %v", report.update.State, st.State)
		}
		if i == 0 {
			baseline = report.families
			continue
		}
		if len(report.families) != len(baseline) {
			t.Fatalf("family count changed across update states")
		}
		for j := range report.families {
			if report.families[j].command != baseline[j].command || report.families[j].status != baseline[j].status {
				t.Errorf("family %q status changed with update state %v: %v vs baseline %v",
					report.families[j].command, st.State, report.families[j].status, baseline[j].status)
			}
		}
	}
}

func TestDoctorTaskAgentAuthenticationPerPreset(t *testing.T) {
	t.Parallel()
	rows := []tasks.AgentCatalogRow{
		{Agent: "cursor"},
		{Agent: "claude"},
		{Agent: "pi"},
		{Agent: "opencode"},
	}
	probes := map[string]tasks.AgentAuthenticationProbe{
		"cursor":   {Status: tasks.AgentAuthUnauthenticated, Detail: `{"isAuthenticated":false}`},
		"claude":   {Status: tasks.AgentAuthAuthenticated, Detail: "authenticated"},
		"pi":       {Status: tasks.AgentAuthCannotDetermine, Detail: "pop cannot determine authentication (no availability probe)"},
		"opencode": {Status: tasks.AgentAuthCannotDetermine, Detail: "pop cannot determine authentication (no availability probe)"},
	}
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.agentCatalog = func() []tasks.AgentCatalogRow { return rows }
	d.probeAgentAuthentication = func(preset string) tasks.AgentAuthenticationProbe {
		return probes[preset]
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop tasks")
	if !ok {
		t.Fatalf("missing pop tasks family")
	}
	for preset, want := range probes {
		check, ok := checkByLabel(family, preset+" authentication")
		if !ok {
			t.Fatalf("missing %q authentication check", preset)
		}
		switch want.Status {
		case tasks.AgentAuthAuthenticated:
			if check.status != doctorStatusOK || check.detail != want.Detail {
				t.Fatalf("%s check = %+v, want OK %q", preset, check, want.Detail)
			}
		case tasks.AgentAuthUnauthenticated:
			if check.status != doctorStatusPartial || check.detail != want.Detail {
				t.Fatalf("%s check = %+v, want Partial %q", preset, check, want.Detail)
			}
		case tasks.AgentAuthCannotDetermine:
			if check.status != doctorStatusNA || check.detail != want.Detail {
				t.Fatalf("%s check = %+v, want N/A %q", preset, check, want.Detail)
			}
		default:
			t.Fatalf("unexpected probe status for %s", preset)
		}
	}
}

func TestDoctorAgentAuthenticationUnknownNotUnauthenticated(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.agentCatalog = func() []tasks.AgentCatalogRow {
		return []tasks.AgentCatalogRow{{Agent: "cursor"}}
	}
	d.probeAgentAuthentication = func(string) tasks.AgentAuthenticationProbe {
		return tasks.AgentAuthenticationProbe{
			Status: tasks.AgentAuthUnknown,
			Detail: "authentication status unknown",
		}
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	check := taskCheck(t, report, "cursor authentication")
	if check.status != doctorStatusNA {
		t.Fatalf("status = %s, want %s", check.status, doctorStatusNA)
	}
	if check.status == doctorStatusPartial || check.status == doctorStatusBlocked {
		t.Fatalf("unknown probe must not read as unauthenticated: %+v", check)
	}
	if check.detail != "authentication status unknown" {
		t.Fatalf("detail = %q, want authentication status unknown", check.detail)
	}
}

func TestDoctorStatusAggregation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		checks     []doctorCheck
		wantStatus doctorStatus
		wantReason string
	}{
		{
			name: "ok",
			checks: []doctorCheck{
				{label: "a", status: doctorStatusOK},
				{label: "b", status: doctorStatusOK},
			},
			wantStatus: doctorStatusOK,
		},
		{
			name: "partial",
			checks: []doctorCheck{
				{label: "a", status: doctorStatusOK},
				{label: "b", status: doctorStatusPartial, detail: "component missing"},
			},
			wantStatus: doctorStatusPartial,
			wantReason: "component missing",
		},
		{
			name: "degraded",
			checks: []doctorCheck{
				{label: "a", status: doctorStatusPartial, detail: "component missing"},
				{label: "b", status: doctorStatusDegraded, detail: "state unknown"},
			},
			wantStatus: doctorStatusDegraded,
			wantReason: "state unknown",
		},
		{
			name: "blocked",
			checks: []doctorCheck{
				{label: "a", status: doctorStatusDegraded, detail: "state unknown"},
				{label: "b", status: doctorStatusBlocked, detail: "conflict exists"},
			},
			wantStatus: doctorStatusBlocked,
			wantReason: "conflict exists",
		},
		{
			name: "na",
			checks: []doctorCheck{
				{label: "a", status: doctorStatusNA, detail: "not supported"},
				{label: "b", status: doctorStatusNA, detail: "not applicable"},
			},
			wantStatus: doctorStatusNA,
			wantReason: "not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStatus, gotReason := aggregateDoctorStatus(tt.checks)
			if gotStatus != tt.wantStatus {
				t.Fatalf("status = %s, want %s", gotStatus, tt.wantStatus)
			}
			if gotReason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", gotReason, tt.wantReason)
			}
		})
	}
}

// TestDoctorManagedWorktreeRootMoveGatedNamesOffenders: a gated root move is the
// one thing in this family a human has to act on, so it is reported as Degraded
// with each blocking worktree named.
func TestDoctorManagedWorktreeRootMoveGatedNamesOffenders(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.pendingWorktreeRootMove = func() (binding.WorktreeRootMove, error) {
		return binding.WorktreeRootMove{
			From:     "/data/pop/queue/worktrees",
			To:       "/data/pop/work/worktrees",
			Pending:  []string{"repo-abc123/set-one"},
			Refusals: []string{"repo-abc123/set-one: uncommitted changes in /data/pop/queue/worktrees/repo-abc123/set-one"},
		}, nil
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	check := taskCheck(t, report, "managed worktree root")
	if check.status != doctorStatusPartial {
		t.Fatalf("managed worktree root status = %s, want %s", check.status, doctorStatusPartial)
	}
	if !strings.Contains(check.detail, "repo-abc123/set-one") || !strings.Contains(check.detail, "uncommitted changes") {
		t.Fatalf("detail should name the blocking worktree: %q", check.detail)
	}
	if check.nextAction == "" {
		t.Fatal("a gated move must carry a next action")
	}
}

// TestDoctorManagedWorktreeRootMovePendingIsReportOnly: a move nothing blocks
// happens by itself on the next binding touch, so it never drives readiness down.
func TestDoctorManagedWorktreeRootMovePendingIsReportOnly(t *testing.T) {
	t.Parallel()
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.pendingWorktreeRootMove = func() (binding.WorktreeRootMove, error) {
		return binding.WorktreeRootMove{
			From:    "/data/pop/queue/worktrees",
			To:      "/data/pop/work/worktrees",
			Pending: []string{"repo-abc123/set-one"},
		}, nil
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop tasks")
	if !ok {
		t.Fatalf("missing pop tasks family")
	}
	if family.status != doctorStatusOK {
		t.Fatalf("task status = %s, want %s (a pending move must not block)", family.status, doctorStatusOK)
	}
	check := taskCheck(t, report, "managed worktree root")
	if check.status != doctorStatusNA {
		t.Fatalf("managed worktree root status = %s, want %s", check.status, doctorStatusNA)
	}
	if !strings.Contains(check.detail, "/data/pop/work/worktrees") {
		t.Fatalf("detail should name the destination root: %q", check.detail)
	}
}
