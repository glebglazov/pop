package cmd

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/conventions"
	"github.com/glebglazov/pop/tasks"
	"github.com/spf13/cobra"
)

// conventionFixture is one repository with an isolated home and data dir, so a
// test can author any subset of the written ranks and read the stack back.
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

func (f *conventionFixture) globalDoc(t *testing.T, kind, body string) string {
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

func (f *conventionFixture) projectPath(t *testing.T, dir, kind string) string {
	t.Helper()
	path, err := conventions.ProjectPath(f.deps.conventionsDeps(), conventions.Kind(kind), dir)
	if err != nil {
		t.Fatalf("resolve project path: %v", err)
	}
	return path
}

// projectDoc writes the human's document for this project straight to the path
// the rank resolves to, for the tests that read rather than write.
func (f *conventionFixture) projectDoc(t *testing.T, dir, kind, body string) string {
	t.Helper()
	return f.write(t, f.projectPath(t, dir, kind), body)
}

// retiredMemoryPath is where pop used to write a convention for itself: one file
// per kind under the repository's Task storage directory. Nothing derives it any
// more, so a test that asserts it is never read has to spell it out.
func (f *conventionFixture) retiredMemoryPath(t *testing.T, kind string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(f.deps.tasksDeps(), f.repo)
	if err != nil {
		t.Fatalf("resolve repository identity: %v", err)
	}
	return filepath.Join(id.StorageDir, "conventions", kind+".md")
}

func (f *conventionFixture) get(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runConventionsGetWith(f.deps.conventionsDeps(), &out, dir, args)
	return out.String(), err
}

func (f *conventionFixture) repoOverlay(t *testing.T, kind, body string) string {
	t.Helper()
	return f.write(t, filepath.Join(f.repo, "docs", "agents", kind+".overlay.md"), body)
}

// TestConventionsGetResolvesToOneAnswerPlusTheOverlay is the whole point of the
// verb: one rank answers, the ranks it stood down are nowhere in the output, and
// the Overlay ranks ride along with whichever won.
func TestConventionsGetResolvesToOneAnswerPlusTheOverlay(t *testing.T) {
	f := newConventionFixture(t)

	projectPath := f.projectDoc(t, f.repo, "commits", "MY-PROJECT-DOC: scopes are the package name.")
	globalPath := f.globalDoc(t, "commits", "MY-GLOBAL-DOC: conventional commits, lowercase subject.")
	repoPath := f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore, docs.")
	overlayPath := f.overlay(t, "commits", "MY-OVERLAY: never mention the agent in the body.")
	repoOverlayPath := f.repoOverlay(t, "commits", "TEAM-OVERLAY: never force-push the trunk.")

	out, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}

	// The human's document for this project outranks their document for every
	// repository, and both outrank the team's: only one of the three is anywhere
	// in the output.
	if !strings.Contains(out, "MY-PROJECT-DOC") {
		t.Fatalf("the human's project document did not answer:\n%s", out)
	}
	for _, stoodDown := range []string{"MY-GLOBAL-DOC", "TEAM-DOC", globalPath, repoPath} {
		if strings.Contains(out, stoodDown) {
			t.Errorf("a rank the answer stood down is still rendered (%q):\n%s", stoodDown, out)
		}
	}
	// Both Overlay ranks append rather than competing, user before team.
	if !strings.Contains(out, "MY-OVERLAY") || !strings.Contains(out, "TEAM-OVERLAY") {
		t.Errorf("both overlays must be appended to the answer:\n%s", out)
	}
	if strings.Index(out, "MY-PROJECT-DOC") > strings.Index(out, "MY-OVERLAY") {
		t.Errorf("the overlay is printed before the answer it rides on:\n%s", out)
	}
	if strings.Index(out, "MY-OVERLAY") > strings.Index(out, "TEAM-OVERLAY") {
		t.Errorf("the repository overlay must follow the user overlay:\n%s", out)
	}

	// Each block is labelled with both its role and the path it came from, which
	// is what lets a reader go and edit the right one.
	for label, path := range map[string]string{
		"ANSWER: USER PROJECT":         projectPath,
		"APPENDED: USER OVERLAY":       overlayPath,
		"APPENDED: REPOSITORY OVERLAY": repoOverlayPath,
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
	if !strings.Contains(last, "user project") || !strings.Contains(last, projectPath) {
		t.Errorf("provenance does not name what answered: %q", last)
	}
	if !strings.Contains(last, overlayPath) || !strings.Contains(last, repoOverlayPath) {
		t.Errorf("provenance does not say both overlays are appended: %q", last)
	}
}

// TestConventionsGetRanksTheHumansTwoDocuments walks the middle of the stack:
// the global document answers where nothing more specific does, and the team's
// document answers where the human has written nothing at all.
func TestConventionsGetRanksTheHumansTwoDocuments(t *testing.T) {
	f := newConventionFixture(t)
	repoPath := f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore, docs.")

	out, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}
	if !strings.Contains(out, "TEAM-DOC") || !strings.Contains(out, repoPath) {
		t.Fatalf("the team's document did not answer:\n%s", out)
	}

	globalPath := f.globalDoc(t, "commits", "MY-GLOBAL-DOC: conventional commits.")
	out, err = f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}
	for _, want := range []string{"MY-GLOBAL-DOC", "ANSWER: USER GLOBAL", globalPath, "yours, every repository"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "TEAM-DOC") {
		t.Errorf("the team's document did not stand down for the human's:\n%s", out)
	}

	projectPath := f.projectDoc(t, f.repo, "commits", "MY-PROJECT-DOC: scopes are the package name.")
	out, err = f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}
	for _, want := range []string{"MY-PROJECT-DOC", "ANSWER: USER PROJECT", projectPath, "yours, this project"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "MY-GLOBAL-DOC") {
		t.Errorf("the human's global document did not stand down for their project one:\n%s", out)
	}
}

// TestConventionsGetNeverReadsTheRetiredMemoryRank: the rank pop wrote for
// itself is gone, and a file left where it used to live changes nothing about
// what resolves. Nothing deletes it, because a rank that stops being consulted
// needs no migration (ADR-0226 decision 5).
func TestConventionsGetNeverReadsTheRetiredMemoryRank(t *testing.T) {
	f := newConventionFixture(t)
	stale := f.write(t, f.retiredMemoryPath(t, "commits"), `---
derived_from: a sample of 40 commits
derived_at: 2026-08-01
---
POP-MEMORY: scopes are the package name.`)

	out, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ANSWER: SHIPPED") {
		t.Fatalf("a stale memory file changed what resolves:\n%s", out)
	}
	for _, gone := range []string{"POP-MEMORY", "pop memory", "a sample of 40 commits", stale} {
		if strings.Contains(out, gone) {
			t.Errorf("the retired rank still reaches the reader (%q):\n%s", gone, out)
		}
	}
	// The file is left alone: it is not pop's to delete.
	if _, err := os.Stat(stale); err != nil {
		t.Errorf("the file at the retired location was removed: %v", err)
	}

	// A written rank still answers with the stale file sitting there.
	f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore.")
	out, err = f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, out)
	}
	if !strings.Contains(out, "TEAM-DOC") || strings.Contains(out, "POP-MEMORY") {
		t.Errorf("resolution with a stale memory file present is not the team's document:\n%s", out)
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
	f.globalDoc(t, "commits", "MY-GLOBAL-DOC: conventional commits.")

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

// TestConventionsProjectRankIsKeyedByTheRemote is why the top rank is not keyed
// by Repository identity: the document is about the project, which outlives any
// one clone, so two checkouts of one remote read one file — and a repository
// nobody has published falls back to the identity-keyed location, there being
// nothing else to name it by.
func TestConventionsProjectRankIsKeyedByTheRemote(t *testing.T) {
	f := newConventionFixture(t)
	docs := filepath.Join(f.dataHome, "home", ".agents", "docs", "projects")

	// With no remote: keyed by the same name pop's own storage for the
	// repository carries.
	id, err := tasks.ResolveRepositoryIdentity(f.deps.tasksDeps(), f.repo)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.projectPath(t, f.repo, "commits"),
		filepath.Join(docs, filepath.Base(id.StorageDir), "commits.md"); got != want {
		t.Errorf("with no remote, project path = %q, want the identity-keyed %q", got, want)
	}

	runGitCheckout(t, f.repo, "remote", "add", "origin", "git@github.com:tripledot/github_dashboard.git")
	want := filepath.Join(docs, "github.com-tripledot-github_dashboard", "commits.md")
	if got := f.projectPath(t, f.repo, "commits"); got != want {
		t.Fatalf("project path = %q, want the remote-keyed %q", got, want)
	}

	// A second clone of the same remote, at another path on disk and so with
	// another Repository identity, reads the same document.
	clone := filepath.Join(f.root, "clone")
	runGitCheckout(t, f.root, "clone", f.repo, clone)
	runGitCheckout(t, clone, "remote", "set-url", "origin", "git@github.com:tripledot/github_dashboard.git")
	f.projectDoc(t, f.repo, "commits", "MY-PROJECT-DOC: scopes are the package name.")
	out, err := f.get(t, clone, "commits")
	if err != nil {
		t.Fatalf("get from the clone: %v\n%s", err, out)
	}
	if !strings.Contains(out, "MY-PROJECT-DOC") || !strings.Contains(out, want) {
		t.Fatalf("a second clone of the project did not read the project document:\n%s", out)
	}
}

// A worktree is the same project as its trunk however the rank is keyed, so a
// document written for the project answers in both.
func TestConventionsProjectRankIsSharedAcrossWorktrees(t *testing.T) {
	f := newConventionFixture(t)
	worktree := filepath.Join(f.root, "feature-wt")
	runGitCheckout(t, f.repo, "worktree", "add", "-b", "feature", worktree)

	path := f.projectDoc(t, f.repo, "commits", "MY-PROJECT-DOC: scopes are the package name.")
	if fromWorktree := f.projectPath(t, worktree, "commits"); fromWorktree != path {
		t.Fatalf("worktree project path = %q, want the trunk's %q", fromWorktree, path)
	}

	out, err := f.get(t, worktree, "commits")
	if err != nil {
		t.Fatalf("get from worktree: %v\n%s", err, out)
	}
	if !strings.Contains(out, "MY-PROJECT-DOC: scopes are the package name.") {
		t.Fatalf("worktree did not read the project's document:\n%s", out)
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

// set and unset name a rank the way the command line does: every write states
// which layer it lands in, so no fixture helper can hide one behind a default.
func (f *conventionFixture) set(t *testing.T, dir, kind, rank, body string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runConventionsSetWith(f.deps.conventionsDeps(), &out, dir, kind, rank, body)
	return out.String(), err
}

func (f *conventionFixture) unset(t *testing.T, dir, kind, rank string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runConventionsUnsetWith(f.deps.conventionsDeps(), &out, dir, kind, rank)
	return out.String(), err
}

// TestConventionsSetWritesWhatGetReadsBack is the round trip: what the human
// stated for this project lands at the top rank, comes back out of `get` as the
// answer, and stands the team's document down.
func TestConventionsSetWritesWhatGetReadsBack(t *testing.T) {
	f := newConventionFixture(t)
	f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore.")

	out, err := f.set(t, f.repo, "commits", "project", "MY-PROJECT-DOC: scopes are the package name.\n")
	if err != nil {
		t.Fatalf("set commits: %v\n%s", err, out)
	}
	path := f.projectPath(t, f.repo, "commits")
	if !strings.Contains(out, path) {
		t.Errorf("set does not report where the convention landed:\n%s", out)
	}

	// The file holds the human's prose and none of pop's bookkeeping: every
	// writable rank is their own statement (ADR-0226 decision 5).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read project document: %v", err)
	}
	stored := string(raw)
	if !strings.Contains(stored, "MY-PROJECT-DOC: scopes are the package name.") {
		t.Errorf("the file does not hold the body:\n%s", stored)
	}
	for _, gone := range []string{"---", "derived_from", "derived_at"} {
		if strings.Contains(stored, gone) {
			t.Errorf("the file carries pop's frontmatter (%q):\n%s", gone, stored)
		}
	}

	got, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, got)
	}
	if !strings.Contains(got, "MY-PROJECT-DOC: scopes are the package name.") {
		t.Fatalf("what was written is not what answers:\n%s", got)
	}
	if strings.Contains(got, "TEAM-DOC") {
		t.Errorf("the team's document was not stood down by the human's:\n%s", got)
	}
	if !strings.Contains(got, "ANSWER: USER PROJECT") {
		t.Errorf("the written rank is not labelled as the human's project document:\n%s", got)
	}
}

// TestConventionsSetReadsStdinOrFile: --file is an alias for stdin, so the
// body must be identical whichever way in the caller used.
func TestConventionsSetReadsStdinOrFile(t *testing.T) {
	f := newConventionFixture(t)
	setCmdLayerDeps(t, f.deps)
	body := "MY-PROJECT-DOC: subjects are imperative.\n"

	fromStdin, err := readConventionBody(strings.NewReader(body), "")
	if err != nil {
		t.Fatalf("read from stdin: %v", err)
	}
	file := f.write(t, filepath.Join(f.root, "stated.md"), body)
	fromFile, err := readConventionBody(strings.NewReader("WRONG: stdin must be ignored"), file)
	if err != nil {
		t.Fatalf("read from file: %v", err)
	}
	if fromStdin != body || fromFile != body {
		t.Fatalf("stdin gave %q and --file gave %q, want both %q", fromStdin, fromFile, body)
	}

	if out, err := f.set(t, f.repo, "commits", "project", fromFile); err != nil {
		t.Fatalf("set from file body: %v\n%s", err, out)
	}
	got, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, got)
	}
	if !strings.Contains(got, "MY-PROJECT-DOC: subjects are imperative.") {
		t.Fatalf("the file's body did not reach the project rank:\n%s", got)
	}
}

// TestConventionsSetReplacesWhatIsThere: the rank holds one document per kind,
// so a second write is a correction rather than a second opinion — and the
// report says so, a write that silently overwrote last month's statement being
// the surprise worth naming.
func TestConventionsSetReplacesWhatIsThere(t *testing.T) {
	f := newConventionFixture(t)
	if out, err := f.set(t, f.repo, "commits", "project", "MY-PROJECT-DOC: first reading."); err != nil {
		t.Fatalf("first set: %v\n%s", err, out)
	}
	out, err := f.set(t, f.repo, "commits", "project", "MY-PROJECT-DOC: second reading.")
	if err != nil {
		t.Fatalf("second set: %v\n%s", err, out)
	}
	if !strings.Contains(out, "REPLACED") {
		t.Errorf("a write over an existing document does not say it replaced one:\n%s", out)
	}

	got, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, got)
	}
	if strings.Contains(got, "first reading") {
		t.Errorf("the replaced body survived:\n%s", got)
	}
	if !strings.Contains(got, "second reading") {
		t.Errorf("the replacing body is missing:\n%s", got)
	}
}

// TestConventionsSetRefusals: everything pop declines to write, and in each
// case it must have written nothing.
func TestConventionsSetRefusals(t *testing.T) {
	f := newConventionFixture(t)
	path := f.projectPath(t, f.repo, "commits")

	out, err := f.set(t, f.repo, "bogus", "project", "MY-PROJECT-DOC: anything.")
	if err == nil {
		t.Fatalf("unknown kind was accepted:\n%s", out)
	}
	for _, kind := range conventions.KindNames() {
		if !strings.Contains(err.Error(), kind) {
			t.Errorf("refusal %q does not list the known kind %q", err, kind)
		}
	}

	if _, err := f.set(t, f.repo, "commits", "project", "   \n"); !errors.Is(err, conventions.ErrEmptyConvention) {
		t.Errorf("empty body: error = %v, want ErrEmptyConvention", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a refused set left a file at %s", path)
	}
}

// TestConventionsUnsetPromotesTheNextRank is why the verb prints more than
// "removed": the human's project document was the answer, and removing it hands
// the kind to the repository's document — which the report names on the spot.
func TestConventionsUnsetPromotesTheNextRank(t *testing.T) {
	f := newConventionFixture(t)
	if out, err := f.set(t, f.repo, "commits", "project", "MY-PROJECT-DOC: scopes are the package name."); err != nil {
		t.Fatalf("set commits: %v\n%s", err, out)
	}
	path := f.projectPath(t, f.repo, "commits")

	// Before the removal, the human's project document is what answers.
	before, err := f.get(t, f.repo, "commits")
	if err != nil {
		t.Fatalf("get commits: %v\n%s", err, before)
	}
	if !strings.Contains(before, "MY-PROJECT-DOC") {
		t.Fatalf("the project document is not the answer to begin with:\n%s", before)
	}
	f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore.")

	out, err := f.unset(t, f.repo, "commits", "project")
	if err != nil {
		t.Fatalf("unset commits: %v\n%s", err, out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the project document survived unset at %s", path)
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
	if strings.Contains(out, "MY-PROJECT-DOC") {
		t.Errorf("the removed layer is still reported as in effect:\n%s", out)
	}
	if !strings.Contains(out, "Provenance:") || !strings.Contains(out, "repository") {
		t.Errorf("unset does not close on the provenance of what remains:\n%s", out)
	}
}

// TestConventionsUnsetWithNothingWritten: the caller asked for a state that
// already holds, which is not a failure.
func TestConventionsUnsetWithNothingWritten(t *testing.T) {
	f := newConventionFixture(t)

	out, err := f.unset(t, f.repo, "commits", "project")
	if err != nil {
		t.Fatalf("unset with nothing to remove: %v\n%s", err, out)
	}
	if !strings.Contains(out, "nothing to remove") {
		t.Errorf("unset does not say there was nothing written:\n%s", out)
	}
	if !strings.Contains(out, "Nobody answers commits now") || !strings.Contains(out, "ANSWER: SHIPPED") {
		t.Errorf("unset does not report what is in force after it:\n%s", out)
	}

	out, err = f.unset(t, f.repo, "bogus", "project")
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

// rankPath is where each rank's document lives for this fixture, so a test can
// assert that a write landed at the rank it named and nowhere else.
func (f *conventionFixture) rankPath(t *testing.T, dir, kind, rank string) string {
	t.Helper()
	docs := filepath.Join(f.dataHome, "home", ".agents", "docs")
	switch rank {
	case "project":
		return f.projectPath(t, dir, kind)
	case "global":
		return filepath.Join(docs, kind+".md")
	case "overlay":
		return filepath.Join(docs, kind+".overlay.md")
	}
	t.Fatalf("no path for rank %q", rank)
	return ""
}

// TestConventionsSetWritesTheRankItWasGiven is what the rank argument buys: the
// same verb reaches three different files, each one named on the command line,
// and `get` reads back the one that was written.
func TestConventionsSetWritesTheRankItWasGiven(t *testing.T) {
	for _, tc := range []struct {
		rank  string
		body  string
		label string
	}{
		{"project", "MY-PROJECT-DOC: scopes are the package name.", "ANSWER: USER PROJECT"},
		{"global", "MY-GLOBAL-DOC: subjects are imperative.", "ANSWER: USER GLOBAL"},
		{"overlay", "MY-OVERLAY: never mention the agent.", "APPENDED: USER OVERLAY"},
	} {
		t.Run(tc.rank, func(t *testing.T) {
			f := newConventionFixture(t)
			path := f.rankPath(t, f.repo, "commits", tc.rank)

			out, err := f.set(t, f.repo, "commits", tc.rank, tc.body+"\n")
			if err != nil {
				t.Fatalf("set --%s: %v\n%s", tc.rank, err, out)
			}
			// The report names the rank, not just the file: a writer who thought
			// they were stating a note to self should be told which layer they
			// took over.
			if !strings.Contains(out, path) || !strings.Contains(out, tc.rank) {
				t.Errorf("set --%s does not report the rank and path it wrote:\n%s", tc.rank, out)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the %s rank: %v", tc.rank, err)
			}
			if strings.TrimSpace(string(raw)) != tc.body {
				t.Errorf("the %s rank holds %q, want %q", tc.rank, raw, tc.body)
			}

			got, err := f.get(t, f.repo, "commits")
			if err != nil {
				t.Fatalf("get commits: %v\n%s", err, got)
			}
			if !strings.Contains(got, tc.body) || !strings.Contains(got, tc.label) {
				t.Errorf("what was written at the %s rank is not what %s reports:\n%s", tc.rank, tc.label, got)
			}
		})
	}
}

// TestConventionsUnsetMirrorsSetAtEveryRank: the same three ranks, named the
// same way, and each removal reports what answers the kind afterwards.
func TestConventionsUnsetMirrorsSetAtEveryRank(t *testing.T) {
	for _, rank := range []string{"project", "global", "overlay"} {
		t.Run(rank, func(t *testing.T) {
			f := newConventionFixture(t)
			f.repoDoc(t, "commits", "TEAM-DOC: the type set is feat, fix, chore.")
			path := f.rankPath(t, f.repo, "commits", rank)
			if out, err := f.set(t, f.repo, "commits", rank, "MINE: written to be removed."); err != nil {
				t.Fatalf("set --%s: %v\n%s", rank, err, out)
			}

			out, err := f.unset(t, f.repo, "commits", rank)
			if err != nil {
				t.Fatalf("unset --%s: %v\n%s", rank, err, out)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("the %s rank survived unset at %s", rank, path)
			}
			if !strings.Contains(out, "REMOVED") || !strings.Contains(out, path) || !strings.Contains(out, rank) {
				t.Errorf("unset --%s does not name the rank and file it removed:\n%s", rank, out)
			}
			// Whatever the rank was, the report closes on what answers now — the
			// team's document, since that is all that is left.
			if !strings.Contains(out, "TEAM-DOC: the type set is feat, fix, chore.") ||
				!strings.Contains(out, "Now in force: repository (") {
				t.Errorf("unset --%s does not report what answers the kind now:\n%s", rank, out)
			}
			if strings.Contains(out, "MINE: written to be removed.") {
				t.Errorf("the removed layer is still reported as in effect:\n%s", out)
			}
		})
	}
}

// TestConventionsWriteRefusesWithoutARank: guessing would land authoritative
// prose at a rank the human did not choose, so a write that names none is
// refused with the ranks that exist — and nothing lands anywhere.
func TestConventionsWriteRefusesWithoutARank(t *testing.T) {
	f := newConventionFixture(t)

	for _, tc := range []struct {
		name string
		run  func() (string, error)
	}{
		{"set", func() (string, error) { return f.set(t, f.repo, "commits", "", "MINE: anything.") }},
		{"unset", func() (string, error) { return f.unset(t, f.repo, "commits", "") }},
	} {
		out, err := tc.run()
		if err == nil {
			t.Fatalf("%s with no rank was accepted:\n%s", tc.name, out)
		}
		for _, rank := range []string{"--project", "--global", "--overlay"} {
			if !strings.Contains(err.Error(), rank) {
				t.Errorf("%s refusal %q does not list the rank %s", tc.name, err, rank)
			}
		}
		if out != "" {
			t.Errorf("%s with no rank printed a report:\n%s", tc.name, out)
		}
	}
	for _, rank := range []string{"project", "global", "overlay"} {
		if path := f.rankPath(t, f.repo, "commits", rank); pathExists(path) {
			t.Errorf("a refused write left a file at the %s rank: %s", rank, path)
		}
	}
}

// TestConventionsWriteRefusesTheRepositoryRank: the team's document is a real
// rank and a reader who reaches for it gets the reason it is not written here,
// with its path — never an unknown-flag error.
func TestConventionsWriteRefusesTheRepositoryRank(t *testing.T) {
	f := newConventionFixture(t)

	for _, tc := range []struct {
		name string
		run  func() (string, error)
	}{
		{"set", func() (string, error) { return f.set(t, f.repo, "commits", "repository", "MINE: anything.") }},
		{"unset", func() (string, error) { return f.unset(t, f.repo, "commits", "repository") }},
	} {
		out, err := tc.run()
		if err == nil {
			t.Fatalf("%s --repository was accepted:\n%s", tc.name, out)
		}
		for _, want := range []string{filepath.Join("docs", "agents", "commits.md"), "version control", "diff"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s --repository refusal %q does not say %q", tc.name, err, want)
			}
		}
	}
	if path := filepath.Join(f.repo, "docs", "agents", "commits.md"); pathExists(path) {
		t.Errorf("a refused write created the team's document at %s", path)
	}
}

// A body that reads as an absent layer is refused wherever it was aimed: one
// reader reads every rank, so one refusal covers them all.
func TestConventionsSetRefusesAnEmptyBodyAtEveryRank(t *testing.T) {
	f := newConventionFixture(t)

	for _, rank := range []string{"project", "global", "overlay"} {
		if _, err := f.set(t, f.repo, "commits", rank, "  \n\t\n"); !errors.Is(err, conventions.ErrEmptyConvention) {
			t.Errorf("set --%s with an empty body = %v, want ErrEmptyConvention", rank, err)
		}
		if path := f.rankPath(t, f.repo, "commits", rank); pathExists(path) {
			t.Errorf("a refused body left a file at the %s rank: %s", rank, path)
		}
	}
}

// TestConventionsWriteVerbsShareOneRankSurface: both verbs carry the same four
// switches, at most one at a time, and the repository one is registered so that
// naming it reaches pop's reason rather than cobra's unknown-flag error.
func TestConventionsWriteVerbsShareOneRankSurface(t *testing.T) {
	for _, cmd := range []*cobra.Command{conventionsSetCmd, conventionsUnsetCmd} {
		for _, rank := range []string{"project", "global", "overlay", "repository"} {
			if cmd.Flags().Lookup(rank) == nil {
				t.Errorf("%s has no --%s flag", cmd.Name(), rank)
			}
		}
	}

	// Two ranks at once is as much a guess as none, so the surface refuses it
	// before any body is read.
	probe := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }}
	probe.SetOut(io.Discard)
	probe.SetErr(io.Discard)
	flags := rankFlags{}
	addRankFlags(probe, flags)
	probe.SetArgs([]string{"--project", "--global"})
	if err := probe.Execute(); err == nil {
		t.Error("two ranks at once was accepted; a write must name exactly one")
	}

	// And the rank a caller named is the one the verb is handed — with none
	// named, the empty string, which the conventions package refuses.
	if got := flags.named(); got != "project" && got != "global" {
		t.Errorf("named() = %q after --project --global, want one of the two", got)
	}
	if got := (rankFlags{}).named(); got != "" {
		t.Errorf("named() with nothing set = %q, want the empty string", got)
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
