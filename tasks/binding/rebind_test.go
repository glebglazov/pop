package binding

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestAuthorizeLeavingBindingCleanSetSkipsProgressPrompt(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	wt := addLinkedWorktree(t, repo, "bound-branch")
	seedLifecycleBinding(t, td, repo, "set-a", Binding{
		RuntimePath: wt,
		Branch:      "bound-branch",
		Project:     filepath.Base(repo),
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	ok, err := AuthorizeLeavingBinding(td, nil, cfg, "set-a", Binding{RuntimePath: wt}, keyForRepoSet(td, repo, "set-a"), false, "", false, strings.NewReader("n\n"), &out, LifecycleHooks{})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !ok {
		t.Fatal("clean set should authorize without prompting")
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestAuthorizeLeavingBindingStartedDecline(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	managed := seedManagedBindingAtRoot(t, td, repo, "set-a")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	ok, err := AuthorizeLeavingBinding(td, nil, cfg, "set-a", managed, keyForRepoSet(td, repo, "set-a"), true, "1/2 done", false, strings.NewReader("n\n"), io.Discard, LifecycleHooks{})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if ok {
		t.Fatal("declined progress prompt should not authorize")
	}
}

func TestAuthorizeLeavingBindingStartedNonInteractiveRequiresYes(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	managed := seedManagedBindingAtRoot(t, td, repo, "set-a")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	_, err := AuthorizeLeavingBinding(td, nil, cfg, "set-a", managed, keyForRepoSet(td, repo, "set-a"), true, "1/2 done", false, tasks.NonInteractiveReader{}, io.Discard, LifecycleHooks{})
	if err == nil || !strings.Contains(err.Error(), implementRebindNonInteractiveErr) {
		t.Fatalf("err = %v, want non-interactive progress error", err)
	}
}

func TestAuthorizeLeavingBindingStartedPipedStdinRequiresYes(t *testing.T) {
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	managed := seedManagedBindingAtRoot(t, td, repo, "set-a")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	_, err = AuthorizeLeavingBinding(td, nil, cfg, "set-a", managed, keyForRepoSet(td, repo, "set-a"), true, "1/2 done", false, os.Stdin, io.Discard, LifecycleHooks{})
	if err == nil || !strings.Contains(err.Error(), implementRebindNonInteractiveErr) {
		t.Fatalf("err = %v, want non-interactive progress error for piped stdin", err)
	}
}

func TestAuthorizeLeavingBindingManagedTeardownPipedStdinRequiresYes(t *testing.T) {
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	managed := seedManagedBindingAtRoot(t, td, repo, "set-a")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	_, err = AuthorizeLeavingBinding(td, nil, cfg, "set-a", managed, keyForRepoSet(td, repo, "set-a"), false, "", false, os.Stdin, io.Discard, LifecycleHooks{})
	if err == nil || !strings.Contains(err.Error(), implementRebindTeardownNonInteractiveErr) {
		t.Fatalf("err = %v, want non-interactive teardown error for piped stdin", err)
	}
}

func TestAuthorizeLeavingBindingManagedTeardownAfterProgress(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	managed := seedManagedBindingAtRoot(t, td, repo, "set-a")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	key := keyForRepoSet(td, repo, "set-a")

	var out bytes.Buffer
	ok, err := AuthorizeLeavingBinding(td, nil, cfg, "set-a", managed, key, true, "1/2 done", false, strings.NewReader("y\ny\n"), &out, LifecycleHooks{})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !ok {
		t.Fatal("want authorized")
	}
	if !strings.Contains(out.String(), "delete managed worktree") {
		t.Fatalf("output = %q, want managed delete prompt after progress", out.String())
	}
	if _, err := os.Stat(managed.RuntimePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed worktree should be removed after teardown confirm")
	}
}

func TestRouteDrainCheckoutForceRebindRepointsToCurrentCheckout(t *testing.T) {
	t.Parallel()
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	oldWT := addLinkedWorktree(t, repo, "old-bound")
	currentWT := addLinkedWorktree(t, repo, "current")
	seedBinding(t, td, repo, "set-a", Adopt(oldWT, "old-bound", "proj"))

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: currentWT,
		SetID:           "set-a",
		Trigger:         TriggerImplementForeground,
		ForceRebind:     true,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if !got.Rebound {
		t.Fatalf("result = %+v, want Rebound", got)
	}
	currentRuntime, err := tasks.ResolveRuntimePathWith(td, currentWT, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.RuntimePath != currentRuntime {
		t.Fatalf("RuntimePath = %q, want current checkout %q", got.RuntimePath, currentRuntime)
	}
	_, b, ok, err := GetForSet(td, currentWT, "set-a")
	if err != nil || !ok || b.RuntimePath != currentRuntime || b.Provisioned {
		t.Fatalf("binding = %+v ok=%v, want adopted rebind at %q", b, ok, currentRuntime)
	}
}

func TestConfirmYesNoNilInUsesCharDeviceStdin(t *testing.T) {
	devNull, err := os.Open("/dev/null")
	if err != nil {
		t.Skipf("no /dev/null: %v", err)
	}
	defer devNull.Close()

	oldStdin := os.Stdin
	os.Stdin = devNull
	defer func() { os.Stdin = oldStdin }()

	var out bytes.Buffer
	ok, err := confirmYesNo(nil, &out, false, "prompt? [y/N]: ", "non-interactive")
	if err != nil {
		t.Fatalf("confirmYesNo(nil, char-device stdin): %v", err)
	}
	if ok {
		t.Fatal("EOF from /dev/null should decline")
	}
	if out.String() != "prompt? [y/N]: " {
		t.Fatalf("output = %q, want prompt only", out.String())
	}
}

func TestDoubleConfirmYesNoReadsSequentialLines(t *testing.T) {
	t.Parallel()
	confirmIn := &lineReader{br: bufio.NewReader(strings.NewReader("y\ny\n"))}
	var out bytes.Buffer
	ok, err := confirmYesNo(confirmIn, &out, false, "first? [y/N]: ", "err1")
	if err != nil || !ok {
		t.Fatalf("first confirm: ok=%v err=%v out=%q", ok, err, out.String())
	}
	ok, err = confirmYesNo(confirmIn, &out, false, "second? [y/N]: ", "err2")
	if err != nil || !ok {
		t.Fatalf("second confirm: ok=%v err=%v out=%q", ok, err, out.String())
	}
}

func keyForRepoSet(td *tasks.Deps, repo, setID string) string {
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		panic(err)
	}
	return Key(id, setID)
}
