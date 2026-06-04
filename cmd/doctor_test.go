package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		configCheck: func() (bool, string) {
			if cfgOK {
				return true, "/cfg/config.toml"
			}
			return false, "/cfg/config.toml: not found"
		},
		daemonRunning: func() bool { return daemon },
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

func TestDoctorDaemonDownIsBlockedButExitsZero(t *testing.T) {
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, false)
	out := &bytes.Buffer{}
	if err := runDoctorWith(d, out); err != nil {
		t.Fatalf("doctor must exit 0 on render success even when unhealthy: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Blocked   pop monitor    monitor daemon is not running") {
		t.Fatalf("daemon-down should report blocked monitor family:\n%s", s)
	}
	if !strings.Contains(s, "(next: pop monitor)") {
		t.Fatalf("daemon-down should carry a next action:\n%s", s)
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
