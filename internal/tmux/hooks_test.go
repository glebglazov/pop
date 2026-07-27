package tmux

import (
	"reflect"
	"testing"
)

func TestInstallHookBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.InstallHook("pane-focus-in", `run-shell "pop pane visit"`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"set-hook", "-ga", "pane-focus-in", `run-shell "pop pane visit"`}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestUninstallHookBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.UninstallHook("after-select-pane[0]"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"set-hook", "-gu", "after-select-pane[0]"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
}

func TestGlobalHooksBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "after-select-pane[0] run-shell \"pop pane set-status\"\n" +
		"after-select-pane[1] run-shell \"echo other\"\n" +
		"no-bracket-line\n"}
	tm := &realTmux{run: r}

	hooks, err := tm.GlobalHooks()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArgs := [][]string{{"show-hooks", "-g"}}
	if !reflect.DeepEqual(r.calls, wantArgs) {
		t.Fatalf("args = %v, want %v", r.calls, wantArgs)
	}
	want := []Hook{
		{Index: "after-select-pane[0]", Command: `run-shell "pop pane set-status"`},
		{Index: "after-select-pane[1]", Command: `run-shell "echo other"`},
	}
	if !reflect.DeepEqual(hooks, want) {
		t.Fatalf("hooks = %+v, want %+v", hooks, want)
	}
}
