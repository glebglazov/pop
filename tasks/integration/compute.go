package integration

import (
	"fmt"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// Compute dry-runs merging a completed runtime branch into the working checkout
// and returns a mergeability record without persisting it. It records the
// working checkout and both HEADs so a later reconcile can SHA-gate
// recomputation (ADR-0055).
func Compute(td *tasks.Deps, workingPath, runtimePath string) (Record, error) {
	if td == nil || td.Git == nil {
		return Record{}, fmt.Errorf("missing git dependencies")
	}
	verdict, base, branch, err := tasks.ComputeMergeVerdict(td, workingPath, runtimePath)
	if err != nil {
		return Record{}, err
	}
	return Record{
		RuntimePath: runtimePath,
		WorkingPath: workingPath,
		Status:      verdict,
		CheckedAt:   time.Now().UTC(),
		Target:      base,
		Source:      branch,
	}, nil
}
