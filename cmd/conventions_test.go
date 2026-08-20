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
	"time"

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
	err := runConventionsGetWith(f.deps.conventionsDeps(), &out, dir, args)
	return out.String(), err
}

// TestConventionsGetResolvesToOneAnswerPlusTheOverlay is the whole point of the
// verb: one rank answers, the ranks it stood down are nowhere in the output, and
// the human's overlay rides along with whichever won.
func TestConventionsGetResolvesToOneAnswerPlusTheOverlay(t *testing.T) {
	f := newConventionFixture(t)

	defaultsPath := f.userDefaults(t, "commits", "MY-DEFAULT: conventional commits, lowercase subject.")
	f.memory(t, f.repo, "commits", `---
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

	// The human's own document outranks the team's, and both stand pop's memory
	// down: only one of the three is anywhere in the output.
	if !strings.Contains(out, "MY-DEFAULT") {
		t.Fatalf("the human's document did not answer:\n%s", out)
	}
	for _, stoodDown := range []string{"TEAM-DOC", "POP-MEMORY", repoPath} {
		if strings.Contains(out, stoodDown) {
			t.Errorf("a rank the answer stood down is still rendered (%q):\n%s", stoodDown, out)
		}
	}
	// The overlay appends rather than competing, so it is there beside the winner.
	if !strings.Contains(out, "MY-OVERLAY") {
		t.Errorf("the overlay was not appended to the answer:\n%s", out)
	}
	if strings.Index(out, "MY-DEFAULT") > strings.Index(out, "MY-OVERLAY") {
		t.Errorf("the overlay is printed before the answer it rides on:\n%s", out)
	}

	// Each block is labelled with both its role and the path it came from, which
	// is what lets a reader go and edit the right one.
	for label, path := range map[string]string{
		"ANSWER: USER DEFAULTS":  defaultsPath,
		"APPENDED: USER OVERLAY": overlayPath,
	} {
		if !strings.Contains(out, label) {
			t.Errorf("output does not carry the %s block:\n%s", label, out)
		}
		if !strings.Contains(out, path) {
			t.Errorf("output does not name the path %q:\n%s", path, out)
		}
	}

	// Nothing survives of the composed reading: no rule to reconcile, no numbering.
	for _, gone := range []string{"contradict", "compose", "LAYER 1 OF"} {
		if strings.Contains(out, gone) {
			t.Errorf("the composition rendering survives (%q):\n%s", gone, out)
		}
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "Provenance:") {
		t.Fatalf("output does not end with the provenance summary, got %q", last)
	}
	if !strings.Contains(last, "user defaults") || !strings.Contains(last, defaultsPath) {
		t.Errorf("provenance does not name what answered: %q", last)
	}
	if !strings.Contains(last, overlayPath) {
		t.Errorf("provenance does not say the overlay is appended: %q", last)
	}
}

// The memory layer answers only when neither document does, and its frontmatter
// is pop's bookkeeping rather than part of the convention: it comes back out as
// the provenance line's derivation clause.
func TestConventionsGetFallsBackToMemory(t *testing.T) {
	f := newConventionFixture(t)
	memoryPath := f.memory(t, f.repo, "commits", `---
derived_from: a sample of 40 commits
derived_at: 2026-08-01
---
POP-MEMORY: scopes are the package name.`)

	out, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}
	for _, want := range []string{"POP-MEMORY: scopes are the package name.", "ANSWER: POP MEMORY", memoryPath} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "derived_from:") {
		t.Errorf("memory frontmatter leaked into the rendered body:\n%s", out)
	}
	if !strings.Contains(out, "a sample of 40 commits") {
		t.Errorf("provenance does not disclose what pop memory was derived from:\n%s", out)
	}

	// A committed document is enough to stand it down entirely.
	f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore, docs.")
	out, err = f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}
	if !strings.Contains(out, "TEAM-DOC") {
		t.Fatalf("the repository's document did not answer:\n%s", out)
	}
	for _, gone := range []string{"POP-MEMORY", "a sample of 40 commits"} {
		if strings.Contains(out, gone) {
			t.Errorf("pop memory did not stand down for a written answer (%q):\n%s", gone, out)
		}
	}
}

// TestConventionsGetWithOnlyUserDefaults is the common shape on a machine
// where nothing repository-specific has been written yet.
func TestConventionsGetWithOnlyUserDefaults(t *testing.T) {
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
		t.Fatalf("provenance does not name the only layer as the answer:\n%s", out)
	}
}

// TestConventionsGetUnwrittenKindResolvesToTheShippedRank: a repository that
// has written nothing still gets rules to follow — pop's own answer, labelled
// an answer — and the command succeeds (ADR-0226 decision 1).
func TestConventionsGetUnwrittenKindResolvesToTheShippedRank(t *testing.T) {
	f := newConventionFixture(t)

	out, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("an unwritten kind is not a failure: %v\n%s", err, out)
	}
	for _, want := range []string{
		"CONVENTION commits",
		"ANSWER: SHIPPED",
		conventions.Shipped(conventions.KindCommits),
		"Provenance:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "METHOD") {
		t.Fatalf("a METHOD banner survives in `get` output:\n%s", out)
	}
}

// A document written at any rank stands pop's own answer down: the shipped rank
// is last, not a floor laid under whatever answered.
func TestConventionsGetDocumentOutranksTheShippedRank(t *testing.T) {
	f := newConventionFixture(t)
	path := f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore.")

	out, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}
	if !strings.Contains(out, "TEAM-DOC") || !strings.Contains(out, path) {
		t.Fatalf("the document is not in force:\n%s", out)
	}
	if strings.Contains(out, "METHOD") || strings.Contains(out, conventions.Shipped(conventions.KindCommits)) {
		t.Errorf("pop's own answer still prints beside a written one:\n%s", out)
	}
	// Asking for pop's own answer directly is still a real request when a
	// document answers: a human revising that document starts from pop's.
	shipped, err := defaultOut(t, "commits")
	if err != nil {
		t.Fatalf("default commits: %v\n%s", err, shipped)
	}
	if !strings.Contains(shipped, "SHIPPED CONVENTION commits") || !strings.Contains(shipped, conventions.Shipped(conventions.KindCommits)) {
		t.Errorf("the default verb does not answer while a document is in force:\n%s", shipped)
	}
}

// TestConventionsGetAllKinds walks every kind with no argument. Nothing written
// anywhere is still exit 0, because every kind resolves to pop's own answer.
func TestConventionsGetAllKinds(t *testing.T) {
	f := newConventionFixture(t)

	out, err := f.get(t, f.repo)
	if err != nil {
		t.Fatalf("nothing written anywhere: %v\n%s", err, out)
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
	// The kinds nobody wrote an answer for print pop's own instead, and no
	// banner survives anywhere in a whole-stack rendering.
	if !strings.Contains(out, conventions.Shipped(conventions.KindIssueTracker)) {
		t.Errorf("an unwritten kind did not fall through to the shipped rank:\n%s", out)
	}
	if strings.Contains(out, "METHOD") {
		t.Errorf("a METHOD banner survives in a whole-stack rendering:\n%s", out)
	}
}

// TestConventionsGetRefusesUnknownKind: the enum is closed, and a kind pop
// has never heard of is refused before anything is looked up or printed.
func TestConventionsGetRefusesUnknownKind(t *testing.T) {
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

// TestConventionsMemoryIsSharedAcrossWorktrees is why the memory layer
// resolves through Repository identity rather than through the directory: a
// convention pop wrote from the trunk is the same convention in a worktree.
func TestConventionsMemoryIsSharedAcrossWorktrees(t *testing.T) {
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

// TestConventionsRenderingLivesInTheDomainPackage guards ADR-0149's split:
// cmd wires and exits, the conventions package renders. Any Fprint here would
// be the first line of a second renderer.
func TestConventionsRenderingLivesInTheDomainPackage(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "conventions.go", nil, 0)
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
			t.Errorf("cmd/conventions.go renders with fmt.%s; rendering belongs in the conventions package", sel.Sel.Name)
		}
		return true
	})
}

func defaultOut(t *testing.T, kind string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runConventionsDefaultWith(&out, kind)
	return out.String(), err
}

// TestConventionsDefaultPrintsEachKindsShippedAnswer: the verb exists on its
// own because a human writing their own document starts from pop's, whether or
// not the kind already resolves to something else.
func TestConventionsDefaultPrintsEachKindsShippedAnswer(t *testing.T) {
	for _, kind := range conventions.KindNames() {
		out, err := defaultOut(t, kind)
		if err != nil {
			t.Fatalf("default %s: %v\n%s", kind, err, out)
		}
		if !strings.Contains(out, "SHIPPED CONVENTION "+kind) {
			t.Errorf("default %s is not printed as pop's own answer:\n%s", kind, out)
		}
		if strings.Contains(out, "METHOD") {
			t.Errorf("default %s prints a method banner:\n%s", kind, out)
		}
	}
}

// TestConventionsDefaultRefusesUnknownKind: the verb reads nothing off disk, so
// the closed enum is the only thing standing between a typo and empty output.
func TestConventionsDefaultRefusesUnknownKind(t *testing.T) {
	out, err := defaultOut(t, "bogus")
	if err == nil {
		t.Fatalf("unknown kind was accepted:\n%s", out)
	}
	if out != "" {
		t.Errorf("unknown kind printed an answer:\n%s", out)
	}
	for _, kind := range conventions.KindNames() {
		if !strings.Contains(err.Error(), kind) {
			t.Errorf("refusal %q does not list the known kind %q", err, kind)
		}
	}
}

func (f *conventionFixture) set(t *testing.T, dir, kind, body, derivedFrom string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runConventionsSetWith(f.deps.conventionsDeps(), &out, dir, kind, body, derivedFrom)
	return out.String(), err
}

func (f *conventionFixture) unset(t *testing.T, dir, kind string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runConventionsUnsetWith(f.deps.conventionsDeps(), &out, dir, kind)
	return out.String(), err
}

func (f *conventionFixture) memoryPath(t *testing.T, dir, kind string) string {
	t.Helper()
	path, err := conventions.MemoryPath(f.deps.conventionsDeps(), conventions.Kind(kind), dir)
	if err != nil {
		t.Fatalf("resolve memory path: %v", err)
	}
	return path
}

// TestConventionsSetRemembersWhatGetReadsBack is the slice: what an agent
// derived goes in at the memory rank, and comes back out of `get` as a layer
// whose provenance is the one recorded at write time.
func TestConventionsSetRemembersWhatGetReadsBack(t *testing.T) {
	f := newConventionFixture(t)
	f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore.")

	out, err := f.set(t, f.repo, "commits", "POP-MEMORY: scopes are the package name.\n", "the last 20 commits")
	if err != nil {
		t.Fatalf("set commits: %v\n%s", err, out)
	}
	path := f.memoryPath(t, f.repo, "commits")
	if !strings.Contains(out, path) {
		t.Errorf("set does not report where the convention landed:\n%s", out)
	}

	// The frontmatter is the reason the provenance line cannot desync from the
	// body: both are written by the same call, into the same file.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read memory file: %v", err)
	}
	stored := string(raw)
	if !strings.Contains(stored, "derived_from: the last 20 commits") {
		t.Errorf("memory file records no derivation source:\n%s", stored)
	}
	if !strings.Contains(stored, "derived_at: "+time.Now().Format("2006-01-02")) {
		t.Errorf("memory file records no write time:\n%s", stored)
	}
	if !strings.Contains(stored, "POP-MEMORY: scopes are the package name.") {
		t.Errorf("memory file does not hold the body:\n%s", stored)
	}

	// The team's committed document stands the fresh memory down: what pop just
	// remembered is not what a reader is handed.
	got, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, got)
	}
	if !strings.Contains(got, "TEAM-DOC") || strings.Contains(got, "POP-MEMORY") {
		t.Errorf("the memory layer did not stand down for the repository's document:\n%s", got)
	}

	// With no document to stand it down, it answers, frontmatter peeled off and
	// re-emitted as the provenance the write recorded.
	if err := os.Remove(filepath.Join(f.repo, "docs", "agents", "commits.md")); err != nil {
		t.Fatal(err)
	}
	got, err = f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, got)
	}
	if !strings.Contains(got, "POP-MEMORY: scopes are the package name.") {
		t.Errorf("the memory layer did not answer once nothing outranked it:\n%s", got)
	}
	if strings.Contains(got, "derived_from:") {
		t.Errorf("frontmatter leaked into the rendered body:\n%s", got)
	}
	last := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if line := last[len(last)-1]; !strings.Contains(line, "the last 20 commits") {
		t.Errorf("provenance does not quote the recorded derivation: %q", line)
	}
}

// TestConventionsSetReadsStdinOrFile: --file is an alias for stdin, so the
// body must be identical whichever way in the caller used.
func TestConventionsSetReadsStdinOrFile(t *testing.T) {
	f := newConventionFixture(t)
	setCmdLayerDeps(t, f.deps)
	body := "POP-MEMORY: subjects are imperative.\n"

	fromStdin, err := readConventionBody(strings.NewReader(body), "")
	if err != nil {
		t.Fatalf("read from stdin: %v", err)
	}
	file := f.write(t, filepath.Join(f.root, "derived.md"), body)
	fromFile, err := readConventionBody(strings.NewReader("WRONG: stdin must be ignored"), file)
	if err != nil {
		t.Fatalf("read from file: %v", err)
	}
	if fromStdin != body || fromFile != body {
		t.Fatalf("stdin gave %q and --file gave %q, want both %q", fromStdin, fromFile, body)
	}

	if out, err := f.set(t, f.repo, "commits", fromFile, "a file the agent wrote"); err != nil {
		t.Fatalf("set from file body: %v\n%s", err, out)
	}
	got, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, got)
	}
	if !strings.Contains(got, "POP-MEMORY: subjects are imperative.") {
		t.Fatalf("the file's body did not reach the memory layer:\n%s", got)
	}
}

// TestConventionsSetReplacesExistingMemory: pop holds one derivation per
// kind, so a second write is a correction rather than a second opinion.
func TestConventionsSetReplacesExistingMemory(t *testing.T) {
	f := newConventionFixture(t)
	if out, err := f.set(t, f.repo, "commits", "POP-MEMORY: first reading.", "5 commits"); err != nil {
		t.Fatalf("first set: %v\n%s", err, out)
	}
	out, err := f.set(t, f.repo, "commits", "POP-MEMORY: second reading.", "40 commits")
	if err != nil {
		t.Fatalf("second set: %v\n%s", err, out)
	}
	if !strings.Contains(out, "REPLACED") {
		t.Errorf("a write over an existing memory does not say it replaced one:\n%s", out)
	}

	got, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, got)
	}
	if strings.Contains(got, "first reading") {
		t.Errorf("the replaced body survived:\n%s", got)
	}
	if !strings.Contains(got, "second reading") || !strings.Contains(got, "40 commits") {
		t.Errorf("the replacing body or its provenance is missing:\n%s", got)
	}
}

// TestConventionsSetIsSharedAcrossWorktrees: the write is keyed by
// Repository identity, so a convention derived in a worktree is the
// repository's, not that directory's.
func TestConventionsSetIsSharedAcrossWorktrees(t *testing.T) {
	f := newConventionFixture(t)
	worktree := filepath.Join(f.root, "feature-wt")
	runGitCheckout(t, f.repo, "worktree", "add", "-b", "feature", worktree)

	if out, err := f.set(t, worktree, "commits", "POP-MEMORY: scopes are the package name.", "the last 20 commits"); err != nil {
		t.Fatalf("set from worktree: %v\n%s", err, out)
	}
	got, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get from trunk: %v\n%s", err, got)
	}
	if !strings.Contains(got, "POP-MEMORY: scopes are the package name.") {
		t.Fatalf("the trunk did not read what the worktree wrote:\n%s", got)
	}
}

// TestConventionsSetRefusals: everything pop declines to remember, and in
// each case it must have written nothing.
func TestConventionsSetRefusals(t *testing.T) {
	f := newConventionFixture(t)
	path := f.memoryPath(t, f.repo, "commits")

	out, err := f.set(t, f.repo, "bogus", "POP-MEMORY: anything.", "a sample")
	if err == nil {
		t.Fatalf("unknown kind was accepted:\n%s", out)
	}
	for _, kind := range conventions.KindNames() {
		if !strings.Contains(err.Error(), kind) {
			t.Errorf("refusal %q does not list the known kind %q", err, kind)
		}
	}

	if _, err := f.set(t, f.repo, "commits", "   \n", "a sample"); !errors.Is(err, conventions.ErrEmptyConvention) {
		t.Errorf("empty body: error = %v, want ErrEmptyConvention", err)
	}
	if _, err := f.set(t, f.repo, "commits", "POP-MEMORY: anything.", "  "); !errors.Is(err, conventions.ErrNoDerivation) {
		t.Errorf("missing derivation: error = %v, want ErrNoDerivation", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a refused set left a memory file at %s", path)
	}
}

// TestConventionsUnsetPromotesTheNextRank is why the verb prints more than
// "removed": memory was the answer, and removing it hands the kind to the
// repository's document — which the report names on the spot.
func TestConventionsUnsetPromotesTheNextRank(t *testing.T) {
	f := newConventionFixture(t)
	if out, err := f.set(t, f.repo, "commits", "POP-MEMORY: scopes are the package name.", "the last 20 commits"); err != nil {
		t.Fatalf("set commits: %v\n%s", err, out)
	}
	path := f.memoryPath(t, f.repo, "commits")

	// Before the removal, memory is what answers.
	before, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, before)
	}
	if !strings.Contains(before, "POP-MEMORY") {
		t.Fatalf("memory is not the answer to begin with:\n%s", before)
	}
	f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore.")

	out, err := f.unset(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("unset commits: %v\n%s", err, out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the memory file survived unset at %s", path)
	}
	if !strings.Contains(out, "REMOVED") || !strings.Contains(out, path) {
		t.Errorf("unset does not name what it removed:\n%s", out)
	}
	if !strings.Contains(out, "Now in force: repository (") ||
		!strings.Contains(out, filepath.Join("docs", "agents", "commits.md")) {
		t.Errorf("unset does not name the rank it promoted:\n%s", out)
	}
	if !strings.Contains(out, "TEAM-DOC: the type set is feat, fix, chore.") {
		t.Errorf("unset does not print what answers the kind now:\n%s", out)
	}
	if strings.Contains(out, "POP-MEMORY") {
		t.Errorf("the removed layer is still reported as in effect:\n%s", out)
	}
	if !strings.Contains(out, "Provenance:") || !strings.Contains(out, "repository") {
		t.Errorf("unset does not close on the provenance of what remains:\n%s", out)
	}
}

// TestConventionsUnsetWithoutMemory: the caller asked for a state that
// already holds, which is not a failure.
func TestConventionsUnsetWithoutMemory(t *testing.T) {
	f := newConventionFixture(t)

	out, err := f.unset(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("unset with nothing to remove: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing to remove") {
		t.Errorf("unset does not say there was no memory:\n%s", out)
	}
	if !strings.Contains(out, "Nobody answers commits now") || !strings.Contains(out, "ANSWER: SHIPPED") {
		t.Errorf("unset does not report what is in force after it:\n%s", out)
	}

	out, err = f.unset(t, f.repo, "bogus")
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
