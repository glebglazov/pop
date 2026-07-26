package routine

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/store"
)

// Handoff prints a Routine's continuation prompt to out using default deps.
func Handoff(id string, out io.Writer) error {
	return HandoffWith(defaultDeps, id, out)
}

// HandoffWith assembles a Routine's handoff prompt and writes it to out. The
// handoff is a prepared continuation prompt for a fresh agent session, built
// from the Routine's artifacts so the human can act on what the Routine has
// been collecting (ADR-0134). It bakes in no task of its own — the closing
// line hands control back to the user, who follows up with the actual task.
func HandoffWith(d *Deps, id string, out io.Writer) error {
	prompt, err := buildHandoff(d, id)
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, prompt)
	return err
}

// buildHandoff is the testable seam (deps-injected) that assembles the handoff
// prompt string. The dashboard verb (task 07) reuses it. Addressing follows
// ADR-0138: a `project:<name>` id (or a bare name that resolves to a Project
// routine) hands off the current checkout's Project routine, reading its
// per-checkout run history and memory.
func buildHandoff(d *Deps, id string) (string, error) {
	if resolvesToProjectRoutine(d, id) {
		return buildProjectHandoff(d, projectRoutineName(id))
	}
	if err := validateID(id); err != nil {
		return "", err
	}
	r, err := loadManifest(d, id)
	if err != nil {
		return "", err
	}

	dir := routineDir(d, id)
	// The handoff describes what the routine does, which is its prompt body — not
	// the settings frontmatter above the fence (ADR-0139).
	_, domainPrompt, err := readPromptFrontmatter(d, dir, id)
	if err != nil {
		return "", err
	}

	s, err := openExecutionStore(d)
	if err != nil {
		return "", err
	}
	run, runErr := s.LastRoutineRun(id)
	if runErr != nil {
		return "", fmt.Errorf("read last routine run: %w", runErr)
	}

	return assembleHandoff(handoffParts{
		displayID: id,
		prompt:    domainPrompt,
		memoryDir: filepath.Join(dir, memoryDirName),
		runsDir:   filepath.Join(dir, runsDirName),
		boundDir:  r.Manifest.BoundDirectory,
		lastRun:   run,
	}), nil
}

// buildProjectHandoff assembles the handoff prompt for a Project routine,
// reading its per-checkout run history and memory (ADR-0138).
func buildProjectHandoff(d *Deps, name string) (string, error) {
	pr, err := findProjectRoutine(d, name)
	if err != nil {
		return "", err
	}
	key := checkoutKey(pr.Dir)
	dataDir := projectRoutineDataDir(d, key, name)

	s, err := openExecutionStore(d)
	if err != nil {
		return "", err
	}
	run, runErr := s.LastRoutineRun(projectStoreID(key, name))
	if runErr != nil {
		return "", fmt.Errorf("read last routine run: %w", runErr)
	}

	return assembleHandoff(handoffParts{
		displayID: ProjectOrigin + name,
		prompt:    pr.Prompt,
		memoryDir: filepath.Join(dataDir, memoryDirName),
		runsDir:   filepath.Join(dataDir, runsDirName),
		boundDir:  pr.Dir,
		lastRun:   run,
	}), nil
}

// handoffParts is the resolved surface a handoff prompt is assembled from,
// independent of whether it came from an authored or a Project routine.
type handoffParts struct {
	displayID string
	prompt    string
	memoryDir string
	runsDir   string
	boundDir  string
	lastRun   *store.RoutineRun
}

// assembleHandoff renders the continuation prompt from resolved parts.
func assembleHandoff(p handoffParts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are picking up the work of the routine %q. What follows is context;\n", p.displayID)
	b.WriteString("the user will follow up with the task they want done.\n\n")

	b.WriteString("## What the routine does\n\n")
	b.WriteString(strings.TrimRight(p.prompt, "\n"))
	b.WriteString("\n\n")

	b.WriteString("## Latest run\n\n")
	if p.lastRun == nil {
		b.WriteString("No runs yet — this routine has not fired, so there is no run report to read.\n\n")
	} else {
		reportPath := p.lastRun.ReportPath
		if reportPath == "" {
			reportPath = reportPathForRun(p.runsDir, p.lastRun.FiredAt)
		}
		fmt.Fprintf(&b, "Read the latest run's report at %s\n", reportPath)
		fmt.Fprintf(&b, "Outcome: %s\n", p.lastRun.Outcome)
		if p.lastRun.Outcome == store.RoutineRunFailed && strings.TrimSpace(p.lastRun.FailReason) != "" {
			fmt.Fprintf(&b, "Fail reason: %s\n", p.lastRun.FailReason)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Routine memory\n\n")
	fmt.Fprintf(&b, "Read the routine memory directory at %s for accumulated context.\n\n", p.memoryDir)

	b.WriteString("## Where the work happens\n\n")
	fmt.Fprintf(&b, "The work happens in the bound directory %s\n\n", p.boundDir)

	b.WriteString("The user will now tell you what to do with this.\n")

	return b.String()
}
