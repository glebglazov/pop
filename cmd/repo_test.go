package cmd

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/conventions"
)

// conventionFixture is one repository with an isolated home and data dir, so a
// test can author any subset of the four layers and read the stack back.
type conventionFixture struct {
	root     string
	repo     string
	dataHome string
	deps     *Deps
}

func newConventionFixture(t *testing.T) *conventionFixture {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithCommitCmd(t, repo)
	dataHome := filepath.Join(root, "xdg")
	return &conventionFixture{
		root:     root,
		repo:     repo,
		dataHome: dataHome,
		deps:     newTestCmdDeps(t, repo, dataHome, ""),
	}
}

func (f *conventionFixture) write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f *conventionFixture) userDefaults(t *testing.T, kind, body string) string {
	t.Helper()
	return f.write(t, filepath.Join(f.dataHome, "home", ".agents", "docs", kind+".md"), body)
}

func (f *conventionFixture) overlay(t *testing.T, kind, body string) string {
	t.Helper()
	return f.write(t, filepath.Join(f.dataHome, "home", ".agents", "docs", kind+".overlay.md"), body)
}

func (f *conventionFixture) repoDoc(t *testing.T, kind, body string) string {
	t.Helper()
	return f.write(t, filepath.Join(f.repo, "docs", "agents", kind+".md"), body)
}

// memory writes the layer nothing in this slice writes for real, straight to
// the path Repository identity resolves to.
func (f *conventionFixture) memory(t *testing.T, dir, kind, body string) string {
	t.Helper()
	path, err := conventions.MemoryPath(f.deps.conventionsDeps(), conventions.Kind(kind), dir)
	if err != nil {
		t.Fatalf("resolve memory path: %v", err)
	}
	return f.write(t, path, body)
}

func (f *conventionFixture) get(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runRepoConventionsGetWith(f.deps.conventionsDeps(), &out, dir, args)
	return out.String(), err
}

// TestRepoConventionsGetComposesTheStack is the whole point of the verb: every
// layer that exists comes back, in rank order, labelled, under one statement of
// the override rule, and closed by the provenance line an agent surfaces.
func TestRepoConventionsGetComposesTheStack(t *testing.T) {
	f := newConventionFixture(t)

	defaultsPath := f.userDefaults(t, "commits", "MY-DEFAULT: conventional commits, lowercase subject.")
	memoryPath := f.memory(t, f.repo, "commits", `---
derived_from: a sample of 40 commits
derived_at: 2026-08-01
---
POP-MEMORY: scopes are the package name.`)
	repoPath := f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore, docs.")
	overlayPath := f.overlay(t, "commits", "MY-OVERLAY: never mention the agent in the body.")

	out, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}

	// Rank order is the contract; the bodies must appear lowest rank first.
	order := []string{"MY-DEFAULT", "POP-MEMORY", "TEAM-DOC", "MY-OVERLAY"}
	at := -1
	for _, marker := range order {
		i := strings.Index(out, marker)
		if i < 0 {
			t.Fatalf("layer %s missing from output:\n%s", marker, out)
		}
		if i < at {
			t.Fatalf("layers out of rank order at %s:\n%s", marker, out)
		}
		at = i
	}

	// Each layer is labelled with both its origin and the path it came from,
	// which is what lets a reader go and edit the right one.
	for origin, path := range map[string]string{
		"USER DEFAULTS": defaultsPath,
		"POP MEMORY":    memoryPath,
		"REPOSITORY":    repoPath,
		"USER OVERLAY":  overlayPath,
	} {
		if !strings.Contains(out, origin) {
			t.Errorf("output does not label the %s layer:\n%s", origin, out)
		}
		if !strings.Contains(out, path) {
			t.Errorf("output does not name the %s path %q:\n%s", origin, path, out)
		}
	}

	// Pop's bookkeeping is not part of the convention: the frontmatter is peeled
	// off the memory layer's body and re-emitted as provenance instead.
	if strings.Contains(out, "derived_from:") {
		t.Errorf("memory frontmatter leaked into the rendered body:\n%s", out)
	}

	if n := strings.Count(out, "where two of them directly contradict"); n != 1 {
		t.Errorf("override rule stated %d times, want exactly once:\n%s", n, out)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "Provenance:") {
		t.Fatalf("output does not end with the provenance summary, got %q", last)
	}
	if !strings.Contains(last, "user overlay") || !strings.Contains(last, overlayPath) {
		t.Errorf("provenance does not name the top layer: %q", last)
	}
	if !strings.Contains(last, "a sample of 40 commits") {
		t.Errorf("provenance does not disclose what pop memory was derived from: %q", last)
	}
}

// TestRepoConventionsGetWithOnlyUserDefaults is the common shape on a machine
// where nothing repository-specific has been written yet.
func TestRepoConventionsGetWithOnlyUserDefaults(t *testing.T) {
	f := newConventionFixture(t)
	path := f.userDefaults(t, "commits", "MY-DEFAULT: conventional commits.")

	out, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}
	for _, want := range []string{"MY-DEFAULT: conventional commits.", "USER DEFAULTS", path} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Provenance:") || !strings.Contains(out, "user defaults") {
		t.Fatalf("provenance does not name the only layer as the top one:\n%s", out)
	}
}

// TestRepoConventionsGetEmptyKindMisses pins the miss: exit 1, and the four
// places pop looked, since "nothing here" is only actionable with them named.
func TestRepoConventionsGetEmptyKindMisses(t *testing.T) {
	f := newConventionFixture(t)

	out, err := f.get(t, f.repo, "commits")
	if !errors.Is(err, conventions.ErrNoConvention) {
		t.Fatalf("error = %v, want ErrNoConvention\n%s", err, out)
	}
	for _, want := range []string{"user defaults", "pop memory", "repository", "user overlay"} {
		if !strings.Contains(out, want) {
			t.Errorf("miss output does not name the %s layer:\n%s", want, out)
		}
	}
	if !strings.Contains(out, filepath.Join("docs", "agents", "commits.md")) {
		t.Errorf("miss output does not name the repository path:\n%s", out)
	}
}

// TestRepoConventionsGetAllKinds walks every kind with no argument. A kind with
// an answer keeps the exit code at 0 even when its neighbour is empty.
func TestRepoConventionsGetAllKinds(t *testing.T) {
	f := newConventionFixture(t)

	out, err := f.get(t, f.repo)
	if !errors.Is(err, conventions.ErrNoConvention) {
		t.Fatalf("all kinds empty: error = %v, want ErrNoConvention\n%s", err, out)
	}

	f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore.")
	out, err = f.get(t, f.repo)
	if err != nil {
		t.Fatalf("one kind answered: %v\n%s", err, out)
	}
	for _, kind := range conventions.KindNames() {
		if !strings.Contains(out, "CONVENTION "+kind) {
			t.Errorf("kind %s did not print:\n%s", kind, out)
		}
	}
	if !strings.Contains(out, "TEAM-DOC") {
		t.Errorf("the answered kind did not render its layer:\n%s", out)
	}
	if !strings.Contains(out, "EMPTY") {
		t.Errorf("the empty kind is not marked as empty:\n%s", out)
	}
}

// TestRepoConventionsGetRefusesUnknownKind: the enum is closed, and a kind pop
// has never heard of is refused before anything is looked up or printed.
func TestRepoConventionsGetRefusesUnknownKind(t *testing.T) {
	f := newConventionFixture(t)
	f.userDefaults(t, "commits", "MY-DEFAULT: conventional commits.")

	out, err := f.get(t, f.repo, "bogus")
	if err == nil {
		t.Fatalf("unknown kind was accepted:\n%s", out)
	}
	if out != "" {
		t.Errorf("unknown kind printed a stack:\n%s", out)
	}
	for _, kind := range conventions.KindNames() {
		if !strings.Contains(err.Error(), kind) {
			t.Errorf("refusal %q does not list the known kind %q", err, kind)
		}
	}
}

// TestRepoConventionsMemoryIsSharedAcrossWorktrees is why the memory layer
// resolves through Repository identity rather than through the directory: a
// convention pop wrote from the trunk is the same convention in a worktree.
func TestRepoConventionsMemoryIsSharedAcrossWorktrees(t *testing.T) {
	f := newConventionFixture(t)
	worktree := filepath.Join(f.root, "feature-wt")
	runGitCheckout(t, f.repo, "worktree", "add", "-b", "feature", worktree)

	memoryPath := f.memory(t, f.repo, "commits", "POP-MEMORY: scopes are the package name.")
	if fromWorktree, err := conventions.MemoryPath(f.deps.conventionsDeps(), conventions.KindCommits, worktree); err != nil {
		t.Fatalf("resolve memory path from worktree: %v", err)
	} else if fromWorktree != memoryPath {
		t.Fatalf("worktree memory path = %q, want the trunk's %q", fromWorktree, memoryPath)
	}

	out, err := f.get(t, worktree, "commits")
	if err != nil {
		t.Fatalf("get from worktree: %v\n%s", err, out)
	}
	if !strings.Contains(out, "POP-MEMORY: scopes are the package name.") {
		t.Fatalf("worktree did not read the repository's memory layer:\n%s", out)
	}
}

// TestRepoConventionsRenderingLivesInTheDomainPackage guards ADR-0149's split:
// cmd wires and exits, the conventions package renders. Any Fprint here would
// be the first line of a second renderer.
func TestRepoConventionsRenderingLivesInTheDomainPackage(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "repo.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "fmt" {
			return true
		}
		if strings.HasPrefix(sel.Sel.Name, "Fprint") {
			t.Errorf("cmd/repo.go renders with fmt.%s; rendering belongs in the conventions package", sel.Sel.Name)
		}
		return true
	})
}
