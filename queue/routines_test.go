package queue

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

func routineTickDeps(t *testing.T, now time.Time) (*Deps, *routine.Deps, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	real := deps.NewRealFileSystem()
	td := tasks.DefaultDeps()
	td.ProcessAlive = func(pid int) bool { return pid == 9999 }
	td.ProcessStartToken = func(pid int) (string, bool) {
		if pid == 9999 {
			return "live", true
		}
		return "", false
	}
	td.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
			}
			return ""
		},
		ReadFileFunc:  real.ReadFile,
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		RemoveAllFunc: real.RemoveAll,
		StatFunc:      real.Stat,
		ReadDirFunc:   real.ReadDir,
		UserHomeDirFunc: func() (string, error) {
			return filepath.Join(dir, "home"), nil
		},
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	rd := &routine.Deps{
		FS:            td.FS,
		Now:           func() time.Time { return now },
		LoadConfig:    func() (*config.Config, error) { return &config.Config{}, nil },
		Tasks:         td,
		IsInteractive: func() bool { return false },
		PID:           func() int { return 4242 },
		ProcStartToken: func(pid int) (string, bool) { return "test", true },
		ProcessAlive:  func(pid int, procStart string) bool { return pid == 9999 },
	}
	qd := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       newRecordingTmux(false, "0"),
		LoadConfig: func(string) (*config.Config, error) { return &config.Config{}, nil },
		Now:        func() time.Time { return now },
	}
	return qd, rd, home
}

func TestTickRoutinesSpawnsDueEveryAndDaily(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)

	if _, err := routine.AddWith(rd, "hourly", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.AddWith(rd, "morning", "daily at 10:00", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "hourly"); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "morning"); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "pop.db"), func(int, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "hourly",
		FiredAt:   now.Add(-2 * time.Hour),
		PID:       1,
		ProcStart: "dead",
	}, func(store.RoutineRun) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(1, store.RoutineRunSucceeded, "", "", now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "morning",
		FiredAt:   time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC),
		PID:       2,
		ProcStart: "dead",
	}, func(store.RoutineRun) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(2, store.RoutineRunSucceeded, "", "", time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	tickRoutines(qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	for _, id := range []string{"hourly", "morning"} {
		cmd, ok := extractRoutineSpawnCommand(rt, id)
		if !ok {
			t.Fatalf("expected spawn for %s, commands=%v", id, rt.commands)
		}
		if !strings.Contains(cmd, "pop routine fire "+id) {
			t.Fatalf("spawn for %s = %q", id, cmd)
		}
	}
	if !strings.Contains(out.String(), "spawned fire") {
		t.Fatalf("output missing spawn lines:\n%s", out.String())
	}
}

func TestTickRoutinesSkipsPaused(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "paused", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.PauseWith(rd, "paused"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	tickRoutines(qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "paused"); ok {
		t.Fatalf("paused routine must not spawn, commands=%v", rt.commands)
	}
}

func TestTickRoutinesSkipsOverlapAndJournals(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "busy", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "busy"); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "pop.db"), func(int, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.StartRoutineRun(store.RoutineRun{
		RoutineID: "busy",
		FiredAt:   now.Add(-90 * time.Minute),
		PID:       9999,
		ProcStart: "live",
	}, func(store.RoutineRun) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	tickRoutines(qd, &out)
	if !strings.Contains(out.String(), "skipped fire") || !strings.Contains(out.String(), routine.SkipReasonOverlap) {
		t.Fatalf("output = %q", out.String())
	}

	events, err := BuildLog(qd.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	var logOut bytes.Buffer
	RenderLog(&logOut, events, 50)
	if !strings.Contains(logOut.String(), "busy skipped "+routine.SkipReasonOverlap) {
		t.Fatalf("journal missing skip:\n%s", logOut.String())
	}
}

func TestTickRoutinesCatchUpOnceAfterMissedSlots(t *testing.T) {
	now := time.Date(2026, 7, 18, 15, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "catchup", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "catchup"); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "pop.db"), func(int, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	last := now.Add(-4 * time.Hour)
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "catchup",
		FiredAt:   last,
		PID:       1,
		ProcStart: "dead",
	}, func(store.RoutineRun) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(1, store.RoutineRunSucceeded, "", "", last); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	tickRoutines(qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	spawnCount := 0
	for _, cmd := range rt.commands {
		if len(cmd) > 0 && cmd[0] == "send-keys" && strings.Contains(strings.Join(cmd, " "), "pop routine fire catchup") {
			spawnCount++
		}
	}
	if spawnCount != 1 {
		t.Fatalf("want exactly one catch-up spawn, got %d", spawnCount)
	}
}

func TestTickRoutinesWarnsBrokenAndFiresHealthy(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)

	if _, err := routine.AddWith(rd, "hourly", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.AddWith(rd, "broken", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "hourly"); err != nil {
		t.Fatal(err)
	}

	// Corrupt the broken routine's manifest so it cannot be loaded.
	brokenManifest := filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "routines", "broken", "manifest.json")
	if err := os.WriteFile(brokenManifest, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "pop.db"), func(int, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "hourly",
		FiredAt:   now.Add(-2 * time.Hour),
		PID:       1,
		ProcStart: "dead",
	}, func(store.RoutineRun) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(1, store.RoutineRunSucceeded, "", "", now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	tickRoutines(qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "hourly"); !ok {
		t.Fatalf("expected spawn for healthy routine, commands=%v", rt.commands)
	}
	if !strings.Contains(out.String(), "broken") || !strings.Contains(out.String(), "manifest load failed") {
		t.Fatalf("output missing broken-routine warning:\n%s", out.String())
	}
}

func TestRoutineSessionUsesRoutinesForNonGitDirectory(t *testing.T) {
	_, rd, home := routineTickDeps(t, time.Now())
	session, dir := routine.SessionAndDir(rd, home)
	if session != routine.RoutinesSessionName {
		t.Fatalf("session = %q, want %q", session, routine.RoutinesSessionName)
	}
	if dir != home {
		t.Fatalf("dir = %q, want %q", dir, home)
	}
}

func extractRoutineSpawnCommand(rt *recordingTmux, routineID string) (string, bool) {
	want := "pop routine fire " + routineID
	for _, cmd := range rt.commands {
		if len(cmd) < 2 || cmd[0] != "send-keys" {
			continue
		}
		line := strings.Join(cmd, " ")
		if strings.Contains(line, want) {
			return line, true
		}
	}
	return "", false
}
