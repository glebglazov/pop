package cmd

// Daemon-verb tests that stub cfgFile or package globals stay serial (ADR-0145).

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
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
[queue]
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

// TestWorkReadSurfacesThreadIncludeDone pins the ADR-0121 Done-inclusion flag
// wiring: `--include-done` on both `pop work status` and `pop work dashboard`
// sets the single inclusion flag (drain.Deps.IncludeDone) the shared row layer
// reads, and it defaults off (DONE hidden).
func TestWorkReadSurfacesThreadIncludeDone(t *testing.T) {
	path := writeDaemonConfig(t, "")

	oldCfgFile := cfgFile
	oldLoad := queueConfigLoad
	oldStatus := workBuildStatus
	oldTables := workBuildStatusTables
	oldDash := workRunDashboard
	oldStatusInc := workStatusIncludeDone
	oldDashInc := workDashboardIncludeDone
	defer func() {
		cfgFile = oldCfgFile
		queueConfigLoad = oldLoad
		workBuildStatus = oldStatus
		workBuildStatusTables = oldTables
		workRunDashboard = oldDash
		workStatusIncludeDone = oldStatusInc
		workDashboardIncludeDone = oldDashInc
	}()

	setCmdLayerDeps(t, newTestCmdDeps(t, "", t.TempDir(), ""))

	cfgFile = path
	queueConfigLoad = func(string) (*config.Config, error) { return &config.Config{}, nil }

	var statusInclude, dashInclude bool
	workBuildStatus = func(d *drain.Deps, _ *config.Config) (drain.StatusSnapshot, error) {
		statusInclude = d.IncludeDone
		return drain.StatusSnapshot{Tasks: cmdLayerDeps().tasksDeps()}, nil
	}
	// `pop work status` renders the dashboard pages' rows as its tables
	// (ADR-0121), so it builds them too; stub both to empty.
	workBuildStatusTables = func(d *drain.Deps, _ *config.Config) (dashboard.StatusTables, error) {
		return dashboard.StatusTables{}, nil
	}
	workRunDashboard = func(d *drain.Deps, _ *config.Config) (string, error) {
		dashInclude = d.IncludeDone
		return "", nil
	}

	// Default: both surfaces hide DONE.
	workStatusIncludeDone = false
	workDashboardIncludeDone = false
	if err := runWorkStatus(workStatusCmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := runWorkDashboard(workDashboardCmd, nil); err != nil {
		t.Fatal(err)
	}
	if statusInclude || dashInclude {
		t.Fatalf("default IncludeDone: status=%v dashboard=%v, want both false", statusInclude, dashInclude)
	}

	// --include-done: both surfaces set the inclusion flag.
	workStatusIncludeDone = true
	workDashboardIncludeDone = true
	if err := runWorkStatus(workStatusCmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := runWorkDashboard(workDashboardCmd, nil); err != nil {
		t.Fatal(err)
	}
	if !statusInclude || !dashInclude {
		t.Fatalf("--include-done IncludeDone: status=%v dashboard=%v, want both true", statusInclude, dashInclude)
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
	if f := workDashboardCmd.Flags().Lookup("include-done"); f == nil {
		t.Fatal("work dashboard missing --include-done flag")
	} else if f.DefValue != "false" {
		t.Fatalf("work dashboard --include-done default = %q, want false", f.DefValue)
	}
}

// TestWorkStatusAndLogPrintTheirSurfaces drives the two read verbs end to end
// against an empty machine: status prints both its tables and log prints the
// journal, each through the command's own writer.
func TestWorkStatusAndLogPrintTheirSurfaces(t *testing.T) {
	oldCfgFile := cfgFile
	oldLoad := queueConfigLoad
	defer func() {
		cfgFile = oldCfgFile
		queueConfigLoad = oldLoad
	}()

	setCmdLayerDeps(t, newTestCmdDeps(t, "", t.TempDir(), ""))
	cfgFile = writeDaemonConfig(t, "")
	queueConfigLoad = func(string) (*config.Config, error) { return &config.Config{}, nil }

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
