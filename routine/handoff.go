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
// prompt string. The dashboard verb (task 07) reuses it.
func buildHandoff(d *Deps, id string) (string, error) {
	if err := validateID(id); err != nil {
		return "", err
	}
	r, err := loadManifest(d, id)
	if err != nil {
		return "", err
	}

	dir := routineDir(d, id)
	promptPath := filepath.Join(dir, promptFileName)
	domainPrompt, err := d.FS.ReadFile(promptPath)
	if err != nil {
		return "", fmt.Errorf("read routine prompt: %w", err)
	}
	memoryDir := filepath.Join(dir, memoryDirName)

	s, err := openExecutionStore(d)
	if err != nil {
		return "", err
	}
	run, runErr := s.LastRoutineRun(id)
	_ = s.Close()
	if runErr != nil {
		return "", fmt.Errorf("read last routine run: %w", runErr)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "You are picking up the work of the routine %q. What follows is context;\n", id)
	b.WriteString("the user will follow up with the task they want done.\n\n")

	b.WriteString("## What the routine does\n\n")
	b.WriteString(strings.TrimRight(string(domainPrompt), "\n"))
	b.WriteString("\n\n")

	b.WriteString("## Latest run\n\n")
	if run == nil {
		b.WriteString("No runs yet — this routine has not fired, so there is no run report to read.\n\n")
	} else {
		reportPath := run.ReportPath
		if reportPath == "" {
			reportPath = reportPathForRun(filepath.Join(dir, runsDirName), run.FiredAt)
		}
		fmt.Fprintf(&b, "Read the latest run's report at %s\n", reportPath)
		fmt.Fprintf(&b, "Outcome: %s\n", run.Outcome)
		if run.Outcome == store.RoutineRunFailed && strings.TrimSpace(run.FailReason) != "" {
			fmt.Fprintf(&b, "Fail reason: %s\n", run.FailReason)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Routine memory\n\n")
	fmt.Fprintf(&b, "Read the routine memory directory at %s for accumulated context.\n\n", memoryDir)

	b.WriteString("## Where the work happens\n\n")
	fmt.Fprintf(&b, "The work happens in the bound directory %s\n\n", r.Manifest.BoundDirectory)

	b.WriteString("The user will now tell you what to do with this.\n")

	return b.String(), nil
}
