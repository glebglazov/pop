package cmd

// Daemon-verb tests that stub cfgFile or package globals stay serial (ADR-0145).

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboard"
	"github.com/glebglazov/pop/tasks/drain"
)

func writeDaemonConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunWorkDaemonHonorsConfiguredPollInterval(t *testing.T) {
	path := writeDaemonConfig(t, `
[work.daemon]
poll_interval = "2s"
`)

	oldCfgFile := cfgFile
	oldRun := supervisorRun
	defer func() {
		cfgFile = oldCfgFile
		supervisorRun = oldRun
	}()

	cfgFile = path
	var got time.Duration
	supervisorRun = func(d *drain.Deps, interval time.Duration, out io.Writer, sigCh <-chan os.Signal) error {
		got = interval
		return nil
	}

	if err := runWorkDaemon(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got != 2*time.Second {
		t.Fatalf("supervisor.Run interval = %s, want 2s", got)
	}
}

// TestWorkReadSurfacesThreadViewPreset pins ADR-0197 preset wiring: default
// surfaces get the configured default preset, and `--include-done` seeds the
// shipped all preset on both `pop work status` and `pop work dashboard`.
func TestWorkReadSurfacesThreadViewPreset(t *testing.T) {
	path := writeDaemonConfig(t, "")

	oldCfgFile := cfgFile
	oldLoad := workConfigLoad
	oldStatus := workBuildStatus
	oldTables := workBuildStatusTables
	oldDash := workRunDashboard
	oldStatusInc := workStatusIncludeDone
	oldDashInc := workDashboardIncludeDone
	oldStatusPreset := workStatusPreset
	oldDashPreset := workDashboardPreset
	defer func() {
		cfgFile = oldCfgFile
		workConfigLoad = oldLoad
		workBuildStatus = oldStatus
		workBuildStatusTables = oldTables
		workRunDashboard = oldDash
		workStatusIncludeDone = oldStatusInc
		workDashboardIncludeDone = oldDashInc
		workStatusPreset = oldStatusPreset
		workDashboardPreset = oldDashPreset
	}()

	setCmdLayerDeps(t, newTestCmdDeps(t, "", t.TempDir(), ""))

	cfgFile = path
	workConfigLoad = func(string) (*config.Config, error) { return &config.Config{}, nil }

	var statusPreset, dashPreset string
	workBuildStatus = func(d *drain.Deps, _ *config.Config) (drain.StatusSnapshot, error) {
		statusPreset = d.ViewPreset.Name
		return drain.StatusSnapshot{Tasks: cmdLayerDeps().tasksDeps()}, nil
	}
	// `pop work status` renders the dashboard pages' rows as its tables
	// (ADR-0121), so it builds them too; stub both to empty.
	workBuildStatusTables = func(d *drain.Deps, _ *config.Config) (dashboard.StatusTables, error) {
		return dashboard.StatusTables{}, nil
	}
	workRunDashboard = func(d *drain.Deps, _ *config.Config, _ string) (string, error) {
		dashPreset = d.ViewPreset.Name
		return "", nil
	}

	// Default: both surfaces use the configured default (shipped active).
	workStatusIncludeDone = false
	workDashboardIncludeDone = false
	workStatusPreset = ""
	if err := runWorkStatus(workStatusCmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := runWorkDashboard(workDashboardCmd, nil); err != nil {
		t.Fatal(err)
	}
	if statusPreset != "active" || dashPreset != "active" {
		t.Fatalf("default ViewPreset: status=%q dashboard=%q, want both active", statusPreset, dashPreset)
	}

	// --include-done: both surfaces seed the all preset.
	workStatusIncludeDone = true
	workDashboardIncludeDone = true
	if err := runWorkStatus(workStatusCmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := runWorkDashboard(workDashboardCmd, nil); err != nil {
		t.Fatal(err)
	}
	if statusPreset != "all" || dashPreset != "all" {
		t.Fatalf("--include-done ViewPreset: status=%q dashboard=%q, want both all", statusPreset, dashPreset)
	}

	// --preset unfolded on status, and on the dashboard (ADR-0197/0210: presets
	// are the shared view mechanism of both read surfaces).
	workStatusIncludeDone = false
	workDashboardIncludeDone = false
	workStatusPreset = "unfolded"
	workDashboardPreset = "unfolded"
	if err := runWorkStatus(workStatusCmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := runWorkDashboard(workDashboardCmd, nil); err != nil {
		t.Fatal(err)
	}
	if statusPreset != "unfolded" || dashPreset != "unfolded" {
		t.Fatalf("--preset unfolded ViewPreset: status=%q dashboard=%q, want both unfolded", statusPreset, dashPreset)
	}

	// `--preset muted` lists what the dashboard's muted preset lists (ADR-0200
	// decision 8). The two surfaces share one row selector, so equality of the
	// threaded preset with the entry the filter menu activates is equality of
	// the rows — there is no second definition anywhere for them to disagree on.
	workStatusPreset = "muted"
	if err := runWorkStatus(workStatusCmd, nil); err != nil {
		t.Fatal(err)
	}
	if statusPreset != "muted" {
		t.Fatalf("--preset muted ViewPreset = %q, want muted", statusPreset)
	}
	threaded, err := resolveWorkStatusPreset(&config.Config{}, "muted", false, io.Discard)
	if err != nil {
		t.Fatalf("resolveWorkStatusPreset(muted): %v", err)
	}
	fromRoster, ok := (&config.Config{}).WorkViewPresetNamed("muted")
	if !ok {
		t.Fatal("muted is not in the resolved roster the dashboard filter menu reads")
	}
	threaded.Number = fromRoster.Number
	if !reflect.DeepEqual(threaded, fromRoster) {
		t.Fatalf("--preset muted threads %#v, dashboard activates %#v", threaded, fromRoster)
	}
}

// TestWorkReadSurfacesRegisterIncludeDoneFlag confirms both Work read surfaces
// expose the `--include-done` flag, defaulting off.
func TestWorkReadSurfacesRegisterIncludeDoneFlag(t *testing.T) {
	t.Parallel()
	if f := workStatusCmd.Flags().Lookup("include-done"); f == nil {
		t.Fatal("work status missing --include-done flag")
	} else if f.DefValue != "false" {
		t.Fatalf("work status --include-done default = %q, want false", f.DefValue)
	}
	if f := workStatusCmd.Flags().Lookup("preset"); f == nil {
		t.Fatal("work status missing --preset flag")
	}
	if f := workDashboardCmd.Flags().Lookup("include-done"); f == nil {
		t.Fatal("work dashboard missing --include-done flag")
	} else if f.DefValue != "false" {
		t.Fatalf("work dashboard --include-done default = %q, want false", f.DefValue)
	}
	if f := workDashboardCmd.Flags().Lookup("preset"); f == nil {
		t.Fatal("work dashboard missing --preset flag")
	}
}

func TestWorkStatusUnknownPresetRefused(t *testing.T) {
	path := writeDaemonConfig(t, "")
	oldCfgFile := cfgFile
	oldLoad := workConfigLoad
	oldPreset := workStatusPreset
	oldInc := workStatusIncludeDone
	defer func() {
		cfgFile = oldCfgFile
		workConfigLoad = oldLoad
		workStatusPreset = oldPreset
		workStatusIncludeDone = oldInc
	}()
	cfgFile = path
	workConfigLoad = func(string) (*config.Config, error) { return &config.Config{}, nil }
	workStatusPreset = "nope"
	workStatusIncludeDone = false
	err := runWorkStatus(workStatusCmd, nil)
	if err == nil {
		t.Fatal("unknown preset must be refused")
	}
	if !strings.Contains(err.Error(), `unknown work view preset "nope"`) {
		t.Fatalf("error = %q, want unknown preset message", err)
	}
	for _, name := range []string{"active", "unfolded", "recent-7d", "recent-30d", "all"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error missing available preset %q: %v", name, err)
		}
	}
}

func TestWorkDaemonUsesShippedActivePreset(t *testing.T) {
	path := writeDaemonConfig(t, `
[work.daemon]
poll_interval = "2s"

[[work.dashboard.tasks.presets]]
name = "custom-default"
`)
	oldCfgFile := cfgFile
	oldRun := supervisorRun
	defer func() {
		cfgFile = oldCfgFile
		supervisorRun = oldRun
	}()
	cfgFile = path
	var got string
	supervisorRun = func(d *drain.Deps, interval time.Duration, out io.Writer, sigCh <-chan os.Signal) error {
		got = d.ViewPreset.Name
		return nil
	}
	if err := runWorkDaemon(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got != "active" {
		t.Fatalf("daemon ViewPreset = %q, want shipped active", got)
	}
}

// TestWorkStatusAndLogPrintTheirSurfaces drives the two read verbs end to end
// against an empty machine: status prints both its tables and log prints the
// journal, each through the command's own writer.
func TestWorkStatusAndLogPrintTheirSurfaces(t *testing.T) {
	oldCfgFile := cfgFile
	oldLoad := workConfigLoad
	defer func() {
		cfgFile = oldCfgFile
		workConfigLoad = oldLoad
	}()

	setCmdLayerDeps(t, newTestCmdDeps(t, "", t.TempDir(), ""))
	cfgFile = writeDaemonConfig(t, "")
	workConfigLoad = func(string) (*config.Config, error) { return &config.Config{}, nil }

	var statusOut bytes.Buffer
	workStatusCmd.SetOut(&statusOut)
	var logOut bytes.Buffer
	workLogCmd.SetOut(&logOut)
	t.Cleanup(func() {
		workStatusCmd.SetOut(nil)
		workLogCmd.SetOut(nil)
	})

	if err := runWorkStatus(workStatusCmd, nil); err != nil {
		t.Fatalf("work status: %v", err)
	}
	for _, want := range []string{"Summary:", "Task sets:", "Routines:"} {
		if !strings.Contains(statusOut.String(), want) {
			t.Fatalf("work status output missing %q:\n%s", want, statusOut.String())
		}
	}

	if err := runWorkLog(workLogCmd, nil); err != nil {
		t.Fatalf("work log: %v", err)
	}
	if strings.TrimSpace(logOut.String()) == "" {
		t.Fatal("work log printed nothing on an empty journal")
	}
}
