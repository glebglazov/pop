package cmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/integrate"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/monitor"
)

// kimiHookPayload is the JSON kimi writes to a hook command's stdin: snake_case
// keys, hook_event_name plus the per-event fields.
const kimiHookPayload = `{"hook_event_name":"PostToolUse","session_id":"sess-1","cwd":"/tmp/repo","tool_name":"Bash"}`

// kimiHookEntry is one [[hooks]] entry as kimi's config loader sees it.
type kimiHookEntry struct {
	Event   string `toml:"event"`
	Command string `toml:"command"`
}

// installedKimiHooks installs kimi's status wiring and returns the hook entries
// pop wrote, read back out of config.toml the way kimi's own loader would.
func installedKimiHooks(t *testing.T) []kimiHookEntry {
	t.Helper()
	fs := newFakeFS()
	if _, err := integrate.Install(fakeDeps(installerHome, fs, io.Discard),
		integrate.Request{Agent: "kimi", CoreOnly: true}); err != nil {
		t.Fatalf("install kimi status wiring: %v", err)
	}
	data, ok := fs.files[filepath.Join(installerHome, ".kimi-code", "config.toml")]
	if !ok {
		t.Fatal("kimi status wiring did not write config.toml")
	}
	var parsed struct {
		Hooks []kimiHookEntry `toml:"hooks"`
	}
	if _, err := toml.Decode(string(data), &parsed); err != nil {
		t.Fatalf("kimi config.toml does not parse: %v\n%s", err, data)
	}
	if len(parsed.Hooks) == 0 {
		t.Fatalf("no [[hooks]] entries written:\n%s", data)
	}
	return parsed.Hooks
}

// setStatusArgsForEvent finds the installed `pop pane set-status <status>` hook
// for an event and returns the status it reports.
func setStatusArgsForEvent(t *testing.T, hooks []kimiHookEntry, event string) string {
	t.Helper()
	for _, h := range hooks {
		if h.Event != event {
			continue
		}
		fields := strings.Fields(h.Command)
		if len(fields) < 4 || fields[0] != "pop" || fields[1] != "pane" || fields[2] != "set-status" {
			continue
		}
		return fields[3]
	}
	t.Fatalf("no pop set-status hook installed for kimi event %s (hooks: %v)", event, hooks)
	return ""
}

// TestKimiHooksReportPaneStatusToMonitor drives the hook commands pop installs
// into kimi's config.toml through pop's real set-status path and asserts the
// Monitor state they produce — the wiring an interactive kimi pane relies on,
// without running kimi.
func TestKimiHooksReportPaneStatusToMonitor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	setCmdLayerDeps(t, newTestCmdDeps(t, "", dir, ""))

	hooks := installedKimiHooks(t)
	statePath := filepath.Join(dir, "pop", "monitor.json")
	cfg := &config.Config{}
	tmux := &tmuxtest.Fake{PaneInfoFunc: func(paneID string) (tmuxmod.PaneInfo, error) {
		return tmuxmod.PaneInfo{Session: "repo", Command: "kimi"}, nil
	}}

	// Turn-level events in the order a kimi session fires them.
	for _, step := range []struct {
		event string
		want  monitor.PaneStatus
	}{
		{"PostToolUse", monitor.StatusWorking},
		{"Stop", monitor.StatusUnread},
		{"Notification", monitor.StatusUnread},
		{"SessionStart", monitor.StatusClear},
	} {
		status := setStatusArgsForEvent(t, hooks, step.event)
		if err := runPaneSetStatusWith(tmux, cfg, "", false, "", []string{"%3", status}); err != nil {
			t.Fatalf("%s hook (%s): %v", step.event, status, err)
		}
		state := loadState(t, statePath)
		entry, ok := state.Panes["%3"]
		if !ok {
			t.Fatalf("%s hook did not register pane %%3 in the monitor", step.event)
		}
		if entry.Status != step.want {
			t.Errorf("%s hook left status %q, want %q", step.event, entry.Status, step.want)
		}
		if entry.Session != "repo" {
			t.Errorf("%s hook registered session %q, want repo", step.event, entry.Session)
		}
	}
}

// TestKimiHookCommandsRunUnderShell executes each installed hook command the way
// kimi does — spawn(command, {shell: true}) with the payload on stdin — against a
// stub pop on PATH, capturing the argv pop is invoked with.
func TestKimiHookCommandsRunUnderShell(t *testing.T) {
	t.Parallel()
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "invocations.log")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + logPath + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "pop"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub pop: %v", err)
	}

	for _, h := range installedKimiHooks(t) {
		cmd := exec.Command("sh", "-c", h.Command)
		cmd.Env = append(os.Environ(), "PATH="+binDir)
		cmd.Stdin = strings.NewReader(kimiHookPayload)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hook %s (%q) failed: %v\n%s", h.Event, h.Command, err, out)
		}
	}

	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("no pop invocations captured: %v", err)
	}
	captured := strings.Split(strings.TrimSpace(string(log)), "\n")
	want := []string{
		"pane set-status clear",
		"pane set-topic --clear",
		"pane set-status working",
		"pane set-status unread",
		"pane set-status unread",
	}
	if len(captured) != len(want) {
		t.Fatalf("captured invocations = %v, want %v", captured, want)
	}
	for i, w := range want {
		if captured[i] != w {
			t.Errorf("invocation %d = %q, want %q", i, captured[i], w)
		}
	}
}

// TestDoctorReportsKimiWiringOnlyWhenIntended: Doctor reads kimi's wiring state
// for an intent-inferred setup, and an unintegrated machine hears nothing about
// kimi — an installed binary alone is a suggestion, not intent.
func TestDoctorReportsKimiWiringOnlyWhenIntended(t *testing.T) {
	t.Parallel()

	t.Run("intended kimi reports its wiring", func(t *testing.T) {
		t.Parallel()
		fs := newFakeFS()
		if _, err := integrate.Install(fakeDeps(installerHome, fs, io.Discard),
			integrate.Request{Agent: "kimi", CoreOnly: true}); err != nil {
			t.Fatalf("install kimi status wiring: %v", err)
		}
		d := readOnlyDoctorDeps(t, fs, true, true, true)
		setDoctorIntent(d, "kimi")

		report, err := buildDoctorReport(d)
		if err != nil {
			t.Fatalf("buildDoctorReport: %v", err)
		}
		monitorFamily, ok := familyByCommand(report, "pop monitor")
		if !ok {
			t.Fatal("missing pop monitor family")
		}
		wiring, ok := checkByLabel(monitorFamily, "intended agent status wiring")
		if !ok {
			t.Fatal("missing intended agent status wiring check")
		}
		if !strings.Contains(wiring.detail, "kimi") || strings.Contains(wiring.detail, "kimi (missing)") {
			t.Errorf("wiring check = %+v, want kimi reported as wired", wiring)
		}
	})

	t.Run("unintegrated kimi stays out of the report", func(t *testing.T) {
		t.Parallel()
		fs := newFakeFS()
		d := readOnlyDoctorDeps(t, fs, true, true, true)
		out := &bytes.Buffer{}
		if err := runDoctorWith(d, out); err != nil {
			t.Fatalf("doctor: %v", err)
		}
		if strings.Contains(out.String(), "kimi") {
			t.Errorf("doctor mentions kimi with no intent detected:\n%s", out)
		}
	})
}
