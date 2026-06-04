package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readOnlyDoctorDeps wires a doctorDeps over a fakeFS with injectable core
// checks. Every write seam fails the test: doctor is strictly read-only, so a
// write under any state combination is a bug (acceptance: "Doctor performs no
// writes under any state combination").
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

// claudeStatusWired writes a settings.json carrying a pop hook so claude's
// status-wiring component reads as installed.
func claudeStatusWired(fs *fakeFS) {
	settings := filepath.Join(installerHome, ".claude", "settings.json")
	fs.files[settings] = []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"pop pane set-status unread 2>/dev/null || true"}]}]}}`)
}

// rowLine returns the table line for a given agent/component, or "" if absent.
func rowLine(out, agent, component string) string {
	for _, ln := range strings.Split(out, "\n") {
		fields := strings.Fields(ln)
		if len(fields) >= 2 && fields[0] == agent && fields[1] == component {
			return ln
		}
	}
	return ""
}

// TestDoctorHealthy: all core checks pass and an installed-current component
// renders without a fix command.
func TestDoctorHealthy(t *testing.T) {
	fs := newFakeFS()
	claudeStatusWired(fs)

	d := readOnlyDoctorDeps(t, fs, true, true, true)
	out := &bytes.Buffer{}
	if err := runDoctorWith(d, out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	s := out.String()

	for _, label := range []string{"tmux available", "config loads", "monitor daemon running"} {
		if !strings.Contains(s, label) {
			t.Fatalf("core check %q missing from output:\n%s", label, s)
		}
	}
	if strings.Contains(s, "[FAIL]") {
		t.Fatalf("healthy machine should report no FAIL core checks:\n%s", s)
	}

	line := rowLine(s, "claude", "status-wiring")
	if !strings.Contains(line, "installed-current") {
		t.Fatalf("claude status-wiring should be installed-current:\n%s", s)
	}
	// Healthy rows carry no fix command.
	if strings.Contains(line, "pop integrate") {
		t.Fatalf("installed-current row must not carry a fix command: %q", line)
	}
}

// TestDoctorNotInstalledCarriesFix: a fresh machine reports not-installed rows
// with the copy-paste integrate command.
func TestDoctorNotInstalledCarriesFix(t *testing.T) {
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, true)
	out := &bytes.Buffer{}
	if err := runDoctorWith(d, out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	s := out.String()

	line := rowLine(s, "claude", "pane-skill")
	if !strings.Contains(line, "not installed") {
		t.Fatalf("claude pane-skill should be not installed:\n%s", s)
	}
	if !strings.Contains(line, "pop integrate claude --pane-skill") {
		t.Fatalf("not-installed row must carry the fix command: %q", line)
	}
}

// TestDoctorStale: an installed pane skill whose render tree drifted from the
// embedded source reports stale and carries the refresh command.
func TestDoctorStale(t *testing.T) {
	fs := newFakeFS()
	// Install the pane skill for real, then drift the render tree on disk.
	setup := fakeDeps(installerHome, fs, nil)
	if err := installFileComponent(setup, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("pre-install: %v", err)
	}
	renderFile, _, _ := paneSkillPaths()
	fs.files[renderFile] = []byte("drifted content not matching the embedded source")

	d := readOnlyDoctorDeps(t, fs, true, true, true)
	out := &bytes.Buffer{}
	if err := runDoctorWith(d, out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	s := out.String()

	line := rowLine(s, "claude", "pane-skill")
	if !strings.Contains(line, "stale") {
		t.Fatalf("claude pane-skill should be stale:\n%s", s)
	}
	if !strings.Contains(line, "pop integrate claude --pane-skill") {
		t.Fatalf("stale row must carry the fix command: %q", line)
	}
}

// TestDoctorConflict: a same-named entry pop does not own reports conflict and
// the fix command removes the conflicting path before re-integrating.
func TestDoctorConflict(t *testing.T) {
	fs := newFakeFS()
	conflictPath := filepath.Join(installerHome, ".claude", "skills", "pane")
	fs.files[conflictPath] = []byte("my own skill")

	d := readOnlyDoctorDeps(t, fs, true, true, true)
	out := &bytes.Buffer{}
	if err := runDoctorWith(d, out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	s := out.String()

	line := rowLine(s, "claude", "pane-skill")
	if !strings.Contains(line, "conflict") {
		t.Fatalf("claude pane-skill should be a conflict:\n%s", s)
	}
	if !strings.Contains(line, conflictPath) || !strings.Contains(line, "pop integrate claude --pane-skill") {
		t.Fatalf("conflict fix must name the path and the re-integrate command: %q", line)
	}
	// The user's own file is never touched (no-write guards would have fired).
	if string(fs.files[conflictPath]) != "my own skill" {
		t.Fatalf("doctor modified the user's own file")
	}
}

// TestDoctorNotSupported: codex cannot host the pane skill, so its row reports
// not supported and carries no fix command.
func TestDoctorNotSupported(t *testing.T) {
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, true)
	out := &bytes.Buffer{}
	if err := runDoctorWith(d, out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	s := out.String()

	line := rowLine(s, "codex", "pane-skill")
	if !strings.Contains(line, "not supported") {
		t.Fatalf("codex pane-skill should be not supported:\n%s", s)
	}
	if strings.Contains(line, "pop integrate") {
		t.Fatalf("not-supported row must not carry a fix command: %q", line)
	}
}

// TestDoctorDaemonDown: a stopped daemon reports FAIL on the core check, and
// doctor still exits 0 (rendering succeeded — exit status ignores findings).
func TestDoctorDaemonDown(t *testing.T) {
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, false)
	out := &bytes.Buffer{}
	if err := runDoctorWith(d, out); err != nil {
		t.Fatalf("doctor must exit 0 on render success even when unhealthy: %v", err)
	}
	s := out.String()

	var daemonLine string
	for _, ln := range strings.Split(s, "\n") {
		if strings.Contains(ln, "monitor daemon running") {
			daemonLine = ln
			break
		}
	}
	if daemonLine == "" || !strings.Contains(daemonLine, "FAIL") {
		t.Fatalf("daemon-down should report FAIL on the monitor daemon check:\n%s", s)
	}
}

// TestDoctorCoversAllAgents: the table includes a row for every agent and every
// catalog component (acceptance: per-agent table covering all five agents).
func TestDoctorCoversAllAgents(t *testing.T) {
	fs := newFakeFS()
	d := readOnlyDoctorDeps(t, fs, true, true, true)
	out := &bytes.Buffer{}
	if err := runDoctorWith(d, out); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	s := out.String()

	for _, agent := range integrationAgents {
		for _, comp := range integrationCatalog {
			if rowLine(s, agent, string(comp.id)) == "" {
				t.Fatalf("missing table row for %s / %s:\n%s", agent, comp.id, s)
			}
		}
	}
}
