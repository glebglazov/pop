package queue

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

func TestRunTeesSupervisorNarrationToDurableLog(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, ".xdg")
	t.Setenv("XDG_DATA_HOME", dataHome)

	td := queueTestTasksDeps(t, true)
	d := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       newRecordingTmux(false, "0"),
		LoadConfig: func(string) (*config.Config, error) { return &config.Config{}, nil },
	}
	sigCh := make(chan os.Signal, 1)
	sigCh <- os.Interrupt

	var stdout bytes.Buffer
	if err := Run(d, time.Hour, &stdout, sigCh); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := "pop queue supervisor started"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
	}
	logPath := SupervisorLogPath(td)
	if !strings.HasPrefix(logPath, filepath.Join(dataHome, "pop", "queue")+string(filepath.Separator)) {
		t.Fatalf("supervisor log path %q is not under queue data dir %q", logPath, QueueDataDir(td))
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read supervisor log: %v", err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("durable supervisor log missing %q:\n%s", want, string(data))
	}
}

func TestRotatingSupervisorLogBoundsActiveFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	td := queueTestTasksDeps(t, true)

	log, err := newRotatingSupervisorLog(td, 10, 2)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}
	for _, line := range []string{"first\n", "second\n", "third\n"} {
		if _, err := log.Write([]byte(line)); err != nil {
			t.Fatalf("write %q: %v", line, err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	active, err := os.ReadFile(SupervisorLogPath(td))
	if err != nil {
		t.Fatalf("read active log: %v", err)
	}
	if string(active) != "third\n" {
		t.Fatalf("active log = %q, want latest generation", string(active))
	}
	previous, err := os.ReadFile(rotatedSupervisorLogPath(SupervisorLogPath(td), 1))
	if err != nil {
		t.Fatalf("read first rotated log: %v", err)
	}
	if string(previous) != "second\n" {
		t.Fatalf("first rotated log = %q, want previous generation", string(previous))
	}
	oldest, err := os.ReadFile(rotatedSupervisorLogPath(SupervisorLogPath(td), 2))
	if err != nil {
		t.Fatalf("read second rotated log: %v", err)
	}
	if string(oldest) != "first\n" {
		t.Fatalf("second rotated log = %q, want oldest retained generation", string(oldest))
	}
}
