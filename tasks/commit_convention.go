package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
)

// manifestCommitConventionKey is the set-level manifest key pop projects the
// resolved Commit convention into.
const manifestCommitConventionKey = "commit_convention"

// CommitConventionSource resolves this repository's Commit convention prose for
// the checkout at cwd. It is a seam for the same reason the Verifier's and the
// Refiner's mandates are: the conventions package resolves Repository identity
// through this one, so tasks cannot reach it and cmd is the layer that holds
// both (see cmd.commitConvention).
type CommitConventionSource func(cwd string) (string, error)

// RecordCommitConvention writes the resolved Commit convention into the manifest
// of the sets a register is responsible for (ADR-0228). The key is pop's
// projection of the stack rather than a claim about it: whatever a planning agent
// wrote there is replaced, because a hand-retyped copy of prose pop had already
// resolved is a copy nothing checks — one observed session dropped four clauses,
// including one of the human's own.
//
// Two sets of manifests are written, and the difference between them is the
// live-set boundary. A set this register *activated* is written outright:
// registration is the moment the set becomes Work, so nothing is running that
// the prose could move under. A set that was already registered is written only
// when it carries no convention at all and is not yet terminal — filling a blank
// takes nothing away from a running drain, and a set that cannot answer "what
// does a commit look like here" leaves a mid-drain Remediation rendering its
// subject from nothing. A value that is already there on an already-registered
// set stands: it may be the human's own edit.
//
// Per-task Planned commit subjects are untouched. Freezing them at planning time
// is what makes a drain reproducible; this key exists only for the agent that
// spawns a task mid-drain and has to render a new subject.
func RecordCommitConvention(d *Deps, result *RefreshResult, cwd string, resolve CommitConventionSource) error {
	if result == nil || resolve == nil {
		return nil
	}
	targets := commitConventionTargets(result)
	if len(targets) == 0 {
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
	for _, id := range targets {
		m := result.Manifests[id]
		if m == nil || m.CommitConvention == convention {
			continue
		}
		if err := writeCommitConventionKey(d, m, convention); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", id, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("write commit convention: %s", strings.Join(failures, "; "))
	}
	return nil
}

// commitConventionTargets is which of the register's sets get the key written:
// every set it activated, then every other registered, non-terminal set whose
// manifest carries no convention. Validity is not a filter — a set registers
// into state whether or not its manifest validates, and the fix-and-re-register
// loop the issue-tracker doc prescribes is not a new registration, so a set
// skipped for malformity here would never be written at all.
func commitConventionTargets(result *RefreshResult) []string {
	targets := append([]string(nil), result.NewRegistrationIDs...)
	activated := make(map[string]bool, len(targets))
	for _, id := range targets {
		activated[id] = true
	}
	for _, row := range result.Rows {
		if activated[row.ID] || TerminalStatus(row.Status) {
			continue
		}
		if m := result.Manifests[row.ID]; m != nil && m.CommitConvention == "" {
			targets = append(targets, row.ID)
		}
	}
	return targets
}

// writeCommitConventionKey sets the key on the manifest *document*, carrying
// every other key across as the bytes on disk, and leaves the loaded manifest
// agreeing with what it wrote.
//
// Going through the document rather than through WriteManifestAtomic is what
// lets a set whose manifest does not validate receive the convention: the
// atomic write re-marshals the parsed fields, so a bad task `type` or a value
// the loader normalised would come back as pop's projection of it instead of
// what the author typed. Here only this one key changes.
//
// A document that is not a JSON object has no key to set. That is not a failure
// — register already reports the set MALFORMED with the parse diagnostic — and
// the convention lands at the first register after the document parses.
func writeCommitConventionKey(d *Deps, m *Manifest, convention string) error {
	data, err := d.FS.ReadFile(m.Path)
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	value, err := json.Marshal(convention)
	if err != nil {
		return err
	}
	doc[manifestCommitConventionKey] = value
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := WriteAtomicWith(d, m.Path, out, 0o644); err != nil {
		return err
	}
	m.CommitConvention = convention
	return nil
}
