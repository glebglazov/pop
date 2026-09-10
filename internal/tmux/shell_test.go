package tmux

import (
	"reflect"
	"testing"
)

func TestDefaultShellReadsGlobalOption(t *testing.T) {
	r := &recordingRunner{out: "/opt/homebrew/bin/zsh"}
	tm := &realTmux{run: r}

	got, err := tm.DefaultShell()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/opt/homebrew/bin/zsh" {
		t.Fatalf("shell = %q, want /opt/homebrew/bin/zsh", got)
	}
	if want := [][]string{{"show-options", "-gv", "default-shell"}}; !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}
