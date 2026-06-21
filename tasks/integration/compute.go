package integration

import (
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// Compute dry-runs merging a completed runtime branch into the working checkout
// and returns a mergeability record without persisting it.
func Compute(td *tasks.Deps, workingPath, runtimePath string) (Record, error) {
	if td == nil || td.Git == nil {
		return Record{}, fmt.Errorf("missing git dependencies")
	}
	target, err := td.Git.CommandInDir(workingPath, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Record{}, fmt.Errorf("resolve working HEAD: %w", err)
	}
	source, err := td.Git.CommandInDir(runtimePath, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Record{}, fmt.Errorf("resolve runtime HEAD: %w", err)
	}
	status := StatusClean
	if _, err := td.Git.CommandInDir(workingPath, "merge-tree", "--write-tree", target, source); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return Record{}, fmt.Errorf("dry-run merge-tree: %w", err)
		}
		status = StatusConflicts
	}
	return Record{
		RuntimePath: runtimePath,
		Status:      status,
		CheckedAt:   time.Now().UTC(),
		Target:      target,
		Source:      source,
	}, nil
}
