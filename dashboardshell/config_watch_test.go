package dashboardshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboard"
	"github.com/glebglazov/pop/tasks"
)

// Decision 5: a config change made outside the dashboard — `pop config` in
// another pane, a hand edit — reaches a running dashboard on the next poll, and
// an untouched config file costs a stat and nothing else.
func TestPollReReadsConfigWhenAFileChanged(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("# as the dashboard opened\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	reloads := 0
	edited := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Edited Agent", Cmd: "codex --model gpt"},
		}},
	}}
	s, err := newShell(PageWork, actionDeps(), &config.Config{}, cfgPath)
	if err != nil {
		t.Fatalf("newShell: %v", err)
	}
	s.reloadConfig = func() (*config.Config, error) {
		reloads++
		return edited, nil
	}
	updated, _ := s.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	s = updated.(Shell)

	want := tasks.FormatAttendedAgentStatus(tasks.EffectiveAttendedEntry(edited))
	if strings.Contains(s.View().Content, want) {
		t.Fatalf("the page already reports %q before the file changed", want)
	}

	s = poll(t, s)
	if reloads != 0 {
		t.Fatalf("re-read config %d times with the config file untouched", reloads)
	}

	if err := os.WriteFile(cfgPath, []byte("# edited from another pane\n"), 0o644); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	touch(t, cfgPath)

	s = poll(t, s)
	if reloads != 1 {
		t.Fatalf("re-read config %d times after the file changed, want once", reloads)
	}
	if view := s.View().Content; !strings.Contains(view, want) {
		t.Fatalf("page does not report the re-read config %q:\n%s", want, view)
	}

	// The change was consumed: polls that follow it stat and stop.
	s = poll(t, s)
	s = poll(t, s)
	if reloads != 1 {
		t.Fatalf("re-read config %d times over three polls, want the one the edit earned", reloads)
	}
}

// The override layer pop writes itself is watched too, so a key set with
// `pop config` in another pane lands without touching config.toml.
func TestPollReReadsConfigWhenTheOverrideLayerChanged(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("# untouched\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	reloads := 0
	s, err := newShell(PageWork, actionDeps(), &config.Config{}, cfgPath)
	if err != nil {
		t.Fatalf("newShell: %v", err)
	}
	s.reloadConfig = func() (*config.Config, error) {
		reloads++
		return &config.Config{}, nil
	}

	// The override file does not exist when the dashboard opens: it appearing is
	// itself the change.
	overridePath := config.DefaultOverrideConfigPath()
	if err := os.MkdirAll(filepath.Dir(overridePath), 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte("work.attended.agents = []\n"), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}

	s = poll(t, s)
	if reloads != 1 {
		t.Fatalf("re-read config %d times after the override layer appeared, want once", reloads)
	}
}

// poll drives one dashboard poll tick through the shell, which is where the
// config stat hangs.
func poll(t *testing.T, s Shell) Shell {
	t.Helper()
	updated, _ := s.Update(dashboard.PollTick(PageWork))
	return updated.(Shell)
}

// touch moves a file's mtime forward. Two writes inside one filesystem timestamp
// tick can leave the mtime unchanged, which would make the test prove nothing.
func touch(t *testing.T, path string) {
	t.Helper()
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}
