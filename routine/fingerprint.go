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
// last run.
func PauseChangedWith(d *Deps, id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	return pauseChanged(d, id)
}
