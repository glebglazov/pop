package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

// runRoutineCmd drives one routine subcommand through cobra with a fresh
// command instance, so flag state and output buffers stay per-test.
func runRoutineCmd(t *testing.T, cmd *cobra.Command, out io.Writer, args ...string) error {
	t.Helper()
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestRunRoutineNewAndList(t *testing.T) {
	// Serial: mutates package-level routineNew / routineNewSchedule hooks.
	setupRoutineCmdTest(t)

	var newOut bytes.Buffer
	if err := runRoutineCmd(t, newRoutineNewCmd(), &newOut, "home-routine", "--schedule", "every 6h"); err != nil {
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
	if err := runRoutineCmd(t, newRoutineListCmd(), &listOut); err != nil {
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
	// Serial: mutates package-level routineNew / routineNewSchedule hooks.
	setupRoutineCmdTest(t)

	var newOut bytes.Buffer
	if err := runRoutineCmd(t, newRoutineNewCmd(), &newOut, "unscheduled-routine"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Schedule: manual", "No schedule was set", "pop routine edit unscheduled-routine --schedule"} {
		if !strings.Contains(newOut.String(), want) {
			t.Fatalf("new output missing %q:\n%s", want, newOut.String())
		}
	}
}

func TestRunRoutinePauseResumeAndRuns(t *testing.T) {
	// Serial: mutates package-level routineNew / routineNewSchedule hooks.
	setupRoutineCmdTest(t)

	if err := runRoutineCmd(t, newRoutineNewCmd(), io.Discard, "cli-routine", "--schedule", "every 6h"); err != nil {
		t.Fatal(err)
	}
	// Routines are created paused; arm it so the pause/resume cycle starts unpaused.
	if err := runRoutineCmd(t, newRoutineResumeCmd(), io.Discard, "cli-routine"); err != nil {
		t.Fatal(err)
	}

	var pauseOut bytes.Buffer
	if err := runRoutineCmd(t, newRoutinePauseCmd(), &pauseOut, "cli-routine"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pauseOut.String(), "Paused routine") {
		t.Fatalf("pause output = %q", pauseOut.String())
	}

	var listOut bytes.Buffer
	if err := runRoutineCmd(t, newRoutineListCmd(), &listOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listOut.String(), "yes") {
		t.Fatalf("list after pause = %q", listOut.String())
	}

	pauseOut.Reset()
	if err := runRoutineCmd(t, newRoutinePauseCmd(), &pauseOut, "cli-routine"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pauseOut.String(), "already paused") {
		t.Fatalf("second pause output = %q", pauseOut.String())
	}

	var resumeOut bytes.Buffer
	if err := runRoutineCmd(t, newRoutineResumeCmd(), &resumeOut, "cli-routine"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resumeOut.String(), "Resumed routine") {
		t.Fatalf("resume output = %q", resumeOut.String())
	}

	resumeOut.Reset()
	if err := runRoutineCmd(t, newRoutineResumeCmd(), &resumeOut, "cli-routine"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resumeOut.String(), "not paused") {
		t.Fatalf("second resume output = %q", resumeOut.String())
	}

	var runsOut bytes.Buffer
	if err := runRoutineCmd(t, newRoutineRunsCmd(), &runsOut, "cli-routine"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runsOut.String(), "No runs yet") {
		t.Fatalf("runs output = %q", runsOut.String())
	}

	if err := runRoutineCmd(t, newRoutinePauseCmd(), io.Discard, "unknown-id"); err == nil {
		t.Fatal("expected unknown pause error")
	}
}
