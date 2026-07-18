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
	tests := []struct {
		path []string
	}{
		{path: []string{"routine", "add"}},
		{path: []string{"routine", "list"}},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.path, " "), func(t *testing.T) {
			if _, _, err := rootCmd.Find(tt.path); err != nil {
				t.Fatalf("Find(%v): %v", tt.path, err)
			}
		})
	}
}

func TestRunRoutineAddAndList(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	oldAdd := routineAdd
	oldList := routineList
	defer func() {
		routineAdd = oldAdd
		routineList = oldList
	}()
	routineAdd = func(id, scheduleRaw, cwd string) (*routine.AddResult, error) {
		d := routine.DefaultDeps()
		d.IsInteractive = func() bool { return false }
		return routine.AddWith(d, id, scheduleRaw, cwd)
	}
	routineList = func(out io.Writer) error {
		d := routine.DefaultDeps()
		return routine.ListWith(d, out)
	}

	var addOut bytes.Buffer
	routineAddCmd.SetOut(&addOut)
	routineAddCmd.SetErr(&addOut)
	routineAddSchedule = "every 6h"
	if err := runRoutineAdd(routineAddCmd, []string{"home-routine"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(addOut.String(), "Created routine") {
		t.Fatalf("add output = %q", addOut.String())
	}

	var listOut bytes.Buffer
	routineListCmd.SetOut(&listOut)
	if err := runRoutineList(routineListCmd, nil); err != nil {
		t.Fatal(err)
	}
	text := listOut.String()
	for _, want := range []string{"home-routine", "every 6h", "no"} {
		if !strings.Contains(text, want) {
			t.Fatalf("list output missing %q:\n%s", want, text)
		}
	}
}
