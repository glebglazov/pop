package conventions

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// overlayResolveDeps is a real filesystem under a temp home, pointed at a
// throwaway git repository so ResolveOverlays can derive both ranks.
func overlayResolveDeps(t *testing.T) (d *Deps, repo, home string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	repo = filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	real := deps.NewRealFileSystem()
	d = &Deps{
		FS: &deps.MockFileSystem{
			UserHomeDirFunc: func() (string, error) { return home, nil },
			ReadFileFunc:    real.ReadFile,
			WriteFileFunc:   real.WriteFile,
			MkdirAllFunc:    real.MkdirAll,
			StatFunc:        real.Stat,
			RemoveAllFunc:   real.RemoveAll,
		},
	}
	return d, repo, home
}

func writeOverlayFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An Overlay resolves for a name that is not a Convention kind — `refine` is
// the reason the key is a string — and the repository rank is appended after
// the human's (ADR-0247).
func TestResolveOverlaysIsDocumentKeyedAndAppendsBothRanks(t *testing.T) {
	d, repo, home := overlayResolveDeps(t)

	userPath := filepath.Join(home, ".agents", "docs", "refine.overlay.md")
	repoPath := filepath.Join(repo, "docs", "agents", "refine.overlay.md")
	writeOverlayFile(t, userPath, "Never touch the generated client.\n")
	writeOverlayFile(t, repoPath, "Never rewrite migrations.\n")

	layers, err := ResolveOverlays(d, "refine", repo)
	if err != nil {
		t.Fatalf("ResolveOverlays: %v", err)
	}
	if len(layers) != 2 {
		t.Fatalf("ResolveOverlays = %d layers, want both ranks: %+v", len(layers), layers)
	}
	if layers[0].Origin != OriginOverlay || layers[0].Body != "Never touch the generated client." {
		t.Errorf("user overlay = %+v", layers[0])
	}
	if layers[1].Origin != OriginRepositoryOverlay || layers[1].Body != "Never rewrite migrations." {
		t.Errorf("repository overlay = %+v, want origin %q", layers[1], OriginRepositoryOverlay)
	}
	if !strings.Contains(layers[0].Path, "refine.overlay.md") || !strings.Contains(layers[1].Path, "refine.overlay.md") {
		t.Errorf("overlay paths = %q, %q", layers[0].Path, layers[1].Path)
	}

	prose := OverlayProse(layers)
	for _, want := range []string{
		"APPENDED: USER OVERLAY",
		"yours, appended to whichever answered",
		layers[0].Path,
		"Never touch the generated client.",
		"APPENDED: REPOSITORY OVERLAY",
		"the team's, appended to whichever answered",
		layers[1].Path,
		"Never rewrite migrations.",
	} {
		if !strings.Contains(prose, want) {
			t.Errorf("OverlayProse missing %q:\n%s", want, prose)
		}
	}
	if strings.Index(prose, "Never touch the generated client.") > strings.Index(prose, "Never rewrite migrations.") {
		t.Errorf("repository overlay must follow the user overlay:\n%s", prose)
	}
}

// A whitespace-only file at either Overlay rank reads as absent, matching the
// write-side refusal (ADR-0247).
func TestWhitespaceOverlayBodyIsAbsentAtBothRanks(t *testing.T) {
	d, repo, home := overlayResolveDeps(t)

	writeOverlayFile(t, filepath.Join(home, ".agents", "docs", "commits.overlay.md"), "  \n\t\n")
	writeOverlayFile(t, filepath.Join(repo, "docs", "agents", "commits.overlay.md"), "\n  \n")

	layers, err := ResolveOverlays(d, "commits", repo)
	if err != nil {
		t.Fatalf("ResolveOverlays: %v", err)
	}
	if len(layers) != 0 {
		t.Fatalf("whitespace overlays must be absent, got %+v", layers)
	}

	stack, err := Resolve(d, KindCommits, repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := stack.Overlays(); len(got) != 0 {
		t.Fatalf("stack.Overlays() = %+v, want none", got)
	}
}

// A convention stack appends both Overlay ranks beneath the answer, each
// labelled with its origin and reach.
func TestStackAppendsBothOverlayRanks(t *testing.T) {
	d, repo, home := overlayResolveDeps(t)

	writeOverlayFile(t, filepath.Join(home, ".agents", "docs", "commits.overlay.md"), "USER: never amend.\n")
	writeOverlayFile(t, filepath.Join(repo, "docs", "agents", "commits.overlay.md"), "TEAM: never force-push.\n")

	stack, err := Resolve(d, KindCommits, repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	prose := StackProse(stack)
	userIdx := strings.Index(prose, "USER: never amend.")
	repoIdx := strings.Index(prose, "TEAM: never force-push.")
	if userIdx < 0 || repoIdx < 0 || userIdx > repoIdx {
		t.Fatalf("both overlays must appear, user before team:\n%s", prose)
	}
	for _, want := range []string{
		"APPENDED: USER OVERLAY",
		"APPENDED: REPOSITORY OVERLAY",
		"yours, appended to whichever answered",
		"the team's, appended to whichever answered",
	} {
		if !strings.Contains(prose, want) {
			t.Errorf("prose missing %q:\n%s", want, prose)
		}
	}
	// The override marker is still the human's alone.
	if _, ok := stack.Overlay(); !ok {
		t.Fatal("Overlay() must report the human's rank for the dashboard marker")
	}
}
