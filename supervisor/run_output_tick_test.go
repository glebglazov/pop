package supervisor

import (
	"bytes"
	"github.com/glebglazov/pop/tasks"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks/drain"
)

func TestRunOutputBaselineOnceAndQuietTick(t *testing.T) {
	repo := t.TempDir()
	queuetest.InitGitRepo(t, repo)
	xdg := filepath.Join(repo, ".xdg")
	t.Setenv("XDG_DATA_HOME", xdg)

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	td := queuetest.TasksDeps(t, true)
	d := &drain.Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       queuetest.NewRecordingTmux(false, "0"),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:   func(runtimePath string) *tasks.RuntimeLockStatus { return queuetest.IdleLock(runtimePath) },
		Refresh:    func(defPath string) (*tasks.RefreshResult, error) { return &tasks.RefreshResult{}, nil },
	}

	var out bytes.Buffer
	runOut := newRunOutputState()

	tick(d, &out, runOut)
	first := out.String()
	if !strings.Contains(first, "Picked-up sets:") {
		t.Fatalf("first tick must print baseline:\n%s", first)
	}
	if !strings.Contains(first, "1 other project: no ready work") {
		t.Fatalf("first tick baseline missing collapsed idle count:\n%s", first)
	}
	if strings.Contains(first, "Daemon state:") {
		t.Fatalf("baseline must not dump daemon JSON:\n%s", first)
	}

	tick(d, &out, runOut)
	second := out.String()[len(first):]
	if strings.TrimSpace(second) != "" {
		t.Fatalf("quiet second tick must print nothing, got:\n%q", second)
	}
}

func TestRunOutputSpawnDelta(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "spawn-set", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := queuetest.NewRecordingTmux(false, "0")
	td := queuetest.TasksDeps(t, true)
	d := &drain.Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	bindSetInPlace(t, d, repo, setID)

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())

	want := "spawned drain for " + setID
	if !strings.Contains(out.String(), want) {
		t.Fatalf("spawn tick must emit delta %q:\n%s", want, out.String())
	}
}
