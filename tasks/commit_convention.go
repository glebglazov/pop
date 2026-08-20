package tasks

import (
	"fmt"
	"strings"
)

// CommitConventionSource resolves this repository's Commit convention prose for
// the checkout at cwd. It is a seam for the same reason the Verifier's and the
// Reviewer's mandates are: the conventions package resolves Repository identity
// through this one, so tasks cannot reach it and cmd is the layer that holds
// both (see cmd.commitConvention).
type CommitConventionSource func(cwd string) (string, error)

// RecordCommitConvention writes the resolved Commit convention into the manifest
// of every set a register just activated (ADR-0228). The key is pop's projection
// of the stack rather than a claim about it: whatever a planning agent wrote
// there is replaced, because a hand-retyped copy of prose pop had already
// resolved is a copy nothing checks — one observed session dropped four clauses,
// including one of the human's own.
//
// Only the sets this register activated are written. A re-register rewrites
// nothing: registration is the moment the set becomes Work, and re-resolving a
// live set's convention would move prose under a drain that is already running.
//
// Per-task Planned commit subjects are untouched. Freezing them at planning time
// is what makes a drain reproducible; this key exists only for the agent that
// spawns a task mid-drain and has to render a new subject.
func RecordCommitConvention(d *Deps, result *RefreshResult, cwd string, resolve CommitConventionSource) error {
	if result == nil || resolve == nil || len(result.NewRegistrationIDs) == 0 {
		return nil
	}
	convention, err := resolve(cwd)
	if err != nil {
		return fmt.Errorf("resolve commit convention: %w", err)
	}
	convention = strings.TrimSpace(convention)
	if convention == "" {
		return nil
	}

	var failures []string
	for _, id := range result.NewRegistrationIDs {
		m := result.Manifests[id]
		// A manifest pop could not parse is one it must not rewrite: the write
		// projects parsed fields back, so an unparsed set would lose its tasks.
		if m == nil || !m.Valid || m.CommitConvention == convention {
			continue
		}
		m.CommitConvention = convention
		if err := WriteManifestAtomic(d, m); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", id, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("write commit convention: %s", strings.Join(failures, "; "))
	}
	return nil
}
