package routine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/frontmatter"
	"github.com/glebglazov/pop/internal/prompt/prompttest"
)

// The goldens below freeze what the Routine Prompt builders say today, byte for
// byte, so the template migration can be proved lossless by diff. Paths are
// fixed rather than temp-dir derived — the data dir is pinned through
// XDG_DATA_HOME and the checkout key is a hash of a literal path — because a
// golden holding a t.TempDir() path would differ on every run.

const (
	goldenDataHome = "/pop/data"
	goldenCheckout = "/pop/checkouts/demo"
)

func routineGoldenPath(name string) string {
	return filepath.Join("testdata", "prompts", name)
}

// routineFixtureFS serves one prompt.md at a fixed path and pins the data dir.
type routineFixtureFS struct {
	deps.MockFileSystem
	files map[string]string
}

func (f *routineFixtureFS) Getenv(key string) string {
	if key == "XDG_DATA_HOME" {
		return goldenDataHome
	}
	return ""
}

func (f *routineFixtureFS) ReadFile(path string) ([]byte, error) {
	if data, ok := f.files[filepath.Clean(path)]; ok {
		return []byte(data), nil
	}
	return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
}

// routineGoldenDeps serves the routine's prompt.md composed exactly as the
// write seam composes it, so create-mode detection sees the same body a real
// scaffolded routine has.
func routineGoldenDeps(t *testing.T, schedule, body string) *Deps {
	t.Helper()
	content, err := frontmatter.Marshal(frontmatter.Fields{Schedule: schedule}, body)
	if err != nil {
		t.Fatalf("compose prompt.md: %v", err)
	}
	return &Deps{FS: &routineFixtureFS{files: map[string]string{
		filepath.Join(goldenDataHome, "pop", "routines", "triage", promptFileName): content,
	}}}
}

func TestWrapRoutinePromptGolden(t *testing.T) {
	memoryDir := filepath.Join(goldenDataHome, "pop", "routines", "triage", memoryDirName)
	reportPath := filepath.Join(goldenDataHome, "pop", "routines", "triage", runsDirName, "2026-05-01T09-00-00Z.md")
	prompttest.Assert(t, routineGoldenPath("wrap.md"),
		wrapRoutinePrompt(memoryDir, reportPath, "# Daily triage\n\nReview open PRs assigned to me and summarize blockers.\n"))
}

func TestAuthoringPromptGoldens(t *testing.T) {
	// Create mode, with the unscheduled branch of the schedule line.
	create := &Routine{ID: "triage", Manifest: Manifest{BoundDirectory: goldenCheckout}}
	prompttest.Assert(t, routineGoldenPath("authoring.create.md"),
		buildAuthoringPrompt(routineGoldenDeps(t, "", promptStub), "triage", create))

	// Revise mode, with a schedule set.
	authored := "# Daily triage\n\nReview open PRs assigned to me and summarize blockers.\n"
	revise := &Routine{ID: "triage", Manifest: Manifest{BoundDirectory: goldenCheckout, Schedule: "every 6h"}}
	prompttest.Assert(t, routineGoldenPath("authoring.revise.md"),
		buildAuthoringPrompt(routineGoldenDeps(t, "every 6h", authored), "triage", revise))
}

func TestProjectAuthoringPromptGoldens(t *testing.T) {
	d := routineGoldenDeps(t, "", promptStub)

	create := &ProjectRoutine{Name: "triage", Dir: goldenCheckout, Prompt: promptStub}
	prompttest.Assert(t, routineGoldenPath("project-authoring.create.md"),
		buildProjectAuthoringPrompt(d, create))

	revise := &ProjectRoutine{Name: "triage", Dir: goldenCheckout,
		Prompt: "# Daily triage\n\nReview open PRs assigned to me and summarize blockers.\n"}
	prompttest.Assert(t, routineGoldenPath("project-authoring.revise.md"),
		buildProjectAuthoringPrompt(d, revise))
}

// knownRoutinePromptWarts records goldens that break the whitespace invariant
// the renderer's normalizer will enforce. This slice froze today's output
// verbatim, so a wart here is fixed in the slice that owns that prompt's text —
// never silently on the way into a golden.
var knownRoutinePromptWarts = map[string]string{}

func TestRoutinePromptGoldensSatisfyWhitespaceInvariant(t *testing.T) {
	prompttest.AssertGoldenWhitespace(t, filepath.Join("testdata", "prompts"), knownRoutinePromptWarts)
}
