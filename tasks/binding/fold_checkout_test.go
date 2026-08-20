package binding

import (
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// foldActionGit wraps a real git and records the calls that change a repository,
// labelling each by the end of the fold it ran in. Two folds that take the same
// git actions produce the same recording, whatever addressed them.
type foldActionGit struct {
	inner   deps.Git
	labels  map[string]string
	mu      sync.Mutex
	actions []string
}

var foldMutatingVerbs = map[string]bool{
	"rebase":      true,
	"merge":       true,
	"checkout":    true,
	"switch":      true,
	"branch":      true,
	"reset":       true,
	"cherry-pick": true,
	"commit":      true,
	"add":         true,
	"worktree":    true,
	"stash":       true,
}

func (g *foldActionGit) label(dir string) string {
	for path, label := range g.labels {
		if sameRealPath(path, dir) {
			return label
		}
	}
	return "elsewhere"
}

// foldReadOnlyCall reports the read-only spellings of otherwise mutating verbs:
// a fold reads the current branch and the worktree list without changing either.
func foldReadOnlyCall(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[0] {
	case "branch":
		return args[1] == "--show-current" || args[1] == "--list" || args[1] == "-l" || args[1] == "--contains"
	case "worktree":
		return args[1] == "list"
	}
	return false
}

// foldCommitID masks the commit ids an action names, so two folds in two
// repositories compare on what they did rather than on which objects they did it to.
var foldCommitID = regexp.MustCompile(`\b[0-9a-f]{40}\b`)

func (g *foldActionGit) record(dir string, args []string) {
	if len(args) == 0 || !foldMutatingVerbs[args[0]] || foldReadOnlyCall(args) {
		return
	}
	action := foldCommitID.ReplaceAllString(g.label(dir)+": git "+strings.Join(args, " "), "<sha>")
	g.mu.Lock()
	defer g.mu.Unlock()
	g.actions = append(g.actions, action)
}

func (g *foldActionGit) Command(args ...string) (string, error) {
	g.record("", args)
	return g.inner.Command(args...)
}

func (g *foldActionGit) CommandInDir(dir string, args ...string) (string, error) {
	g.record(dir, args)
	return g.inner.CommandInDir(dir, args...)
}

func sameRealPath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}

// The Task-set fold is a specialization of the checkout fold, so the two must
// reach for git identically: same commands, same order, same checkout each ran in.
func TestSetFoldAndCheckoutFoldTakeTheSameGitActions(t *testing.T) {
	t.Parallel()

	// Set-addressed: a DONE set bound to a managed worktree, folded by set id.
	setRepo := initAdoptRepo(t)
	setTD := lifecycleTestDeps(t)
	seedDoneTaskSet(t, setTD, setRepo, "set-parity")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: setTD, CheckoutPath: setRepo, SetID: "set-parity",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "work\n", "branch work")
	writeFileCommit(t, setRepo, "trunk.txt", "trunk work\n", "trunk work")
	setRec := &foldActionGit{
		inner:  setTD.Git,
		labels: map[string]string{b.RuntimePath: "folding", setRepo: "trunk"},
	}
	setTD.Git = setRec
	setCfg := &config.Config{Projects: []config.ProjectEntry{{Path: setRepo}}}
	// Declining teardown keeps the comparison to the fold itself; teardown is a
	// promise the set owes, not a git action of folding.
	if _, err := Fold(setTD, nil, setCfg, "set-parity", FoldOptions{In: strings.NewReader("n\n")}, LifecycleHooks{}, io.Discard); err != nil {
		t.Fatalf("set-addressed fold: %v", err)
	}

	// Checkout-addressed: the same worktree shape, no set and no binding, folded by path.
	plainRepo := initAdoptRepo(t)
	plainTD := lifecycleTestDeps(t)
	wt := addLinkedWorktree(t, plainRepo, b.Branch)
	writeFileCommit(t, wt, "feature.txt", "work\n", "branch work")
	writeFileCommit(t, plainRepo, "trunk.txt", "trunk work\n", "trunk work")
	plainRec := &foldActionGit{
		inner:  plainTD.Git,
		labels: map[string]string{wt: "folding", plainRepo: "trunk"},
	}
	plainTD.Git = plainRec
	plainCfg := &config.Config{Projects: []config.ProjectEntry{{Path: plainRepo}}}
	got, err := FoldCheckout(plainTD, plainCfg, wt, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard)
	if err != nil {
		t.Fatalf("checkout-addressed fold: %v", err)
	}
	if got.Branch != b.Branch {
		t.Fatalf("Branch = %q, want %q", got.Branch, b.Branch)
	}
	if !sameRealPath(got.TrunkPath, plainRepo) {
		t.Fatalf("TrunkPath = %q, want %q", got.TrunkPath, plainRepo)
	}

	setActions := strings.Join(setRec.actions, "\n")
	plainActions := strings.Join(plainRec.actions, "\n")
	if setActions != plainActions {
		t.Fatalf("git actions differ\nset-addressed:\n%s\n\ncheckout-addressed:\n%s", setActions, plainActions)
	}
	if !strings.Contains(plainActions, "folding: git rebase ") {
		t.Fatalf("fold must rebase in the folding checkout, got:\n%s", plainActions)
	}
	if !strings.Contains(plainActions, "trunk: git merge --ff-only "+foldScratchBranch(b.Branch)) {
		t.Fatalf("fold must fast-forward trunk onto the fold scratch branch, got:\n%s", plainActions)
	}
}

// A checkout with no set still reaches the Fold conflict prompt, and a
// non-interactive fold refuses there with trunk untouched.
func TestFoldCheckoutConflictRefusesWithTrunkUnchanged(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	wt := addLinkedWorktree(t, repo, "human-work")
	writeFileCommit(t, wt, "clash.txt", "from-branch\n", "branch clash")
	writeFileCommit(t, repo, "clash.txt", "from-trunk\n", "trunk clash")
	trunkBefore := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err := FoldCheckout(td, cfg, wt, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want conflict refusal", err)
	}
	if now := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD")); now != trunkBefore {
		t.Fatalf("trunk moved on a conflicted fold: %s -> %s", trunkBefore, now)
	}
}

func TestFoldCheckoutRefusesTrunkItself(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	_, err := FoldCheckout(td, cfg, repo, FoldOptions{In: tasks.NonInteractiveReader{}}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "is the Trunk worktree itself; nothing to fold") {
		t.Fatalf("err = %v, want the Trunk-itself refusal", err)
	}
}
