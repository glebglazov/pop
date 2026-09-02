package tasks

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/prompt/prompttest"
)

// The goldens below freeze what every Task-set Prompt builder says today, byte
// for byte, so the template migration can be proved lossless by diff. They are
// the oracle, not a second set of assertions: the substring tests beside them
// keep saying which facts must reach the agent, and survive reflow.
//
// Every fixture is deterministic — a fake filesystem at fixed absolute paths, a
// stub git — because a golden holding a t.TempDir() path would differ on every
// run.

const (
	goldenSetDir      = "/pop/tasks/2026-05-01-demo"
	goldenRuntimePath = "/pop/checkouts/demo"
	goldenSetID       = "2026-05-01-demo"
)

func goldenPath(name string) string {
	return filepath.Join("testdata", "prompts", name)
}

// promptFixtureFS is a read-only filesystem over a fixed file map. It answers
// ReadFile, ReadDir and Stat — the three reads the prompt builders make through
// the Deps seam — so a fixture can name absolute paths that exist nowhere.
type promptFixtureFS struct {
	deps.MockFileSystem
	files map[string]string
}

func (f *promptFixtureFS) ReadFile(path string) ([]byte, error) {
	if data, ok := f.files[filepath.Clean(path)]; ok {
		return []byte(data), nil
	}
	return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
}

func (f *promptFixtureFS) Stat(path string) (os.FileInfo, error) {
	clean := filepath.Clean(path)
	if data, ok := f.files[clean]; ok {
		return deps.MockFileInfo{NameVal: filepath.Base(clean), SizeVal: int64(len(data))}, nil
	}
	if f.isDir(clean) {
		return deps.MockFileInfo{NameVal: filepath.Base(clean), IsDirVal: true, ModeVal: fs.ModeDir}, nil
	}
	return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrNotExist}
}

func (f *promptFixtureFS) ReadDir(path string) ([]os.DirEntry, error) {
	clean := filepath.Clean(path)
	if !f.isDir(clean) {
		return nil, &os.PathError{Op: "readdir", Path: path, Err: os.ErrNotExist}
	}
	seen := map[string]bool{}
	var entries []os.DirEntry
	for file := range f.files {
		rest, ok := strings.CutPrefix(file, clean+string(filepath.Separator))
		if !ok {
			continue
		}
		name, _, isDir := strings.Cut(rest, string(filepath.Separator))
		if seen[name] {
			continue
		}
		seen[name] = true
		entries = append(entries, deps.MockDirEntry{NameVal: name, IsDirVal: isDir})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func (f *promptFixtureFS) isDir(clean string) bool {
	prefix := clean + string(filepath.Separator)
	for file := range f.files {
		if strings.HasPrefix(file, prefix) {
			return true
		}
	}
	return false
}

// unreadableFS fails every read, which is the branch each body-inlining prompt
// takes when the task file cannot be read.
type unreadableFS struct{ deps.MockFileSystem }

func (unreadableFS) ReadFile(path string) ([]byte, error) {
	return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
}

// goldenFixtureDeps is the populated set: task bodies, a spec, progress records
// and one recorded failure reason, all at fixed paths.
func goldenFixtureDeps(t *testing.T) *Deps {
	t.Helper()
	files := map[string]string{
		filepath.Join(goldenSetDir, "01-afk.md"): "## What to build\n\nFreeze every prompt behind a golden.\n\n## Acceptance criteria\n\n- [x] a golden per prompt\n",
		filepath.Join(goldenSetDir, "02-remediation.md"): "## What to build\n\nWiden the commit range the Verifier reads.\n\n## Acceptance criteria\n\n- [x] the range starts at the recorded base\n",
		filepath.Join(goldenSetDir, "03-hitl.md"):        "## Review\n\nRead the goldens and confirm nothing moved.\n\n## Acceptance criteria\n\n- [ ] approved\n",
		filepath.Join(goldenSetDir, "04-afk.md"):         "## What to build\n\nMigrate the builders onto templates.\n\n## Acceptance criteria\n\n- [ ] every builder renders through the seam\n",
		filepath.Join(goldenSetDir, "spec.md"):           "# Prompt templates\n\nThe ten agent prompts become embedded markdown templates.\n",
		filepath.Join(goldenSetDir, "progress.txt"): "2026-05-01T09:00:00Z [01-afk.md] DONE\ncaptured a golden for each builder\nasserted the whitespace invariant\n" +
			"---\n" +
			"2026-05-01T10:00:00Z [02-remediation.md] DONE\nwidened the range to the recorded base\n" +
			"---\n" +
			"2026-05-01T11:00:00Z [04-afk.md] FAILED\nleft an acceptance box unticked\n" +
			"---\n",
	}
	for path, data := range seededFailureStream(t) {
		files[path] = data
	}
	return &Deps{FS: &promptFixtureFS{files: files}, Git: stubGit("shaHEAD\n", "", " tasks/prompt.go | 12 ++++----\n 1 file changed\n")}
}

// seededFailureStream produces the Captured attempt stream that LatestFailureReason
// reads, by writing one through the real recorder into a temp dir and lifting the
// bytes onto the fixture's fixed paths.
func seededFailureStream(t *testing.T) map[string]string {
	t.Helper()
	tmp := t.TempDir()
	streamDir := taskStreamDir(tmp, "04-afk.md")
	writeTimingStreamRecords(t, streamDir, "attempt-001.jsonl.gz",
		streamHeaderRecord{Type: "header", Agent: "claude", Attempt: 1, StartTime: time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC)},
		[]streamEventRecord{{Type: "event", AtMS: 5, Raw: `{"type":"system"}`}},
		streamFooterRecord{Type: "footer", Outcome: streamOutcomeFailed, DurationMS: 1000, Reason: "left an acceptance box unticked", ExitCode: 0})

	files := map[string]string{}
	err := filepath.WalkDir(tmp, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(tmp, path)
		if err != nil {
			return err
		}
		files[filepath.Join(goldenSetDir, rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("seed failure stream: %v", err)
	}
	return files
}

// goldenFullManifest is the set with every optional manifest fact present:
// titles, blockers, effort, a recorded base and commits, a Commit convention,
// and a done Remediation task.
func goldenFullManifest() *Manifest {
	return &Manifest{
		Stem:               goldenSetID,
		Dir:                goldenSetDir,
		Valid:              true,
		BaseCommit:         "base000",
		BaseCommitRecorded: true,
		CommitConvention:   "tasks(<set-slug>): <task-id> — imperative, lower case, no trailing period.",
		Tasks: []Task{
			{ID: "01-afk", File: "01-afk.md", Title: "Freeze the prompts", Type: "AFK", Status: TaskDone, Effort: "standard",
				Commit: &TaskCommit{SHA: "c0ffee1", Subject: "tasks(prompt-templates): 01-freeze-current-prompts"}},
			{ID: "02-remediation", File: "02-remediation.md", Title: "Remediation 1: widen the range", Type: "AFK", Status: TaskDone, Effort: "heavy", BlockedBy: []string{"01-afk"}},
			{ID: "03-hitl", File: "03-hitl.md", Title: "Review the goldens", Type: "HITL", Status: TaskOpen, Effort: "standard", BlockedBy: []string{"01-afk", "02-remediation"}},
			{ID: "04-afk", File: "04-afk.md", Title: "Migrate the templates", Type: "AFK", Status: TaskFailed, Effort: "standard", BlockedBy: []string{"01-afk"}},
		},
	}
}

// goldenBareManifest is the same set stripped to what a manifest must carry:
// no titles, no blockers, no base, no convention, one open task.
func goldenBareManifest() *Manifest {
	return &Manifest{
		Stem:  goldenSetID,
		Dir:   goldenSetDir,
		Valid: true,
		Tasks: []Task{
			{ID: "01-afk", File: "01-afk.md", Type: "AFK", Status: TaskOpen},
		},
	}
}

// bareDeps reads nothing successfully: no progress.txt, no spec, no attempt
// stream — the absent-everything side of each optional section.
func bareDeps() *Deps {
	return &Deps{FS: &promptFixtureFS{files: map[string]string{}}, Git: stubGit("", "", "")}
}

func TestAgentPromptGoldens(t *testing.T) {
	// Absent side: the default, where [work.implement].include_implementation_convention
	// is off and the builder's prompt reads as it did before the toggle existed.
	prompttest.Assert(t, goldenPath("agent.md"),
		BuildAgentPrompt(filepath.Join(goldenSetDir, "04-afk.md"), goldenRuntimePath, ""))

	prompttest.Assert(t, goldenPath("agent.implementation-convention.md"),
		BuildAgentPrompt(filepath.Join(goldenSetDir, "04-afk.md"), goldenRuntimePath, goldenImplementationConvention))
}

// goldenImplementationConvention stands in for what the `implementation`
// Convention stack renders: labelled blocks and a provenance line, and no
// Read-whole notice — that notice belongs to the command paths a human reads
// (ADR-0230).
const goldenImplementationConvention = `----- ANSWER: SHIPPED (pop's own) -----
conventions/shipped/implementation.md

Name things after what they are in this repository's language.
Keep a function's abstraction level uniform.

Rules: pop's own (shipped). No project or overlay document for this kind.`

func TestHITLAssistancePromptGoldens(t *testing.T) {
	full := goldenFullManifest()
	prompttest.Assert(t, goldenPath("hitl-assistance.full.md"),
		BuildHITLAssistancePrompt(goldenFixtureDeps(t), goldenSetID, full, full.Tasks[2], goldenRuntimePath))

	// Absent side: no title on the blocking task, no runtime checkout, an
	// unreadable body, and no completed AFK work.
	bare := goldenBareManifest()
	prompttest.Assert(t, goldenPath("hitl-assistance.bare.md"),
		BuildHITLAssistancePrompt(&Deps{FS: &unreadableFS{}}, goldenSetID, bare, bare.Tasks[0], ""))
}

func TestFailedAssistancePromptGoldens(t *testing.T) {
	full := goldenFullManifest()
	prompttest.Assert(t, goldenPath("failed-assistance.full.md"),
		BuildFailedAssistancePrompt(goldenFixtureDeps(t), goldenSetID, full, full.Tasks[3], goldenRuntimePath))

	bare := goldenBareManifest()
	prompttest.Assert(t, goldenPath("failed-assistance.bare.md"),
		BuildFailedAssistancePrompt(bareDeps(), goldenSetID, bare, bare.Tasks[0], ""))
}

func TestVerifyFailedAssistancePromptGoldens(t *testing.T) {
	full := goldenFullManifest()
	prompttest.Assert(t, goldenPath("verify-failed-assistance.full.md"),
		BuildVerifyFailedAssistancePrompt(goldenFixtureDeps(t), goldenSetID, full, "shaHEAD",
			"01-afk: the golden for the Assist prompt is missing.", goldenRuntimePath))

	bare := goldenBareManifest()
	prompttest.Assert(t, goldenPath("verify-failed-assistance.bare.md"),
		BuildVerifyFailedAssistancePrompt(bareDeps(), goldenSetID, bare, "", "", ""))

	// The third state of the diff section: a set that demonstrably committed
	// something whose range this checkout cannot name.
	undetermined := goldenFullManifest()
	d := goldenFixtureDeps(t)
	d.Git = &deps.MockGit{CommandInDirFunc: func(string, ...string) (string, error) {
		return "", &os.PathError{Op: "git", Path: goldenRuntimePath, Err: os.ErrNotExist}
	}}
	prompttest.Assert(t, goldenPath("verify-failed-assistance.undetermined.md"),
		BuildVerifyFailedAssistancePrompt(d, goldenSetID, undetermined, "shaHEAD", "the range moved under us", goldenRuntimePath))
}

func TestInterruptAssistancePromptGoldens(t *testing.T) {
	full := goldenFullManifest()
	prompttest.Assert(t, goldenPath("interrupt-assistance.full.md"),
		BuildInterruptAssistancePrompt(goldenFixtureDeps(t), goldenSetID, full, full.Tasks[3], goldenRuntimePath))

	bare := goldenBareManifest()
	prompttest.Assert(t, goldenPath("interrupt-assistance.bare.md"),
		BuildInterruptAssistancePrompt(&Deps{FS: &unreadableFS{}}, goldenSetID, bare, bare.Tasks[0], ""))
}

func TestAssistPromptGoldens(t *testing.T) {
	full := goldenFullManifest()
	prompttest.Assert(t, goldenPath("assist.full.md"),
		BuildAssistPrompt(goldenFixtureDeps(t), goldenSetID, full, StatusFailed, goldenRuntimePath,
			"01-afk: the golden for the Assist prompt is missing."))

	bare := goldenBareManifest()
	prompttest.Assert(t, goldenPath("assist.bare.md"),
		BuildAssistPrompt(bareDeps(), goldenSetID, bare, StatusReady, "", ""))

	// Progress has a third state between "records" and "no file": a file that
	// parses to nothing.
	empty := &Deps{FS: &promptFixtureFS{files: map[string]string{filepath.Join(goldenSetDir, "progress.txt"): "\n"}}, Git: stubGit("", "", "")}
	prompttest.Assert(t, goldenPath("assist.empty-progress.md"),
		BuildAssistPrompt(empty, goldenSetID, bare, StatusReady, "", ""))
}

func TestVerifierPromptGoldens(t *testing.T) {
	full := goldenFullManifest()
	prompttest.Assert(t, goldenPath("verifier.full.md"),
		buildVerifierPrompt(goldenFixtureDeps(t), full, "shaHEAD",
			workDiffView{Range: "base000..HEAD", Stat: " tasks/prompt.go | 12 ++++----\n 1 file changed"},
			"the retry cap is deliberate — it bounds one attempt, not the drain.",
			"Run `make test` before believing the work.\n\nfrom pop's shipped answer (nobody wrote one above it)"))

	prompttest.Assert(t, goldenPath("verifier.bare.md"),
		buildVerifierPrompt(bareDeps(), goldenBareManifest(), "", workDiffView{}, "", ""))
}

func TestRefinerPromptGoldens(t *testing.T) {
	prompttest.Assert(t, goldenPath("refiner.full.md"),
		buildRefinerPrompt(goldenFixtureDeps(t), goldenFullManifest(),
			workDiffView{Range: "base000..HEAD", Stat: " tasks/prompt.go | 12 ++++----\n 1 file changed"},
			"CONVENTION refine\n\nSmall functions; table-driven tests.", "",
			passDocument{Path: filepath.Join(goldenSetDir, "refine", "refine-20260501T090000Z.md"),
				Body: "## Naming\n\n`buildThing` builds nothing."}, true))

	// Absent side: no convention derived, no report before this one, no spec.
	prompttest.Assert(t, goldenPath("refiner.bare.md"),
		buildRefinerPrompt(bareDeps(), goldenBareManifest(),
			workDiffView{Range: "root000..HEAD", Stat: " a.go | 1 +"}, "", "", passDocument{}, false))
}

func TestFoldConflictPromptGoldens(t *testing.T) {
	ctx := FoldConflictContext{
		SetID:       goldenSetID,
		Manifest:    goldenFullManifest(),
		RuntimePath: goldenRuntimePath,
		SetBranch:   "pop/2026-05-01-demo",
		TrunkBranch: "master",
		TrunkPath:   "/pop/checkouts/trunk",
	}
	prompttest.Assert(t, goldenPath("fold-conflict.full.md"),
		BuildFoldConflictPrompt(goldenFixtureDeps(t), ctx, []string{"tasks/prompt.go", "tasks/verify.go"}))

	// Absent side: no manifest to describe the work, and no conflicted paths
	// listed yet.
	bare := FoldConflictContext{
		SetID:       goldenSetID,
		RuntimePath: goldenRuntimePath,
		SetBranch:   "pop/2026-05-01-demo",
		TrunkBranch: "master",
		TrunkPath:   "/pop/checkouts/trunk",
	}
	prompttest.Assert(t, goldenPath("fold-conflict.bare.md"),
		BuildFoldConflictPrompt(bareDeps(), bare, nil))
}

// knownTaskPromptWarts records goldens that break the whitespace invariant the
// renderer's normalizer will enforce. This slice froze today's output verbatim,
// so a wart here is fixed in the slice that owns that prompt's text — never
// silently on the way into a golden.
var knownTaskPromptWarts = map[string]string{}

func TestTaskPromptGoldensSatisfyWhitespaceInvariant(t *testing.T) {
	prompttest.AssertGoldenWhitespace(t, filepath.Join("testdata", "prompts"), knownTaskPromptWarts)
}
