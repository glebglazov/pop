package tasks

import (
	"strings"
	"time"

	"github.com/glebglazov/pop/store"
)

// The Work groups a retry cap can be spent in. They are the same phase words a
// Captured run is filed under, so a spent cap and the streams behind it name the
// same thing.
const (
	spendPhaseImplement = "implement"
	spendPhaseVerify    = "verify"
	spendPhaseRefine    = "refine"
)

// SpentRetryCapRecord is one spent-cap event at the tasks boundary: an agent
// that burned its whole retry cap on one piece of work without finishing it.
// The **Work journal** ranks these as severe on their own, whatever the drain
// they happened in went on to do — the attempts were paid for either way, and a
// later agent finishing the work does not refund them (ADR-0231).
type SpentRetryCapRecord struct {
	Repo        string
	SetID       string
	RuntimePath string
	Phase       string
	TaskID      string
	Preset      string
	Attempts    int
	Reason      string
	SpentAt     time.Time
}

// AllSpentRetryCaps returns every spent-cap event, oldest-first. It opens the
// store only when it already exists, so a pure reader never materialises an
// empty database.
func AllSpentRetryCaps(d *Deps) ([]SpentRetryCapRecord, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.AllSpentRetryCaps()
	if err != nil {
		return nil, err
	}
	out := make([]SpentRetryCapRecord, 0, len(rows))
	for _, c := range rows {
		out = append(out, SpentRetryCapRecord{
			Repo:        c.Repo,
			SetID:       c.SetID,
			RuntimePath: c.RuntimePath,
			Phase:       c.Phase,
			TaskID:      c.TaskID,
			Preset:      c.Preset,
			Attempts:    c.Attempts,
			Reason:      c.Reason,
			SpentAt:     c.SpentAt,
		})
	}
	return out, nil
}

// recordSpentRetryCap files the burn at the moment the cap runs out, which is
// the only moment that knows it happened: by the time the drain reaches a
// terminal, an agent whose cap was spent on the second task of five is
// indistinguishable from one that was never asked. Best-effort — a journal write
// that fails must not change what the walk does next, and the operator has
// already been told on stdout.
func recordSpentRetryCap(d *Deps, runtimePath string, c SpentRetryCapRecord) {
	if c.Preset == "" || c.SetID == "" {
		return
	}
	id, err := ResolveRepositoryIdentity(d, runtimePath)
	if err != nil {
		return
	}
	s, err := openDrainStore(d)
	if err != nil {
		return
	}
	_ = s.RecordSpentRetryCap(store.SpentRetryCap{
		Repo:        id.CommonDir,
		SetID:       c.SetID,
		RuntimePath: runtimePath,
		Phase:       c.Phase,
		TaskID:      c.TaskID,
		Preset:      c.Preset,
		Attempts:    c.Attempts,
		Reason:      clampAgentDiagnostic(strings.TrimSpace(c.Reason)),
		SpentAt:     time.Now().UTC(),
	})
}
