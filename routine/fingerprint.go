package routine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/glebglazov/pop/store"
)

// fingerprintInputs is the run-affecting surface a Routine's fingerprint hashes
// (ADR-0128): prompt.md content plus the explicitly-set schedule, runtime agent
// list, and effort. Fields serialize in fixed struct order and optional ones
// carry omitempty, so introducing a future criterion never moves an existing
// fingerprint until a human sets that criterion — the safety net stays quiet
// for routines that never opted in.
type fingerprintInputs struct {
	Prompt   string   `json:"prompt"`
	Schedule string   `json:"schedule"`
	Agents   []string `json:"agents,omitempty"`
	Effort   string   `json:"effort,omitempty"`
}

// fingerprintOf hashes the run-affecting inputs into a hex sha256 digest.
func fingerprintOf(prompt string, m Manifest) string {
	in := fingerprintInputs{
		Prompt:   prompt,
		Schedule: m.Schedule,
		Agents:   nonEmptyAgentSpecs(m.Agents),
		Effort:   m.Effort,
	}
	data, _ := json.Marshal(in)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Fingerprint returns the canonical run-affecting fingerprint of a Routine: a
// hash over its prompt.md content and explicitly-set schedule/agents/effort
// (ADR-0128). The daemon compares this against the fingerprint recorded on the
// last non-skipped run to detect prompt.md edits no chokepoint saw.
func Fingerprint(d *Deps, r *Routine) (string, error) {
	promptPath := filepath.Join(routineDir(d, r.ID), promptFileName)
	prompt, err := d.FS.ReadFile(promptPath)
	if err != nil {
		return "", fmt.Errorf("read routine prompt: %w", err)
	}
	return fingerprintOf(string(prompt), r.Manifest), nil
}

// LastFingerprint returns the fingerprint recorded on the routine's most recent
// non-skipped run, or the empty string when there is none or it predates the
// fingerprint migration. The caller reads empty as "nothing to compare" and
// never a mismatch, so pre-migration rows never trigger the changed-pause.
func LastFingerprint(s *store.Store, routineID string) (string, error) {
	return s.LastRoutineFingerprint(routineID)
}

// pauseChanged pauses id with reason `changed` (ADR-0128), the run-affecting
// drift signal shared by the daemon fingerprint check and the schedule/prompt
// edit chokepoints. An already-paused routine is overwritten to `changed`,
// since the drift is the latest and most useful cause.
func pauseChanged(d *Deps, id string) error {
	r, err := loadManifest(d, id)
	if err != nil {
		return err
	}
	r.Manifest.Paused = true
	r.Manifest.PauseReason = PauseReasonChanged
	return writeManifest(d, id, r.Manifest)
}

// PauseChangedWith pauses id with reason `changed` using the given deps. The
// Queue daemon calls it when a due routine's fingerprint no longer matches its
// last run. The daemon only reaches this for a Routine with a non-empty last
// fingerprint (i.e. one that has fired), so the anchor rule below is already
// satisfied and it does not need re-checking here.
func PauseChangedWith(d *Deps, id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	return pauseChanged(d, id)
}

// routineHasFired reports whether the Routine has at least one non-skipped run
// on record — the anchor the daemon uses to decide a schedule is live. A
// missing execution store means it has never fired.
func routineHasFired(d *Deps, id string) (bool, error) {
	s, ok, err := openExecutionStoreIfExists(d)
	if err != nil || !ok {
		return false, err
	}
	defer func() { _ = s.Close() }()
	last, err := s.LastRoutineFireTime(id)
	if err != nil {
		return false, err
	}
	return !last.IsZero(), nil
}

// editPauseReason returns the pause reason a run-affecting edit chokepoint should
// apply. The `changed` reason requires an anchor (ADR-0134): it means
// "run-affecting drift since runs existed", so it applies only once the Routine
// has fired at least once. A never-fired Routine has nothing to drift from and
// keeps reason `created` through any edit — this is what lets an agent set the
// schedule while refining a freshly created Routine without flipping created→changed.
func editPauseReason(d *Deps, id string) (PauseReason, error) {
	fired, err := routineHasFired(d, id)
	if err != nil {
		return "", err
	}
	if fired {
		return PauseReasonChanged, nil
	}
	return PauseReasonCreated, nil
}

// pauseAfterEdit pauses id following a run-affecting edit, choosing the reason
// via the anchor rule in editPauseReason. It is the load-modify-write companion
// used by the prompt-editor chokepoint; the schedule/runtime chokepoints fold
// the same reason into their own single write.
func pauseAfterEdit(d *Deps, id string) error {
	reason, err := editPauseReason(d, id)
	if err != nil {
		return err
	}
	r, err := loadManifest(d, id)
	if err != nil {
		return err
	}
	r.Manifest.Paused = true
	r.Manifest.PauseReason = reason
	return writeManifest(d, id, r.Manifest)
}
