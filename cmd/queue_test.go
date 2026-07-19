package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/queue"
	"github.com/glebglazov/pop/tasks"
)

func writeQueueConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunQueueRunHonorsConfiguredPollInterval(t *testing.T) {
	path := writeQueueConfig(t, `
[queue]
poll_interval = "2s"
`)

	oldCfgFile := cfgFile
	oldRun := queueRun
	defer func() {
		cfgFile = oldCfgFile
		queueRun = oldRun
	}()

	cfgFile = path
	var got time.Duration
	queueRun = func(d *queue.Deps, interval time.Duration, out io.Writer, sigCh <-chan os.Signal) error {
		got = interval
		return nil
	}

	if err := runQueueRun(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got != 2*time.Second {
		t.Fatalf("queue.Run interval = %s, want 2s", got)
	}
}

// TestQueueReadSurfacesThreadIncludeDone pins the ADR-0121 Done-inclusion flag
// wiring: `--include-done` on both `pop queue status` and `pop work dashboard`
// sets the single inclusion flag (queue.Deps.IncludeDone) the shared row layer
// reads, and it defaults off (DONE hidden).
func TestQueueReadSurfacesThreadIncludeDone(t *testing.T) {
	path := writeQueueConfig(t, "")

	oldCfgFile := cfgFile
	oldLoad := queueConfigLoad
	oldStatus := queueBuildStatus
	oldBuildDash := queueBuildDashboard
	oldDash := queueRunDashboard
	oldStatusInc := queueStatusIncludeDone
	oldDashInc := workDashboardIncludeDone
	defer func() {
		cfgFile = oldCfgFile
		queueConfigLoad = oldLoad
		queueBuildStatus = oldStatus
		queueBuildDashboard = oldBuildDash
		queueRunDashboard = oldDash
		queueStatusIncludeDone = oldStatusInc
		workDashboardIncludeDone = oldDashInc
	}()

	// RenderStatus reads bindings off the snapshot's Tasks deps; point it at an
	// empty temp data dir so the render path stays panic-free.
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfgFile = path
	queueConfigLoad = func(string) (*config.Config, error) { return &config.Config{}, nil }

	var statusInclude, dashInclude bool
	queueBuildStatus = func(d *queue.Deps, _ *config.Config) (queue.StatusSnapshot, error) {
		statusInclude = d.IncludeDone
		return queue.StatusSnapshot{Tasks: tasks.DefaultDeps()}, nil
	}
	// `pop queue status` renders the dashboard's rows as its table (ADR-0121), so
	// it builds the dashboard too; stub it to an empty snapshot.
	queueBuildDashboard = func(d *queue.Deps, _ *config.Config) (queue.DashboardSnapshot, error) {
		return queue.DashboardSnapshot{}, nil
	}
	queueRunDashboard = func(d *queue.Deps, _ *config.Config) (string, error) {
		dashInclude = d.IncludeDone
		return "", nil
	}

	// Default: both surfaces hide DONE.
	queueStatusIncludeDone = false
	workDashboardIncludeDone = false
	if err := runQueueStatus(queueStatusCmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := runWorkDashboard(workDashboardCmd, nil); err != nil {
		t.Fatal(err)
	}
	if statusInclude || dashInclude {
		t.Fatalf("default IncludeDone: status=%v dashboard=%v, want both false", statusInclude, dashInclude)
	}

	// --include-done: both surfaces set the inclusion flag.
	queueStatusIncludeDone = true
	workDashboardIncludeDone = true
	if err := runQueueStatus(queueStatusCmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := runWorkDashboard(workDashboardCmd, nil); err != nil {
		t.Fatal(err)
	}
	if !statusInclude || !dashInclude {
		t.Fatalf("--include-done IncludeDone: status=%v dashboard=%v, want both true", statusInclude, dashInclude)
	}
}

// TestQueueReadSurfacesRegisterIncludeDoneFlag confirms both Queue read
// surfaces expose the `--include-done` flag, defaulting off.
func TestQueueReadSurfacesRegisterIncludeDoneFlag(t *testing.T) {
	if f := queueStatusCmd.Flags().Lookup("include-done"); f == nil {
		t.Fatal("queue status missing --include-done flag")
	} else if f.DefValue != "false" {
		t.Fatalf("queue status --include-done default = %q, want false", f.DefValue)
	}
	if f := workDashboardCmd.Flags().Lookup("include-done"); f == nil {
		t.Fatal("work dashboard missing --include-done flag")
	} else if f.DefValue != "false" {
		t.Fatalf("work dashboard --include-done default = %q, want false", f.DefValue)
	}
	if f := queueDashboardCmd.Flags().Lookup("include-done"); f == nil {
		t.Fatal("queue dashboard alias missing --include-done flag")
	} else if f.DefValue != "false" {
		t.Fatalf("queue dashboard alias --include-done default = %q, want false", f.DefValue)
	}
}
