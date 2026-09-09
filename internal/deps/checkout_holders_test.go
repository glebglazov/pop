package deps

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRealCheckoutHolderProbeFindsWorkingDirectoriesAndFileMonitorSocket(t *testing.T) {
	t.Parallel()
	checkout := filepath.Join("/repo", "feature")
	gitDir := filepath.Join("/repo", ".git", "worktrees", "feature")
	outputs := []string{
		"p12\ncserver\nfcwd\nn/repo/feature\np13\nceditor\nfcwd\nn/repo/feature/subdir\np14\ncshell\nfcwd\nn/repo/other\n",
		"p15\ncgit\n",
	}
	probe := &RealCheckoutHolderProbe{run: func(args ...string) (string, error) {
		out := outputs[0]
		outputs = outputs[1:]
		return out, nil
	}}

	holders, err := probe.CheckoutHolders(checkout, gitDir)
	if err != nil {
		t.Fatalf("probe holders: %v", err)
	}
	want := []CheckoutHolder{{PID: 12, Name: "server"}, {PID: 13, Name: "editor"}, {PID: 15, Name: "git"}}
	if !reflect.DeepEqual(holders, want) {
		t.Fatalf("holders = %#v, want %#v", holders, want)
	}
}

func TestRealCheckoutHolderProbeReturnsCWDProbeFailure(t *testing.T) {
	t.Parallel()
	want := errors.New("lsof unavailable")
	probe := &RealCheckoutHolderProbe{run: func(args ...string) (string, error) { return "", want }}
	_, err := probe.CheckoutHolders("/repo/feature", "")
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
