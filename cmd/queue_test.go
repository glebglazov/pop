package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/queue"
)

func writeQueueConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveQueueRunConfigDefaults(t *testing.T) {
	path := writeQueueConfig(t, ``)

	got, err := resolveQueueRunConfig(config.Load, path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PollInterval != config.DefaultQueuePollInterval {
		t.Fatalf("poll interval = %s, want %s", got.PollInterval, config.DefaultQueuePollInterval)
	}
}

func TestResolveQueueRunConfigIgnoresQueueAgents(t *testing.T) {
	path := writeQueueConfig(t, `
[queue]
agents = ["missing-agent --flag"]
poll_interval = "5s"
agent_quota_retry_after = "30m"
crash_retry_delays = ["1s", "2s"]
`)

	got, err := resolveQueueRunConfig(config.Load, path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PollInterval != 5*time.Second {
		t.Fatalf("poll interval = %s, want 5s", got.PollInterval)
	}
	if got.AgentQuotaRetryAfter != 30*time.Minute {
		t.Fatalf("quota retry = %s, want 30m", got.AgentQuotaRetryAfter)
	}
	if want := []time.Duration{time.Second, 2 * time.Second}; !equalQueueDurations(got.CrashRetryDelays, want) {
		t.Fatalf("crash retry delays = %#v, want %#v", got.CrashRetryDelays, want)
	}
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

func equalQueueDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
