package queue

import (
	"errors"
	"fmt"
	"os/exec"
	"time"
)

func computeMergeability(d *Deps, workingPath, runtimePath string) (MergeabilityRecord, error) {
	if d == nil || d.Tasks == nil || d.Tasks.Git == nil {
		return MergeabilityRecord{}, fmt.Errorf("missing git dependencies")
	}
	target, err := d.Tasks.Git.CommandInDir(workingPath, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return MergeabilityRecord{}, fmt.Errorf("resolve working HEAD: %w", err)
	}
	source, err := d.Tasks.Git.CommandInDir(runtimePath, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return MergeabilityRecord{}, fmt.Errorf("resolve runtime HEAD: %w", err)
	}
	status := MergeabilityClean
	if _, err := d.Tasks.Git.CommandInDir(workingPath, "merge-tree", "--write-tree", target, source); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return MergeabilityRecord{}, fmt.Errorf("dry-run merge-tree: %w", err)
		}
		status = MergeabilityConflicts
	}
	return MergeabilityRecord{
		RuntimePath: runtimePath,
		Status:      status,
		CheckedAt:   time.Now().UTC(),
		Target:      target,
		Source:      source,
	}, nil
}
