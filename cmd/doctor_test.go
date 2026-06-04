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
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/project"
)

// readOnlyDoctorDeps wires a doctorDeps over a fakeFS with injectable core
// checks. Every write operation fails the test because doctor is strictly
// read-only.
func readOnlyDoctorDeps(t *testing.T, fs *fakeFS, tmux, cfgOK, daemon bool) *doctorDeps {
	t.Helper()
	base := fakeDeps(installerHome, fs, nil)
	base.writeFile = func(string, []byte, os.FileMode) error { t.Fatalf("doctor wrote a file"); return nil }
	base.mkdirAll = func(string, os.FileMode) error { t.Fatalf("doctor created a directory"); return nil }
	base.removeAll = func(string) error { t.Fatalf("doctor removed a path"); return nil }
	base.symlink = func(string, string) error { t.Fatalf("doctor created a symlink"); return nil }
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
		intendedAgentStatusWiring: func() ([]doctorAgentStatusWiring, error) {
			return []doctorAgentStatusWiring{{agent: "claude", state: componentStateInfo{kind: stateInstalledCurrent}}}, nil
		},
		resolveWorkloadRuntime: func() (string, error) {
			return "/repo/app", nil
		},
		workloadArtifactIgnored: func(runtimePath, probePath string) (bool, error) {
			if runtimePath != "/repo/app" {
				t.Fatalf("workload ignore probe runtime = %q, want /repo/app", runtimePath)
			}
			if !strings.HasPrefix(probePath, "thoughts/") {
				t.Fatalf("workload ignore probe path = %q, want path under thoughts/", probePath)
			}
			return true, nil
		},
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

func workloadIgnoreCheck(t *testing.T, report *doctorReport) doctorCheck {
	t.Helper()
	family, ok := familyByCommand(report, "pop workload")
	if !ok {
		t.Fatalf("missing pop workload family")
	}
	check, ok := checkByLabel(family, "workload artifact ignore coverage")
	if !ok {
		t.Fatalf("missing workload artifact ignore coverage check")
	}
	return check
}

func TestDoctorReportsCanonicalCommandFamilies(t *testing.T) {
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, true)
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
}

func TestDoctorNestedChecksAreGenericAndActionable(t *testing.T) {
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, true)
	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}

	integrate, ok := familyByCommand(report, "pop integrate")
	if !ok {
		t.Fatalf("missing pop integrate family")
	}
	check, ok := checkByLabel(integrate, "claude pane-skill")
	if !ok {
		t.Fatalf("missing claude pane-skill check")
	}
	if check.status != doctorStatusPartial {
		t.Fatalf("check status = %s, want %s", check.status, doctorStatusPartial)
	}
	if check.detail == "" {
		t.Fatalf("non-OK check must carry detail")
	}
	if check.nextAction != "pop integrate claude --pane-skill" {
		t.Fatalf("nextAction = %q, want pane-skill integrate command", check.nextAction)
	}
}

func TestDoctorReadOnlyConflictCheck(t *testing.T) {
	fs := newFakeFS()
	conflictPath := filepath.Join(installerHome, ".claude", "skills", "pane")
	fs.files[conflictPath] = []byte("my own skill")

	d := readOnlyDoctorDeps(t, fs, true, true, true)
	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}

	integrate, ok := familyByCommand(report, "pop integrate")
	if !ok {
		t.Fatalf("missing pop integrate family")
	}
	check, ok := checkByLabel(integrate, "claude pane-skill")
	if !ok {
		t.Fatalf("missing claude pane-skill check")
	}
	if check.status != doctorStatusBlocked {
		t.Fatalf("check status = %s, want %s", check.status, doctorStatusBlocked)
	}
	if !strings.Contains(check.detail, conflictPath) {
		t.Fatalf("blocked detail must name conflict path: %q", check.detail)
	}
	if !strings.Contains(check.nextAction, conflictPath) || !strings.Contains(check.nextAction, "pop integrate claude --pane-skill") {
		t.Fatalf("blocked next action must remove path and re-integrate: %q", check.nextAction)
	}
	if string(fs.files[conflictPath]) != "my own skill" {
		t.Fatalf("doctor modified the user's own file")
	}
}

func TestDoctorStaleComponentIsPartialCheck(t *testing.T) {
	fs := newFakeFS()
	setup := fakeDeps(installerHome, fs, nil)
	if err := installFileComponent(setup, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("pre-install: %v", err)
	}
	renderFile, _, _ := paneSkillPaths()
	fs.files[renderFile] = []byte("drifted content not matching the embedded source")

	d := readOnlyDoctorDeps(t, fs, true, true, true)
	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}

	integrate, ok := familyByCommand(report, "pop integrate")
	if !ok {
		t.Fatalf("missing pop integrate family")
	}
	check, ok := checkByLabel(integrate, "claude pane-skill")
	if !ok {
		t.Fatalf("missing claude pane-skill check")
	}
	if check.status != doctorStatusPartial {
		t.Fatalf("check status = %s, want %s", check.status, doctorStatusPartial)
	}
	if !strings.Contains(check.detail, "stale") {
		t.Fatalf("stale check should carry concrete detail: %q", check.detail)
	}
	if check.nextAction != "pop integrate claude --pane-skill" {
		t.Fatalf("nextAction = %q, want pane-skill integrate command", check.nextAction)
	}
}

func TestDoctorHealthyCoreFamiliesRenderOK(t *testing.T) {
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

func TestDoctorWorktreeReadinessOKWithListedWorktrees(t *testing.T) {
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

func TestDoctorWorkloadBlocksOutsideGitAndIgnoreCoverageIsNA(t *testing.T) {
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.resolveWorkloadRuntime = func() (string, error) {
		return "", errors.New("runtime path \"/tmp/outside\" is not a git checkout")
	}
	d.workloadArtifactIgnored = func(string, string) (bool, error) {
		t.Fatal("ignore coverage should not be probed without a git runtime checkout")
		return false, nil
	}

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop workload")
	if !ok {
		t.Fatalf("missing pop workload family")
	}
	if family.status != doctorStatusBlocked {
		t.Fatalf("workload status = %s, want %s", family.status, doctorStatusBlocked)
	}
	if !strings.Contains(family.reason, "no git runtime checkout resolved") {
		t.Fatalf("blocked reason should explain missing git runtime: %q", family.reason)
	}
	check := workloadIgnoreCheck(t, report)
	if check.status != doctorStatusNA {
		t.Fatalf("ignore coverage status = %s, want %s", check.status, doctorStatusNA)
	}
	if strings.Contains(check.detail, "missing") || strings.Contains(check.detail, "not cover") {
		t.Fatalf("outside-git ignore coverage should not report a missing-rule false positive: %q", check.detail)
	}
}

func TestDoctorWorkloadEffectiveIgnoreCoverageSources(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, repo string)
	}{
		{
			name: "configured global excludes",
			setup: func(t *testing.T, repo string) {
				excludes := filepath.Join(t.TempDir(), "global-ignore")
				if err := os.WriteFile(excludes, []byte("thoughts/\n"), 0o644); err != nil {
					t.Fatalf("write global excludes: %v", err)
				}
				runDoctorGit(t, repo, "config", "core.excludesfile", excludes)
			},
		},
		{
			name: "git info exclude",
			setup: func(t *testing.T, repo string) {
				if err := os.WriteFile(filepath.Join(repo, ".git", "info", "exclude"), []byte("thoughts/\n"), 0o644); err != nil {
					t.Fatalf("write info exclude: %v", err)
				}
			},
		},
		{
			name: "repository gitignore",
			setup: func(t *testing.T, repo string) {
				if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("thoughts/\n"), 0o644); err != nil {
					t.Fatalf("write .gitignore: %v", err)
				}
			},
		},
		{
			name: "default global ignore",
			setup: func(t *testing.T, repo string) {
				xdg := filepath.Join(t.TempDir(), "xdg")
				target := filepath.Join(xdg, "git", "ignore")
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatalf("mkdir default global ignore: %v", err)
				}
				if err := os.WriteFile(target, []byte("thoughts/\n"), 0o644); err != nil {
					t.Fatalf("write default global ignore: %v", err)
				}
				t.Setenv("XDG_CONFIG_HOME", xdg)
			},
		},
		{
			name: "pop workload gitignore step",
			setup: func(t *testing.T, repo string) {
				d := &integrateDeps{
					userHomeDir: func() (string, error) { return os.Getenv("HOME"), nil },
					readFile:    os.ReadFile,
					writeFile:   os.WriteFile,
					mkdirAll:    os.MkdirAll,
					gitConfig:   func(string) (string, error) { return "", os.ErrNotExist },
					getenv:      os.Getenv,
				}
				if err := installGitignore(d, "", "claude"); err != nil {
					t.Fatalf("install workload gitignore: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "empty-xdg"))
			repo := initDoctorGitRepo(t)
			tt.setup(t, repo)

			d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
			d.resolveWorkloadRuntime = func() (string, error) { return repo, nil }
			d.workloadArtifactIgnored = defaultDoctorWorkloadArtifactIgnored

			report, err := buildDoctorReport(d)
			if err != nil {
				t.Fatalf("buildDoctorReport: %v", err)
			}
			family, ok := familyByCommand(report, "pop workload")
			if !ok {
				t.Fatalf("missing pop workload family")
			}
			if family.status != doctorStatusOK {
				t.Fatalf("workload status = %s, want %s (%s)", family.status, doctorStatusOK, family.reason)
			}
			check := workloadIgnoreCheck(t, report)
			if check.status != doctorStatusOK {
				t.Fatalf("ignore coverage status = %s, want %s (%s)", check.status, doctorStatusOK, check.detail)
			}
			if !strings.Contains(check.detail, "git check-ignore") || !strings.Contains(check.detail, doctorWorkloadIgnoreProbe) {
				t.Fatalf("ignore coverage should disclose probe method and path: %q", check.detail)
			}
			if _, err := os.Stat(filepath.Join(repo, doctorWorkloadIgnoreProbe)); !os.IsNotExist(err) {
				t.Fatalf("ignore probe should not create representative artifact, stat err = %v", err)
			}
		})
	}
}

func TestDoctorWorkloadMissingEffectiveIgnoreCoverageIsActionable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "empty-xdg"))
	repo := initDoctorGitRepo(t)
	d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
	d.resolveWorkloadRuntime = func() (string, error) { return repo, nil }
	d.workloadArtifactIgnored = defaultDoctorWorkloadArtifactIgnored

	report, err := buildDoctorReport(d)
	if err != nil {
		t.Fatalf("buildDoctorReport: %v", err)
	}
	family, ok := familyByCommand(report, "pop workload")
	if !ok {
		t.Fatalf("missing pop workload family")
	}
	if family.status == doctorStatusOK {
		t.Fatalf("workload status = %s, want non-OK when ignore coverage is missing", family.status)
	}
	check := workloadIgnoreCheck(t, report)
	if check.status == doctorStatusOK || check.status == doctorStatusNA {
		t.Fatalf("ignore coverage status = %s, want actionable non-OK", check.status)
	}
	if !strings.Contains(check.detail, "does not cover") || !strings.Contains(check.detail, gitignoreLine) {
		t.Fatalf("missing ignore coverage should explain the rule to add: %q", check.detail)
	}
	if !strings.Contains(check.nextAction, "pop integrate") && !strings.Contains(check.nextAction, ".gitignore") {
		t.Fatalf("missing ignore coverage should include clear next action: %q", check.nextAction)
	}
}

func TestDoctorDaemonStoppedButStartableIsOKAndExitsZero(t *testing.T) {
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
	tests := []struct {
		name       string
		wiring     []doctorAgentStatusWiring
		wantStatus doctorStatus
	}{
		{
			name: "all intended agents wired",
			wiring: []doctorAgentStatusWiring{
				{agent: "claude", state: componentStateInfo{kind: stateInstalledCurrent}},
				{agent: "codex", state: componentStateInfo{kind: stateInstalledCurrent}},
			},
			wantStatus: doctorStatusOK,
		},
		{
			name: "no intended agents wired",
			wiring: []doctorAgentStatusWiring{
				{agent: "claude", state: componentStateInfo{kind: stateNotInstalled}},
				{agent: "codex", state: componentStateInfo{kind: stateNotInstalled}},
			},
			wantStatus: doctorStatusDegraded,
		},
		{
			name: "mixed intended agents",
			wiring: []doctorAgentStatusWiring{
				{agent: "claude", state: componentStateInfo{kind: stateInstalledCurrent}},
				{agent: "codex", state: componentStateInfo{kind: stateNotInstalled}},
			},
			wantStatus: doctorStatusPartial,
		},
		{
			name: "mixed intended agents with stale wiring",
			wiring: []doctorAgentStatusWiring{
				{agent: "claude", state: componentStateInfo{kind: stateInstalledCurrent}},
				{agent: "codex", state: componentStateInfo{kind: stateStale}},
			},
			wantStatus: doctorStatusPartial,
		},
		{
			name: "mixed intended agents with conflicting wiring",
			wiring: []doctorAgentStatusWiring{
				{agent: "claude", state: componentStateInfo{kind: stateInstalledCurrent}},
				{agent: "codex", state: componentStateInfo{kind: stateConflict, conflictPath: "/home/me/.codex/hooks.json"}},
			},
			wantStatus: doctorStatusPartial,
		},
		{
			name: "no usable intended agents because wiring conflicts",
			wiring: []doctorAgentStatusWiring{
				{agent: "claude", state: componentStateInfo{kind: stateConflict, conflictPath: "/home/me/.claude/settings.json"}},
				{agent: "codex", state: componentStateInfo{kind: stateConflict, conflictPath: "/home/me/.codex/hooks.json"}},
			},
			wantStatus: doctorStatusDegraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := readOnlyDoctorDeps(t, newFakeFS(), true, true, true)
			d.intendedAgentStatusWiring = func() ([]doctorAgentStatusWiring, error) { return tt.wiring, nil }

			report, err := buildDoctorReport(d)
			if err != nil {
				t.Fatalf("buildDoctorReport: %v", err)
			}
			family, ok := familyByCommand(report, "pop monitor")
			if !ok {
				t.Fatalf("missing pop monitor family")
			}
			if family.status != tt.wantStatus {
				t.Fatalf("monitor status = %s, want %s (%s)", family.status, tt.wantStatus, family.reason)
			}
			check, ok := checkByLabel(family, "intended agent status wiring")
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
	report := &doctorReport{families: []doctorFamilyReport{
		familyReport("pop project", []doctorCheck{
			{label: "config loads", status: doctorStatusOK, detail: "/cfg/config.toml"},
		}),
		familyReport("pop pane", []doctorCheck{
			{label: "tmux available", status: doctorStatusPartial, detail: "tmux executable was not found", nextAction: "brew install tmux"},
		}),
		familyReport("pop workload", []doctorCheck{
			{label: "issue manifest readable", status: doctorStatusDegraded, detail: "manifest read returned inconsistent state"},
		}),
		familyReport("pop integrate", []doctorCheck{
			{label: "claude pane-skill", status: doctorStatusBlocked, detail: "claude pane-skill conflicts at /home/me/.claude/skills/pane", nextAction: "rm /home/me/.claude/skills/pane && pop integrate claude --pane-skill"},
		}),
	}}

	out := &bytes.Buffer{}
	renderDoctorReport(out, report)

	want := `Command-family readiness

STATUS    COMMAND        SUMMARY
OK        pop project    ready
  OK        config loads - /cfg/config.toml
Partial   pop pane       tmux executable was not found
  Partial   tmux available - tmux executable was not found (next: brew install tmux)
Degraded  pop workload   manifest read returned inconsistent state
  Degraded  issue manifest readable - manifest read returned inconsistent state
Blocked   pop integrate  claude pane-skill conflicts at /home/me/.claude/skills/pane
  Blocked   claude pane-skill - claude pane-skill conflicts at /home/me/.claude/skills/pane (next: rm /home/me/.claude/skills/pane && pop integrate claude --pane-skill)
`
	if out.String() != want {
		t.Fatalf("rendered doctor report mismatch:\nwant:\n%s\ngot:\n%s", want, out.String())
	}
}

func TestDoctorStatusAggregation(t *testing.T) {
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
