package tasks

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/glebglazov/pop/config"
)

// DefaultMaxRemediationDepth bounds the verify→remediate→re-verify loop when
// user config sets no explicit cap (ADR-0086): after this many Verifier-produced
// Remediation tasks, a set that still returns FIXABLE parks at VERIFY-FAILED
// rather than spawning another. The configurable `[tasks.verify]
// .max_remediation_depth` overrides it.
const DefaultMaxRemediationDepth = 3

// remediationIDPattern matches a Remediation task's id (`NN-remediation`), so
// the set's remediation depth is derived from these entries.
var remediationIDPattern = regexp.MustCompile(`^\d+-remediation$`)

// Remediation origins (ADR-0105) tag a Remediation task's provenance so depth
// counts only the unattended run. Auto = Verifier-spawned on FIXABLE, human =
// spawned via the Remediate disposition.
const (
	RemediationOriginAuto  = "auto"
	RemediationOriginHuman = "human"
)

// remediationOrigin reads a task's remediation origin, defaulting an absent or
// empty origin to auto (ADR-0105) so legacy Remediation entries written before
// origins existed keep their prior depth-cap contribution.
func remediationOrigin(t Task) string {
	if t.Origin == RemediationOriginHuman {
		return RemediationOriginHuman
	}
	return RemediationOriginAuto
}

// taskNumberPattern extracts the leading zero-padded ordinal from a task id or
// file (`07-foo` → 7), so the next Remediation task takes the next number.
var taskNumberPattern = regexp.MustCompile(`^(\d+)-`)

// maxRemediationDepth resolves the per-set remediation depth cap from user
// config, falling back to DefaultMaxRemediationDepth when unconfigured (ADR-0086).
func maxRemediationDepth(cfg *config.Config) int {
	if cfg != nil && cfg.Task != nil && cfg.Task.Verify != nil && cfg.Task.Verify.MaxRemediationDepth != nil {
		return *cfg.Task.Verify.MaxRemediationDepth
	}
	return DefaultMaxRemediationDepth
}

// remediationDepth is the count of consecutive auto-origin Remediation tasks
// since the most recent human-origin one (ADR-0105) — the unattended loop's
// bound. A human remediation resets the count to zero: human intervention is a
// fresh grant of trust, so it re-enables the auto budget rather than consuming
// it. Derived straight off the manifest in order, needing no stored counter
// (the same idiom as crash backoff from drain history). Legacy entries without
// an origin read as auto, so pre-ADR-0105 sets keep their prior cap behavior.
func remediationDepth(m *Manifest) int {
	if m == nil {
		return 0
	}
	n := 0
	for _, t := range m.Tasks {
		if !remediationIDPattern.MatchString(t.ID) {
			continue
		}
		if remediationOrigin(t) == RemediationOriginHuman {
			n = 0
		} else {
			n++
		}
	}
	return n
}

// nextTaskNumber returns the next free ordinal for a task in the set — one past
// the highest leading number across existing task ids, so a new task never
// collides with an existing file or id.
func nextTaskNumber(m *Manifest) int {
	max := 0
	if m == nil {
		return 1
	}
	for _, t := range m.Tasks {
		if match := taskNumberPattern.FindStringSubmatch(t.ID); match != nil {
			if n, err := strconv.Atoi(match[1]); err == nil && n > max {
				max = n
			}
		}
	}
	return max + 1
}

// remediationBody assembles a Remediation task's markdown body (ADR-0086): the
// Verifier's findings for the work at workSHA, framed as fixable work, with an
// Acceptance criteria section (the manifest validator requires one). Findings
// live only here — never edited into another task's spec, which stays stable,
// task-scoped intent.
//
// A non-empty humanNote marks a human-triggered remediation (ADR-0103): a human
// turned the findings into a fix by authorising this task, past the auto path's
// FIXABLE-under-cap gate (from a NEEDS-HUMAN verdict, or when the depth cap is
// exhausted). The note is folded into a "## Human note" section and the "What to
// build" framing reflects the human authorisation rather than the automatic
// FIXABLE spawn — the untrusted note is neutralized the same way findings are so
// an echoed AC heading can never invalidate the task.
func remediationBody(workSHA, findings, humanNote string, cycle int) string {
	note := neutralizeACHeaders(strings.TrimSpace(humanNote))
	var b strings.Builder
	b.WriteString("## What to build\n\n")
	if note != "" {
		b.WriteString("Resolve the verification findings below. A human reviewed this task set's completed work")
		if workSHA != "" {
			b.WriteString(fmt.Sprintf(" at %s", ShortSHA(workSHA)))
		}
		b.WriteString(" and authorised this remediation: the acceptance criteria are not yet met, and the human has directed an agent to close the gaps (see their note below). ")
		b.WriteString("Make the changes needed to satisfy the set's acceptance criteria. Do not edit the other task specs — they are stable, task-scoped intent; fix the code and artifacts they describe.\n\n")
	} else {
		b.WriteString("Resolve the verification findings below. An independent Verifier judged this task set's completed work")
		if workSHA != "" {
			b.WriteString(fmt.Sprintf(" at %s", ShortSHA(workSHA)))
		}
		b.WriteString(" and returned FIXABLE: the acceptance criteria are not yet met, but the gaps are ones an agent can close. ")
		b.WriteString("Make the changes needed to satisfy the set's acceptance criteria. Do not edit the other task specs — they are stable, task-scoped intent; fix the code and artifacts they describe.\n\n")
	}
	b.WriteString(fmt.Sprintf("This is remediation cycle %d.\n\n", cycle))
	if note != "" {
		b.WriteString("## Human note\n\n")
		b.WriteString(note)
		b.WriteString("\n\n")
	}
	b.WriteString("## Findings\n\n")
	f := neutralizeACHeaders(strings.TrimSpace(findings))
	if f == "" {
		f = "(the Verifier recorded no specific findings)"
	}
	b.WriteString(f)
	b.WriteString("\n\n## Acceptance criteria\n\n")
	b.WriteString("- [ ] Every finding above is resolved and the task set's acceptance criteria are met\n")
	return b.String()
}

// neutralizeACHeaders demotes any line in the free-text findings that would
// otherwise be parsed as an "## Acceptance criteria" heading (the manifest
// validator rejects a task body with more than one). The verifier's findings
// are untrusted text; without this, findings that echo that exact heading would
// produce an invalid Remediation task that the drain could not pick up.
func neutralizeACHeaders(findings string) string {
	if findings == "" {
		return ""
	}
	lines := strings.Split(findings, "\n")
	for i, line := range lines {
		if acHeaderPattern.MatchString(strings.TrimSpace(line)) {
			lines[i] = "#" + line // demote to "### …" so it no longer matches ^##\s
		}
	}
	return strings.Join(lines, "\n")
}

// spawnRemediationTask writes a new AFK Remediation task into the set (ADR-0086):
// a markdown body carrying the findings plus an atomically-appended index.json
// entry at the next number. The markdown is written first — the manifest entry
// references it and the validator requires the file to exist — then the manifest
// entry, so a reader never sees an index entry without its body. A manifest-write
// failure rolls the orphan markdown back, keeping the two in sync. It mutates the
// passed manifest's task list and returns the new task's id.
//
// repo is the canonical git common directory for the set's repository. When
// non-empty, the new open AFK work ends any cached verification episode for the
// set so the Verifier re-fires against the next work SHA (ADR-0096). An empty
// repo or a store error skips invalidation silently — remediation must still
// succeed.
//
// A non-empty humanNote marks a human-triggered remediation (ADR-0103): the
// human's rationale is carried into the task body alongside the findings and the
// framing reflects that a human authorised the fix (the auto FIXABLE path passes
// an empty note).
func spawnRemediationTask(d *Deps, m *Manifest, repo, workSHA, findings, humanNote, origin string) (string, error) {
	id, err := writeRemediationTask(d, m, workSHA, findings, humanNote, origin)
	if err != nil {
		return "", err
	}
	// The set is no longer terminal: any cached verdict is for the old episode
	// and must be discarded so the Verifier re-fires at the new work SHA.
	invalidateVerifyVerdicts(d, repo, m.Stem)
	return id, nil
}

// writeRemediationTask writes the Remediation task's markdown body and appends
// it to the manifest, returning the new task id. It performs only the filesystem
// half of spawning — the caller invalidates the set's cached verdicts. The human
// out-of-band Remediate path (ADR-0104) drives this directly so the manifest
// append and the verdict invalidation ride one quiescence-gated transaction.
func writeRemediationTask(d *Deps, m *Manifest, workSHA, findings, humanNote, origin string) (string, error) {
	if m == nil {
		return "", exitErr(ExitOperational, "spawn remediation task: nil manifest")
	}
	cycle := remediationDepth(m) + 1
	id := fmt.Sprintf("%02d-remediation", nextTaskNumber(m))
	file := id + ".md"

	mdPath := filepath.Join(m.Dir, file)
	if err := WriteAtomicWith(d, mdPath, []byte(remediationBody(workSHA, findings, humanNote, cycle)), 0o644); err != nil {
		return "", exitErr(ExitOperational, "write remediation task body: %v", err)
	}

	m.Tasks = append(m.Tasks, Task{
		ID:        id,
		File:      file,
		Title:     fmt.Sprintf("Remediation %d: resolve verification findings", cycle),
		Type:      "AFK",
		Status:    TaskOpen,
		BlockedBy: []string{},
		Origin:    origin,
	})
	if err := WriteManifestAtomic(d, m); err != nil {
		// Roll the orphan markdown back so markdown and index.json stay in sync.
		_ = d.FS.RemoveAll(mdPath)
		m.Tasks = m.Tasks[:len(m.Tasks)-1]
		return "", exitErr(ExitOperational, "append remediation task to manifest: %v", err)
	}
	return id, nil
}

// spawnRemediationIfUnderCap enacts a FIXABLE verdict (ADR-0086). While the set
// is under its remediation depth cap it spawns a new AFK Remediation task whose
// body is the findings and returns spawned=true — the Drain picks it up, and its
// completion moves the work SHA so the cached verdict goes stale and the Verifier
// re-fires, closing the loop. At or over the cap it writes nothing and returns
// spawned=false, so the caller parks the set at VERIFY-FAILED.
func spawnRemediationIfUnderCap(d *Deps, m *Manifest, repo, workSHA, findings string, maxDepth int) (spawned bool, id string, err error) {
	if remediationDepth(m) >= maxDepth {
		return false, "", nil
	}
	id, err = spawnRemediationTask(d, m, repo, workSHA, findings, "", RemediationOriginAuto)
	if err != nil {
		return false, "", err
	}
	return true, id, nil
}
