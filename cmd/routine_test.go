package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/routine"
)

func TestRoutineCommandTree(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path []string
	}{
		{path: []string{"routine", "new"}},
		{path: []string{"routine", "edit"}},
		{path: []string{"routine", "list"}},
		{path: []string{"routine", "fire"}},
		{path: []string{"routine", "pause"}},
		{path: []string{"routine", "resume"}},
		{path: []string{"routine", "runs"}},
		{path: []string{"routine", "handoff"}},
		{path: []string{"routine", "dashboard"}},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.path, " "), func(t *testing.T) {
			t.Parallel()
			if _, _, err := rootCmd.Find(tt.path); err != nil {
				t.Fatalf("Find(%v): %v", tt.path, err)
			}
		})
	}
}

func setupRoutineCmdTest(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	cd := newTestCmdDeps(t, home, dataHome, "")
	setCmdLayerDeps(t, cd)
}

func TestRunRoutineNewAndList(t *testing.T) {
	t.Parallel()
	setupRoutineCmdTest(t)

	oldNew := routineNew
	oldList := routineList
	oldInteractive := routineInteractive
	defer func() {
		routineNew = oldNew
		routineList = oldList
		routineInteractive = oldInteractive
	}()
	routineInteractive = func() bool { return false }
	routineNew = func(id, scheduleRaw, cwd string) (*routine.AddResult, error) {
		d := cmdLayerDeps().routineDeps()
		d.IsInteractive = func() bool { return false }
		return routine.AddWith(d, id, scheduleRaw, cwd)
	}
	routineList = func(out io.Writer) error {
		return routine.ListWith(cmdLayerDeps().routineDeps(), out)
	}

	var newOut bytes.Buffer
	routineNewCmd.SetOut(&newOut)
	routineNewCmd.SetErr(&newOut)
	routineNewSchedule = "every 6h"
	if err := runRoutineNew(routineNewCmd, []string{"home-routine"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(newOut.String(), "Created routine") {
		t.Fatalf("new output = %q", newOut.String())
	}
	for _, want := range []string{"created paused", "pop routine fire home-routine", "pop routine resume home-routine"} {
		if !strings.Contains(newOut.String(), want) {
			t.Fatalf("new output missing guidance %q:\n%s", want, newOut.String())
		}
	}

	var listOut bytes.Buffer
	routineListCmd.SetOut(&listOut)
	if err := runRoutineList(routineListCmd, nil); err != nil {
		t.Fatal(err)
	}
	text := listOut.String()
	for _, want := range []string{"home-routine", "every 6h", "yes"} {
		if !strings.Contains(text, want) {
			t.Fatalf("list output missing %q:\n%s", want, text)
		}
	}
}

func TestRunRoutineNewUnscheduledHint(t *testing.T) {
	t.Parallel()
	setupRoutineCmdTest(t)

	oldNew := routineNew
	oldInteractive := routineInteractive
	oldSchedule := routineNewSchedule
	defer func() {
		routineNew = oldNew
		routineInteractive = oldInteractive
		routineNewSchedule = oldSchedule
	}()
	routineInteractive = func() bool { return false }
	routineNew = func(id, scheduleRaw, cwd string) (*routine.AddResult, error) {
		d := cmdLayerDeps().routineDeps()
		d.IsInteractive = func() bool { return false }
		return routine.AddWith(d, id, scheduleRaw, cwd)
	}

	var newOut bytes.Buffer
	routineNewCmd.SetOut(&newOut)
	routineNewCmd.SetErr(&newOut)
	routineNewSchedule = ""
	if err := runRoutineNew(routineNewCmd, []string{"unscheduled-routine"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Schedule: manual", "No schedule was set", "pop routine edit unscheduled-routine --schedule"} {
		if !strings.Contains(newOut.String(), want) {
			t.Fatalf("new output missing %q:\n%s", want, newOut.String())
		}
	}
}

func TestRunRoutinePauseResumeAndRuns(t *testing.T) {
	t.Parallel()
	setupRoutineCmdTest(t)

	oldNew := routineNew
	oldList := routineList
	oldPause := routinePause
	oldResume := routineResume
	oldRuns := routineRuns
	oldInteractive := routineInteractive
	defer func() {
		routineNew = oldNew
		routineList = oldList
		routinePause = oldPause
		routineResume = oldResume
		routineRuns = oldRuns
		routineInteractive = oldInteractive
	}()
	routineInteractive = func() bool { return false }
	routineNew = func(id, scheduleRaw, cwd string) (*routine.AddResult, error) {
		d := cmdLayerDeps().routineDeps()
		d.IsInteractive = func() bool { return false }
		return routine.AddWith(d, id, scheduleRaw, cwd)
	}
	rd := func() *routine.Deps { return cmdLayerDeps().routineDeps() }
	routineList = func(out io.Writer) error {
		return routine.ListWith(rd(), out)
	}
	routinePause = func(id string) (*routine.PauseResult, error) {
		return routine.PauseWith(rd(), id)
	}
	routineResume = func(id string) (*routine.ResumeResult, error) {
		return routine.ResumeWith(rd(), id)
	}
	routineRuns = func(id string, out io.Writer) error {
		return routine.RunsWith(rd(), id, out)
	}

	routineNewSchedule = "every 6h"
	if err := runRoutineNew(routineNewCmd, []string{"cli-routine"}); err != nil {
		t.Fatal(err)
	}
	// Routines are created paused; arm it so the pause/resume cycle starts unpaused.
	routineResumeCmd.SetOut(io.Discard)
	if err := runRoutineResume(routineResumeCmd, []string{"cli-routine"}); err != nil {
		t.Fatal(err)
	}

	var pauseOut bytes.Buffer
	routinePauseCmd.SetOut(&pauseOut)
	if err := runRoutinePause(routinePauseCmd, []string{"cli-routine"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pauseOut.String(), "Paused routine") {
		t.Fatalf("pause output = %q", pauseOut.String())
	}

	var listOut bytes.Buffer
	routineListCmd.SetOut(&listOut)
	if err := runRoutineList(routineListCmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut.String(), "yes") {
		t.Fatalf("list after pause = %q", listOut.String())
	}

	pauseOut.Reset()
	if err := runRoutinePause(routinePauseCmd, []string{"cli-routine"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pauseOut.String(), "already paused") {
		t.Fatalf("second pause output = %q", pauseOut.String())
	}

	var resumeOut bytes.Buffer
	routineResumeCmd.SetOut(&resumeOut)
	if err := runRoutineResume(routineResumeCmd, []string{"cli-routine"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resumeOut.String(), "Resumed routine") {
		t.Fatalf("resume output = %q", resumeOut.String())
	}

	resumeOut.Reset()
	if err := runRoutineResume(routineResumeCmd, []string{"cli-routine"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resumeOut.String(), "not paused") {
		t.Fatalf("second resume output = %q", resumeOut.String())
	}

	var runsOut bytes.Buffer
	routineRunsCmd.SetOut(&runsOut)
	if err := runRoutineRuns(routineRunsCmd, []string{"cli-routine"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runsOut.String(), "No runs yet") {
		t.Fatalf("runs output = %q", runsOut.String())
	}

	if err := runRoutinePause(routinePauseCmd, []string{"unknown-id"}); err == nil {
		t.Fatal("expected unknown pause error")
	}
}
