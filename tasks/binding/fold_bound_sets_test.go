package binding

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// answering is a confirmation source that reads canned lines, standing in for the
// TTY the picker's fold pane gives the verb.
func answering(lines string) *lineReader {
	return &lineReader{br: bufio.NewReader(strings.NewReader(lines))}
}

// seedMalformedTaskSet registers a set pop cannot read: the manifest names an
// effort no scheduler knows, which is what makes a row MALFORMED.
func seedMalformedTaskSet(t *testing.T, td *tasks.Deps, repo, setID string) {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatalf("def path: %v", err)
	}
	taskDir := filepath.Join(defPath, setID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "01-open.md"), []byte("## Acceptance criteria\n\n- [ ] open\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"tasks": []map[string]any{{
		"id": "01-open", "file": "01-open.md", "title": "Open", "type": "AFK", "status": "open", "effort": "extreme",
	}}}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.RegisterWith(td, defPath, tasks.StatePathFor(defPath)); err != nil {
		t.Fatalf("register: %v", err)
	}
}

// bindSetTo records a second binding on a checkout another set already uses, so a
// fold meets more than one bound set.
func bindSetTo(t *testing.T, td *tasks.Deps, repo, path, branch, setID string) {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if err := Put(td, Key(id, setID), Adopt(td, path, branch, repo)); err != nil {
		t.Fatalf("bind %s: %v", setID, err)
	}
}

// A binding is pop's own bookkeeping, so it asks rather than refuses; a finished set
// is signed off and released by the fold, because withholding that would strand it
// behind the already-contained refusal with no verb left to clear the binding.
func TestFoldCheckoutBoundDoneSetConfirmsThenReleasesIt(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-bound-done")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-bound-done",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	if _, err := FoldCheckout(td, cfg, b.RuntimePath, FoldOptions{
		In: answering("y\n"), ConfirmCheckoutFold: true,
	}, &out); err != nil {
		t.Fatalf("fold of a bound checkout: %v\n%s", err, out.String())
	}
	for _, want := range []string{b.RuntimePath, "Task set set-bound-done (DONE)", "binding released"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("confirmation missing %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
		t.Fatalf("trunk must carry the folded work: %v", err)
	}
	if _, _, ok, err := FindBySetID(td, "set-bound-done"); err != nil || ok {
		t.Fatalf("finished set kept its binding: ok=%v err=%v", ok, err)
	}
	// The checkout verb deletes nothing, whatever bindings it released.
	if _, err := os.Stat(b.RuntimePath); err != nil {
		t.Fatalf("checkout torn down by a checkout-addressed fold: %v", err)
	}

	// Nothing is left for the set-addressed verb to do — and in particular it does
	// not meet the already-contained refusal with a binding it cannot clear.
	_, err = Fold(td, nil, cfg, "set-bound-done", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no worktree binding") {
		t.Fatalf("re-run of the set-addressed fold = %v, want nothing left to do", err)
	}
}

// Folding an Awaiting-approval set is its sign-off, and one question covers the
// whole landing: the canned answer below is a single line.
func TestFoldCheckoutBoundAwaitingApprovalSignsOffOnOneQuestion(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	setID := "set-bound-approval"
	seedAwaitingApprovalTaskSet(t, td, repo, setID, []map[string]any{
		{"id": "09-signoff", "title": "Sign off"},
	})
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: setID,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	if _, err := FoldCheckout(td, cfg, b.RuntimePath, FoldOptions{
		In: answering("y\n"), ConfirmCheckoutFold: true,
	}, &out); err != nil {
		t.Fatalf("fold of an Awaiting-approval binding: %v\n%s", err, out.String())
	}
	for _, want := range []string{"(AWAITING-APPROVAL)", "09-signoff", "binding released"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("confirmation missing %q:\n%s", want, out.String())
		}
	}
	if got := hitlTaskStatus(t, td, repo, setID, "09-signoff"); got != tasks.TaskDone {
		t.Fatalf("HITL task = %q, want the fold to have signed it off", got)
	}
	if got := manifestStatusAt(t, td, repo, setID); got != tasks.StatusDone {
		t.Fatalf("set status = %q, want DONE", got)
	}
	if _, _, ok, _ := FindBySetID(td, setID); ok {
		t.Fatal("signed-off set kept its binding")
	}
}

// Several bound sets are named one by one, and each answers for itself: the
// finished one is released, the unfinished one keeps the checkout it lives in.
func TestFoldCheckoutNamesEveryBoundSetAndKeepsUnfinishedBindings(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-shared-done")
	seedOpenTaskSet(t, td, repo, "set-shared-open")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-shared-done",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	bindSetTo(t, td, repo, b.RuntimePath, b.Branch, "set-shared-open")
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	if _, err := FoldCheckout(td, cfg, b.RuntimePath, FoldOptions{
		In: answering("y\n"), ConfirmCheckoutFold: true,
	}, &out); err != nil {
		t.Fatalf("fold of a checkout two sets are bound to: %v\n%s", err, out.String())
	}
	for _, want := range []string{
		"Task set set-shared-done (DONE) — signed off and its binding released",
		"Task set set-shared-open (READY) — binding kept",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("confirmation missing the line %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "2 Task sets") {
		t.Fatalf("bound sets collapsed to a count:\n%s", out.String())
	}
	if _, _, ok, _ := FindBySetID(td, "set-shared-done"); ok {
		t.Fatal("finished set kept its binding")
	}
	_, kept, ok, err := FindBySetID(td, "set-shared-open")
	if err != nil || !ok {
		t.Fatalf("unfinished set lost its binding: ok=%v err=%v", ok, err)
	}
	if kept.RuntimePath != b.RuntimePath {
		t.Fatalf("unfinished set moved home: %q, want %q", kept.RuntimePath, b.RuntimePath)
	}
	// The checkout is still a place that set can carry on in: same directory, same
	// branch, now standing on the landed tip.
	if _, err := os.Stat(b.RuntimePath); err != nil {
		t.Fatalf("checkout removed under an unfinished set: %v", err)
	}
	if got := currentBranchAt(t, b.RuntimePath); got != b.Branch {
		t.Fatalf("checkout branch = %q, want %q", got, b.Branch)
	}
	if got, want := refAt(t, b.RuntimePath, b.Branch), refAt(t, repo, "HEAD"); got != want {
		t.Fatalf("checkout tip = %s, want the landed trunk tip %s", got, want)
	}
}

// Every status outside DONE and Awaiting-approval asks the same question and names
// what it found — NEEDS-VERIFY holds no privileged refusal, and MALFORMED is
// admitted for one rule rather than nine.
func TestFoldCheckoutAsksAlikeForEveryUnfinishedStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status string
		verify bool
		seed   func(t *testing.T, td *tasks.Deps, repo, setID string)
	}{
		{name: "ready", status: "READY", seed: func(t *testing.T, td *tasks.Deps, repo, setID string) {
			seedOpenTaskSet(t, td, repo, setID)
		}},
		{name: "needs-verify", status: "NEEDS-VERIFY", verify: true, seed: func(t *testing.T, td *tasks.Deps, repo, setID string) {
			seedDoneTaskSet(t, td, repo, setID)
		}},
		{name: "malformed", status: "MALFORMED", seed: seedMalformedTaskSet},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := initAdoptRepo(t)
			td := lifecycleTestDeps(t)
			setID := "set-" + tc.name
			tc.seed(t, td, repo, setID)
			b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
				TD: td, CheckoutPath: repo, SetID: setID,
			})
			if err != nil {
				t.Fatalf("provision: %v", err)
			}
			writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
			cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
			if tc.verify {
				cfg.Work = &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}
			}

			var out bytes.Buffer
			if _, err := FoldCheckout(td, cfg, b.RuntimePath, FoldOptions{
				In: answering("y\n"), ConfirmCheckoutFold: true,
			}, &out); err != nil {
				t.Fatalf("fold of a %s binding: %v\n%s", tc.status, err, out.String())
			}
			want := fmt.Sprintf("Task set %s (%s) — binding kept", setID, tc.status)
			if !strings.Contains(out.String(), want) {
				t.Fatalf("confirmation missing %q:\n%s", want, out.String())
			}
			if _, _, ok, _ := FindBySetID(td, setID); !ok {
				t.Fatalf("%s set lost its binding to a fold that did not need it", tc.status)
			}
			if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
				t.Fatalf("trunk must carry the folded work: %v", err)
			}
		})
	}
}

// The answer is obeyed both ways: declining leaves trunk and the binding as they
// were.
func TestFoldCheckoutDeclinedBoundConfirmationChangesNothing(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-declined")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-declined",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	trunkBefore := refAt(t, repo, "HEAD")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	_, err = FoldCheckout(td, cfg, b.RuntimePath, FoldOptions{
		In: answering("n\n"), ConfirmCheckoutFold: true,
	}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "fold cancelled") {
		t.Fatalf("err = %v, want the declined fold to cancel", err)
	}
	if got := refAt(t, repo, "HEAD"); got != trunkBefore {
		t.Fatalf("trunk moved on a declined fold: %s -> %s", trunkBefore, got)
	}
	if _, _, ok, _ := FindBySetID(td, "set-declined"); !ok {
		t.Fatal("declined fold released the binding")
	}
}

// --yes is the entry ticket every headless channel needs, so it answers this
// question like any other — and the fold puts the override it took in the record.
func TestFoldCheckoutYesLandsBoundSetAndSaysSo(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-unasked")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-unasked",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	if _, err := FoldCheckout(td, cfg, b.RuntimePath, FoldOptions{
		Yes: true, In: tasks.NonInteractiveReader{}, ConfirmCheckoutFold: true,
	}, &out); err != nil {
		t.Fatalf("non-interactive fold of a bound checkout: %v\n%s", err, out.String())
	}
	for _, want := range []string{"--yes", "without asking", "set-unasked is DONE"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Fatalf("--yes still printed a question:\n%s", out.String())
	}
	if _, _, ok, _ := FindBySetID(td, "set-unasked"); ok {
		t.Fatal("binding survived the non-interactive fold")
	}
}

// A checkout-addressed fold's tail is a set's business now, so it needs the same
// convergence the set-addressed tail has: the landing marker goes back and the
// re-run finishes what the failure left.
func TestFoldCheckoutBoundTailFailureRestoresMarkerAndRerunFinishes(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	setID := "set-checkout-tail"
	seedAwaitingApprovalTaskSet(t, td, repo, setID, []map[string]any{
		{"id": "09-signoff", "title": "Sign off"},
	})
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: setID,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	writeFileCommit(t, repo, "trunk.txt", "trunk work\n", "trunk work")
	scratch := foldScratchBranch(b.Branch)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	mock, ok := td.FS.(*deps.MockFileSystem)
	if !ok {
		t.Fatalf("test deps FS = %T, want a mock to fail the sign-off write", td.FS)
	}
	realRename := mock.RenameFunc
	// Armed only once git has deleted the scratch ref: that is the state nothing but
	// the restored marker can recover from.
	var armed atomic.Bool
	mock.RenameFunc = func(oldpath, newpath string) error {
		if armed.Load() && strings.HasSuffix(newpath, filepath.Join(setID, "index.json")) {
			return fmt.Errorf("simulated manifest write failure")
		}
		return realRename(oldpath, newpath)
	}
	inner := td.Git
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			out, err := inner.CommandInDir(dir, args...)
			if err == nil && len(args) >= 3 && args[0] == "branch" && args[1] == "-d" && args[2] == scratch {
				armed.Store(true)
			}
			return out, err
		},
	}

	opts := FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}, ConfirmCheckoutFold: true}
	if _, err := FoldCheckout(td, cfg, b.RuntimePath, opts, io.Discard); err == nil || !strings.Contains(err.Error(), "marks the landing") {
		t.Fatalf("err = %v, want the tail failure to report the preserved landing marker", err)
	}
	landed := refAt(t, repo, "HEAD")
	if !branchExists(t, repo, scratch) {
		t.Fatalf("tail failure left no landing marker %s, so a rerun cannot converge", scratch)
	}
	if _, _, ok, _ := FindBySetID(td, setID); !ok {
		t.Fatal("binding released despite the failed tail")
	}

	armed.Store(false)
	td.Git = inner
	if _, err := FoldCheckout(td, cfg, b.RuntimePath, opts, io.Discard); err != nil {
		t.Fatalf("rerun after the tail failure: %v", err)
	}
	if got := manifestStatusAt(t, td, repo, setID); got != tasks.StatusDone {
		t.Fatalf("set status after rerun = %q, want DONE", got)
	}
	if _, _, ok, _ := FindBySetID(td, setID); ok {
		t.Fatal("rerun left the binding behind")
	}
	if branchExists(t, repo, scratch) {
		t.Fatalf("rerun left the landing marker %s", scratch)
	}
	if now := refAt(t, repo, "HEAD"); now != landed {
		t.Fatalf("rerun moved trunk: %s -> %s", landed, now)
	}
	if n := strings.Count(runGitOutput(t, repo, "log", "--oneline"), "set work"); n != 1 {
		t.Fatalf("trunk carries the folded commit %d times, want one", n)
	}
	if _, err := os.Stat(b.RuntimePath); err != nil {
		t.Fatalf("checkout torn down by the converging fold: %v", err)
	}
}

// A binding stopped being a reason to refuse; nothing else did. Where there is no
// act to perform, the refusal comes before any question is asked.
func TestFoldCheckoutBoundCheckoutStillRefusesAlreadyContained(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-contained")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-contained",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	_, err = FoldCheckout(td, cfg, b.RuntimePath, FoldOptions{
		In: answering("y\n"), ConfirmCheckoutFold: true,
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "already contained in trunk") {
		t.Fatalf("err = %v, want the already-contained refusal", err)
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Fatalf("a refusal asked a question first:\n%s", out.String())
	}
	if _, _, ok, _ := FindBySetID(td, "set-contained"); !ok {
		t.Fatal("refused fold released the binding")
	}
}
