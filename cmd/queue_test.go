package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestResolveQueueRunConfigDefaultsAgent(t *testing.T) {
	path := writeQueueConfig(t, ``)

	got, err := resolveQueueRunConfig(config.Load, path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PollInterval != config.DefaultQueuePollInterval {
		t.Fatalf("poll interval = %s, want %s", got.PollInterval, config.DefaultQueuePollInterval)
	}
	if want := []string{"claude"}; !equalQueueStrings(got.Agents, want) {
		t.Fatalf("agents = %#v, want %#v", got.Agents, want)
	}
}

func TestResolveQueueRunConfigValidatesAgentPresets(t *testing.T) {
	path := writeQueueConfig(t, `
[queue]
agents = ["claude --model opus4.8", "codex", "opencode"]
poll_interval = "5s"
agent_quota_retry_after = "30m"
crash_retry_delays = ["1s", "2s"]
`)

	got, err := resolveQueueRunConfig(config.Load, path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"claude --model opus4.8", "codex", "opencode"}; !equalQueueStrings(got.Agents, want) {
		t.Fatalf("agents = %#v, want %#v", got.Agents, want)
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

func TestResolveQueueRunConfigRejectsUnknownAgentPreset(t *testing.T) {
	path := writeQueueConfig(t, `
[queue]
agents = ["missing-agent --flag"]
`)

	_, err := resolveQueueRunConfig(config.Load, path)
	if err == nil {
		t.Fatal("expected unknown agent error")
	}
	if !strings.Contains(err.Error(), `[queue] agents[0]`) || !strings.Contains(err.Error(), `unknown agent preset "missing-agent"`) {
		t.Fatalf("error = %q", err)
	}
}

func TestResolveQueueRunConfigDoesNotCheckAgentPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	path := writeQueueConfig(t, `
[queue]
agents = ["codex"]
`)

	if _, err := resolveQueueRunConfig(config.Load, path); err != nil {
		t.Fatalf("recognized preset must not fail just because its binary may be absent from PATH: %v", err)
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

func TestRunQueueAbandonInvokesQueueAbandon(t *testing.T) {
	path := writeQueueConfig(t, ``)

	oldCfgFile := cfgFile
	oldAbandon := queueAbandon
	oldYes := queueAbandonYes
	defer func() {
		cfgFile = oldCfgFile
		queueAbandon = oldAbandon
		queueAbandonYes = oldYes
	}()

	cfgFile = path
	queueAbandonYes = true
	var gotSet string
	var gotYes bool
	queueAbandon = func(d *queue.Deps, cfg *config.Config, setID string, out io.Writer, opts queue.AbandonOptions) (queue.AbandonResult, error) {
		gotSet = setID
		gotYes = opts.Yes
		if cfg == nil {
			t.Fatal("config was nil")
		}
		if opts.In == nil {
			t.Fatal("abandon input was nil")
		}
		return queue.AbandonResult{SetID: setID}, nil
	}

	if err := runQueueAbandon(nil, []string{"set-1"}); err != nil {
		t.Fatal(err)
	}
	if gotSet != "set-1" {
		t.Fatalf("setID = %q, want set-1", gotSet)
	}
	if !gotYes {
		t.Fatal("expected --yes to be forwarded")
	}
}

func equalQueueStrings(a, b []string) bool {
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
