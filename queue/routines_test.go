package queue

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/frontmatter"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// editRoutineBody rewrites a routine's prompt.md body while preserving its
// intent frontmatter (ADR-0139), mimicking a stray $EDITOR touch below the fence.
func editRoutineBody(t *testing.T, id, body string) {
	t.Helper()
	path := routinePromptPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fields, _, err := frontmatter.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}
	out, err := frontmatter.Marshal(fields, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

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
		FS:             td.FS,
		Now:            func() time.Time { return now },
		LoadConfig:     func() (*config.Config, error) { return &config.Config{}, nil },
		Tasks:          td,
		IsInteractive:  func() bool { return false },
		PID:            func() int { return 4242 },
		ProcStartToken: func(pid int) (string, bool) { return "test", true },
		ProcessAlive:   func(pid int, procStart string) bool { return pid == 9999 },
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

// routineTick drives the Routine kind through the supervisor's own phases —
// reconcile, the pure candidate read, then dispatch through the very function
// tick() dispatches with — so these tests exercise the seam rather than a
// rehearsal of it. It stands in for a whole tick where a test cares only about
// routines; TestSupervisorDrivesRoutinesThroughTheAdvanceSeam drives the real
// loop.
func routineTick(t *testing.T, qd *Deps, out io.Writer) {
	t.Helper()
	adv := qd.routineAdvancer()
	_ = adv.Reconcile()
	candidates, err := adv.Candidates()
	if err != nil {
		fmt.Fprintln(out, err.Error())
		return
	}
	pass := kindPass{kind: ref.KindRoutine, adv: adv}
	occupancy := newCheckoutOccupancy(qd)
	for _, c := range candidates {
		if line := dispatch(pass, c, occupancy).Line(); line != "" {
			fmt.Fprintln(out, line)
		}
	}
}

func routineCandidates(t *testing.T, qd *Deps) []work.Candidate {
	t.Helper()
	candidates, err := qd.routineAdvancer().Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	return candidates
}

func TestRoutineTickSpawnsDueEveryAndDaily(t *testing.T) {
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
	routineTick(t, qd, &out)

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

func TestRoutineTickSkipsPaused(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "paused", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.PauseWith(rd, "paused"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	routineTick(t, qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "paused"); ok {
		t.Fatalf("paused routine must not spawn, commands=%v", rt.commands)
	}
}

func TestRoutineTickSkipsUnscheduled(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	// An unscheduled routine is durable manual-fire-only (ADR-0134): the daemon
	// never fires it even when resumed and anchored by a prior manual fire.
	if _, err := routine.AddWith(rd, "manual-only", "", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "manual-only"); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "pop.db"), func(int, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "manual-only",
		FiredAt:   now.Add(-24 * time.Hour),
		PID:       1,
		ProcStart: "dead",
	}, func(store.RoutineRun) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(1, store.RoutineRunSucceeded, "", "", now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	var out bytes.Buffer
	routineTick(t, qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "manual-only"); ok {
		t.Fatalf("unscheduled routine must not spawn, commands=%v", rt.commands)
	}
}

func TestRoutineTickSkipsOverlapAndJournals(t *testing.T) {
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
	routineTick(t, qd, &out)
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

func TestRoutineTickCatchUpOnceAfterMissedSlots(t *testing.T) {
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
	routineTick(t, qd, &out)

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

func TestRoutineTickWarnsBrokenAndFiresHealthy(t *testing.T) {
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

	// Corrupt the broken routine's state.json so it cannot be loaded (ADR-0139:
	// state.json is the read half; manifest.json is never read).
	brokenState := filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "routines", "broken", "state.json")
	if err := os.WriteFile(brokenState, []byte("{ not json"), 0o644); err != nil {
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
	routineTick(t, qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "hourly"); !ok {
		t.Fatalf("expected spawn for healthy routine, commands=%v", rt.commands)
	}
	if !strings.Contains(out.String(), "broken") || !strings.Contains(out.String(), "load failed") {
		t.Fatalf("output missing broken-routine warning:\n%s", out.String())
	}
}

// TestRoutineTickSurvivesUnparseableScheduleFrontmatter proves that an
// unparseable schedule in a routine's frontmatter suspends only that routine
// with a warning; the daemon survives and still fires healthy siblings, never
// treating the broken one as silently unscheduled (ADR-0139).
func TestRoutineTickSurvivesUnparseableScheduleFrontmatter(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)

	if _, err := routine.AddWith(rd, "hourly", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.AddWith(rd, "bad-sched", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "hourly"); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "bad-sched"); err != nil {
		t.Fatal(err)
	}
	// Valid YAML, but a schedule the parser rejects — this must warn, not silently
	// pass through as unscheduled.
	if err := os.WriteFile(routinePromptPath("bad-sched"), []byte("---\nschedule: every week\n---\nbody\n"), 0o644); err != nil {
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
	routineTick(t, qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "hourly"); !ok {
		t.Fatalf("expected spawn for healthy routine, commands=%v", rt.commands)
	}
	if _, ok := extractRoutineSpawnCommand(rt, "bad-sched"); ok {
		t.Fatalf("routine with an unparseable schedule must not fire, commands=%v", rt.commands)
	}
	if !strings.Contains(out.String(), "bad-sched") || !strings.Contains(out.String(), "load failed") {
		t.Fatalf("output missing warning for the unparseable-schedule routine:\n%s", out.String())
	}
}

// recordRoutineRun inserts a finished, non-skipped run carrying fingerprint so
// tests can seed the daemon's "last non-skipped run" comparison point.
func recordRoutineRun(t *testing.T, id, fingerprint string, firedAt time.Time) {
	t.Helper()
	s, err := store.Open(filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "pop.db"), func(int, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID:   id,
		FiredAt:     firedAt,
		PID:         1,
		ProcStart:   "dead",
		Fingerprint: fingerprint,
	}, func(store.RoutineRun) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(run.ID, store.RoutineRunSucceeded, "", "", firedAt); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
}

func routinePromptPath(id string) string {
	return filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "routines", id, "prompt.md")
}

func loadRoutineForTest(t *testing.T, rd *routine.Deps, id string) *routine.Routine {
	t.Helper()
	routines, _, err := routine.ListRoutines(rd)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range routines {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("routine %q not found", id)
	return nil
}

func TestRoutineTickFiresWhenFingerprintMatches(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "match", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "match"); err != nil {
		t.Fatal(err)
	}
	fp, err := routine.Fingerprint(rd, loadRoutineForTest(t, rd, "match"))
	if err != nil {
		t.Fatal(err)
	}
	recordRoutineRun(t, "match", fp, now.Add(-2*time.Hour))

	var out bytes.Buffer
	routineTick(t, qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "match"); !ok {
		t.Fatalf("expected spawn when fingerprint matches, commands=%v", rt.commands)
	}
}

func TestRoutineTickPausesChangedOnFingerprintDrift(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "drift", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "drift"); err != nil {
		t.Fatal(err)
	}
	fp, err := routine.Fingerprint(rd, loadRoutineForTest(t, rd, "drift"))
	if err != nil {
		t.Fatal(err)
	}
	recordRoutineRun(t, "drift", fp, now.Add(-2*time.Hour))

	// A direct prompt.md body edit no chokepoint saw moves the fingerprint.
	editRoutineBody(t, "drift", "edited by a stray $EDITOR\n")

	var out bytes.Buffer
	routineTick(t, qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "drift"); ok {
		t.Fatalf("drifted routine must not fire, commands=%v", rt.commands)
	}
	if !strings.Contains(out.String(), "paused (changed)") {
		t.Fatalf("output missing changed-pause line:\n%s", out.String())
	}
	got := loadRoutineForTest(t, rd, "drift")
	if !got.Manifest.Paused || got.Manifest.PauseReason != routine.PauseReasonChanged {
		t.Fatalf("manifest = {paused:%v reason:%q}, want paused with reason changed", got.Manifest.Paused, got.Manifest.PauseReason)
	}

	// The pause is also a skipped fire, and a skipped fire belongs in the journal
	// beside the drains: one journal covers everything the daemon did.
	events, err := BuildLog(qd.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	var logOut bytes.Buffer
	RenderLog(&logOut, events, 50)
	if !strings.Contains(logOut.String(), "drift skipped "+routine.SkipReasonChanged) {
		t.Fatalf("journal missing the fingerprint-pause skip:\n%s", logOut.String())
	}

	// The pause is what keeps it to one: the next tick stops at the pause bit.
	var second bytes.Buffer
	routineTick(t, qd, &second)
	events, err = BuildLog(qd.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	skips := 0
	for _, ev := range events {
		if ev.SetID == "drift" && ev.Kind == "skipped" {
			skips++
		}
	}
	if skips != 1 {
		t.Fatalf("journal carries %d drift skips, want exactly one per drift", skips)
	}
}

func TestRoutineTickFiresWhenNoRecordedFingerprint(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "premig", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "premig"); err != nil {
		t.Fatal(err)
	}
	// Pre-migration row: empty fingerprint. The current fingerprint is non-empty
	// (a real prompt.md exists), but an empty last must never be a mismatch.
	recordRoutineRun(t, "premig", "", now.Add(-2*time.Hour))
	editRoutineBody(t, "premig", "changed after the run\n")

	var out bytes.Buffer
	routineTick(t, qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "premig"); !ok {
		t.Fatalf("routine with no recorded fingerprint must still fire, commands=%v", rt.commands)
	}
	if strings.Contains(out.String(), "paused (changed)") {
		t.Fatalf("pre-migration row must not pause:\n%s", out.String())
	}
}

func TestRoutineTickMissingStoreFiresNoneAndCreatesNoDatabase(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "hourly", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "hourly"); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "pop.db")
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("pop.db must not exist before tick, stat err = %v", err)
	}

	var out bytes.Buffer
	routineTick(t, qd, &out)

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "hourly"); ok {
		t.Fatalf("routine must not fire without a store, commands=%v", rt.commands)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("a routine tick must not materialise pop.db when it is missing, stat err = %v", err)
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

// TestSupervisorDrivesRoutinesThroughTheAdvanceSeam pins the wiring the slice
// exists for: a supervisor tick — not a routine-only pass beside it — reconciles
// and fires Routines, and it does so through the advance seam rather than an
// inline pipeline of its own.
func TestSupervisorDrivesRoutinesThroughTheAdvanceSeam(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "nightly", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "nightly"); err != nil {
		t.Fatal(err)
	}
	recordRoutineRun(t, "nightly", "", now.Add(-2*time.Hour))

	advanced := false
	for _, adv := range qd.advancers(&config.Config{}) {
		if advancerKindID(adv) == ref.KindRoutine {
			advanced = true
		}
	}
	if !advanced {
		t.Fatal("the supervisor's advancer list carries no routine kind")
	}

	var out bytes.Buffer
	tick(qd, &out, newRunOutputState())

	rt := qd.Tmux.(*recordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "nightly"); !ok {
		t.Fatalf("a tick must fire a due routine, commands=%v", rt.commands)
	}
	if !strings.Contains(out.String(), "queue: routine nightly: spawned fire") {
		t.Fatalf("tick output missing the routine's fire line:\n%s", out.String())
	}
}

// TestRoutineCandidateIsContainerLevelAndOccupiesNoCheckout pins the two facts
// the seam needs from a Routine candidate: its ref names the container and not a
// run — a candidate exists before a run does, and the run id is minted by the
// fire — and it claims no checkout, so a read-only fire is never queued behind a
// drain.
func TestRoutineCandidateIsContainerLevelAndOccupiesNoCheckout(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "audit", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "audit"); err != nil {
		t.Fatal(err)
	}
	recordRoutineRun(t, "audit", "", now.Add(-2*time.Hour))

	candidates := routineCandidates(t, qd)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want the one due routine", candidates)
	}
	got := candidates[0]
	if got.Ref != (ref.WorkRef{Kind: ref.KindRoutine, ContainerID: "audit"}) {
		t.Fatalf("candidate ref = %s, want routine:audit", got.Ref)
	}
	if got.Ref.IsItem() {
		t.Fatalf("candidate ref %s names an item; the run id is minted by the fire", got.Ref)
	}
	if got.Checkout != "" {
		t.Fatalf("candidate checkout = %q, want none: a fire occupies no checkout", got.Checkout)
	}
	if reason := newCheckoutOccupancy(qd).refusal(got); reason != "" {
		t.Fatalf("occupancy refused a routine: %q", reason)
	}
}

// TestRoutineCandidatesWriteNothingAndDispatchDoes is the purity guard on the
// half of the pass that moved: the candidate read touches nothing on disk, and
// the write the overlap refusal carries happens at dispatch — which is also what
// proves the snapshot can see a write at all.
func TestRoutineCandidatesWriteNothingAndDispatchDoes(t *testing.T) {
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
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "busy",
		FiredAt:   now.Add(-90 * time.Minute),
		PID:       9999,
		ProcStart: "live",
	}, func(store.RoutineRun) bool { return true }); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	dataDir := filepath.Dir(tasks.DrainStorePathWith(qd.Tasks))
	adv := qd.routineAdvancer()
	// Settle the data dir: the first read opens the store and creates its sidecar
	// files, which is a one-off rather than a write the read performs.
	if _, err := adv.Candidates(); err != nil {
		t.Fatalf("Candidates: %v", err)
	}

	before := dataDirSnapshot(t, dataDir)
	candidates, err := adv.Candidates()
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	assertSameSnapshot(t, "Candidates", before, dataDirSnapshot(t, dataDir))

	if len(candidates) != 1 || !candidates[0].Refused() {
		t.Fatalf("candidates = %+v, want one overlap refusal", candidates)
	}
	if candidates[0].Verdict.Reason != routine.SkipReasonOverlap {
		t.Fatalf("refusal reason = %q, want %q", candidates[0].Verdict.Reason, routine.SkipReasonOverlap)
	}
	if _, err := adv.Advance(candidates[0]); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if sameSnapshot(before, dataDirSnapshot(t, dataDir)) {
		t.Fatal("dispatching the overlap refusal wrote nothing; the skipped run must be recorded kind-side at dispatch")
	}
}

// TestRoutineCandidatesAreGlobalAndCwdIndependent pins that the advance seam has
// no scope: the same Routines are candidates wherever the daemon is standing,
// and a Project routine — discovered live from the checkout it is committed in —
// is never one of them, because carrying no schedule it can never consent.
func TestRoutineCandidatesAreGlobalAndCwdIndependent(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "global", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "global"); err != nil {
		t.Fatal(err)
	}
	recordRoutineRun(t, "global", "", now.Add(-2*time.Hour))

	checkout := t.TempDir()
	spawnInitGitRepo(t, checkout)
	if err := os.MkdirAll(filepath.Join(checkout, ".pop", "routines"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".pop", "routines", "committed.md"), []byte("a committed prompt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rd.Project = cwdPinnedProject(checkout)
	if found, _ := routine.DiscoverProjectRoutines(rd); len(found) != 1 {
		t.Fatalf("fixture: the checkout's Project routine is not discoverable, found=%+v", found)
	}

	elsewhere := labelsOf(routineCandidates(t, qd))
	qd.Project = cwdPinnedProject(checkout)
	inCheckout := labelsOf(routineCandidates(t, qd))

	if strings.Join(elsewhere, ",") != strings.Join(inCheckout, ",") {
		t.Fatalf("candidates moved with the cwd: %v outside the checkout, %v inside it", elsewhere, inCheckout)
	}
	for _, label := range inCheckout {
		if strings.Contains(label, "committed") {
			t.Fatalf("a Project routine became a candidate: %v", inCheckout)
		}
	}
	if strings.Join(inCheckout, ",") != "routine global" {
		t.Fatalf("candidates = %v, want the one due authored routine", inCheckout)
	}
}

// cwdPinnedProject returns project deps that report dir as the working
// directory, so a test can read candidates from "inside" a checkout without
// moving the process.
func cwdPinnedProject(dir string) *project.Deps {
	pd := project.DefaultDeps()
	pd.FS = &deps.MockFileSystem{GetwdFunc: func() (string, error) { return dir, nil }}
	return pd
}

func labelsOf(candidates []work.Candidate) []string {
	labels := make([]string, 0, len(candidates))
	for _, c := range candidates {
		labels = append(labels, c.Label)
	}
	return labels
}
