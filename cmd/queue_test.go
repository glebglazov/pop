package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

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
