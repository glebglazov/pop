package tmux

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestExecRunnerSurfacesStderr proves the moved-in error mapping: a non-zero
// tmux exit surfaces the binary's stderr through outputError.
func TestExecRunnerSurfacesStderr(t *testing.T) {
	withFakeTmux(t, "tmux says nope")

	_, err := execRunner{}.output("list-sessions")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tmux says nope") {
		t.Fatalf("expected stderr in error, got %q", err.Error())
	}
}

func TestExecRunnerLeavesEmptyStderrUnchanged(t *testing.T) {
	withFakeTmux(t, "")

	_, err := execRunner{}.output("list-sessions")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), ": ") {
		t.Fatalf("expected bare error without stderr suffix, got %q", err.Error())
	}
}

// TestExecRunnerSocketFlag proves ADR-0199 decision 1: an empty socket emits
// no -L (argv identical to pre-socket-key pop); a set socket prepends -L on
// every runner method.
func TestExecRunnerSocketFlag(t *testing.T) {
	t.Run("unset emits no socket flag", func(t *testing.T) {
		got := withArgRecordingTmux(t)
		_, err := (execRunner{}).output("list-sessions")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"list-sessions"}
		if !reflect.DeepEqual(got(), want) {
			t.Fatalf("output argv = %v, want %v", got(), want)
		}
	})

	t.Run("set prepends -L on output", func(t *testing.T) {
		got := withArgRecordingTmux(t)
		_, err := (execRunner{socket: "pop"}).output("list-sessions")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"-L", "pop", "list-sessions"}
		if !reflect.DeepEqual(got(), want) {
			t.Fatalf("output argv = %v, want %v", got(), want)
		}
	})

	t.Run("set prepends -L on attach", func(t *testing.T) {
		got := withArgRecordingTmux(t)
		if err := (execRunner{socket: "pop"}).attach("attach-session", "-t", "s"); err != nil {
			t.Fatal(err)
		}
		want := []string{"-L", "pop", "attach-session", "-t", "s"}
		if !reflect.DeepEqual(got(), want) {
			t.Fatalf("attach argv = %v, want %v", got(), want)
		}
	})

	t.Run("set prepends -L on input", func(t *testing.T) {
		got := withArgRecordingTmux(t)
		if err := (execRunner{socket: "pop"}).input("hi", "load-buffer", "-"); err != nil {
			t.Fatal(err)
		}
		want := []string{"-L", "pop", "load-buffer", "-"}
		if !reflect.DeepEqual(got(), want) {
			t.Fatalf("input argv = %v, want %v", got(), want)
		}
	})

	t.Run("unset attach argv unchanged", func(t *testing.T) {
		got := withArgRecordingTmux(t)
		if err := (execRunner{}).attach("attach-session", "-t", "s"); err != nil {
			t.Fatal(err)
		}
		want := []string{"attach-session", "-t", "s"}
		if !reflect.DeepEqual(got(), want) {
			t.Fatalf("attach argv = %v, want %v", got(), want)
		}
	})
}

// withFakeTmux puts a failing tmux stub earlier on PATH so exec.Command
// finds it. The stub writes stderr (when given) and exits non-zero.
func withFakeTmux(t *testing.T, stderr string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\n"
	if stderr != "" {
		script += "printf '%s\\n' '" + strings.ReplaceAll(stderr, "'", "'\\''") + "' >&2\n"
	}
	script += "exit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// withArgRecordingTmux installs a tmux stub that records argv to a file and
// exits zero. The returned func reads the latest recorded argv.
func withArgRecordingTmux(t *testing.T) func() []string {
	t.Helper()

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	bin := filepath.Join(dir, "tmux")
	// Record "$@" null-separated so spaces in args survive.
	script := "#!/bin/sh\n" +
		"printf '%s\\0' \"$@\" > '" + argsPath + "'\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return func() []string {
		data, err := os.ReadFile(argsPath)
		if err != nil {
			t.Fatalf("read recorded args: %v", err)
		}
		parts := strings.Split(string(data), "\x00")
		// Trailing printf leaves a final empty segment.
		if len(parts) > 0 && parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		return parts
	}
}
