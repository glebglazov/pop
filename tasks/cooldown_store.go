package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/store"
)

const (
	agentCooldownFileName     = "agent-cooldowns.json"
	maxAgentQuotaResetHorizon = 8 * 24 * time.Hour
	// defaultUnclassedQuotaCeiling dates a guess whose refusal named no window
	// class and whose caller configured no ceiling. It is the shortest class
	// span, for the reason ADR-0235 gives the config default the same value: a
	// shorter blind wait is a wait that ends before the window does, and the
	// refusal it earns is what used to push the deadline further out.
	defaultUnclassedQuotaCeiling = 5 * time.Hour
	// defaultModelSkipRetryAfter is the Effort model skip's own default when the
	// refusal named no reset. It stays at an hour where a preset ceiling does
	// not, because re-probing one model costs a refusal that exits before the
	// model is engaged (ADR-0168).
	defaultModelSkipRetryAfter = time.Hour
	// maxModelSkipHorizon caps how long one Effort model skip holds, whatever
	// reset the refusal claimed (ADR-0168).
	maxModelSkipHorizon = 24 * time.Hour
)

// AgentCooldownEntry records when one subscription-level agent preset may be
// tried again. It is the legacy agent-cooldowns.json record shape, retained so a
// surviving file can be migrated into the store on first read (ADR-0055).
type AgentCooldownEntry struct {
	ExhaustedUntil time.Time `json:"exhausted_until"`
}

type agentCooldownStore map[string]store.AgentCooldown

// AgentCooldownPathWith returns the retired standalone agent cooldown store
// path. ADR-0055 folds its contents into the global store on first read and then
// removes the file; the path is kept only to find and migrate a surviving file.
func AgentCooldownPathWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", agentCooldownFileName)
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop", agentCooldownFileName)
	}
	return filepath.Join(home, ".local", "share", "pop", agentCooldownFileName)
}

// readAgentCooldowns returns every recorded agent-preset cooldown keyed by
// preset, regardless of whether it has elapsed. It first migrates any surviving
// agent-cooldowns.json into the store and retires the file, then reads from the
// store. It opens the store only when it already exists, so a pure reader with no
// legacy file never materialises an empty database.
func readAgentCooldowns(d *Deps) (agentCooldownStore, error) {
	if err := migrateLegacyAgentCooldownFile(d); err != nil {
		return nil, err
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return agentCooldownStore{}, err
	}
	return s.AllAgentCooldowns()
}

// ActiveAgentCooldownsWith returns active machine-global agent cooldowns keyed
// by preset. It is read-only so status/reporting callers do not need to know the
// cooldown store format.
func ActiveAgentCooldownsWith(d *Deps, now time.Time) (map[string]time.Time, error) {
	store, err := readAgentCooldowns(d)
	if err != nil {
		return nil, err
	}
	return activeAgentCooldowns(store, now), nil
}

// AgentQuotaCooldownRequest is one refusal reduced to what the cooldown store
// needs: the instant the provider stated, the Quota window class it named, and
// the ceiling to date a guess from when it named no class.
//
// Stated is zero whenever pop is guessing — including for an instant pop itself
// invented, such as a spend cap's hour. An invented instant recorded as a
// statement would be read at face value by everything downstream and would
// re-date itself on every later refusal, which is the compounding ADR-0235
// kills.
type AgentQuotaCooldownRequest struct {
	Preset  string
	Stated  time.Time
	Class   AgentQuotaWindowClass
	Ceiling time.Duration
}

// quotaCooldownRequest reads one proceed verdict as a cooldown request.
func quotaCooldownRequest(v AgentProceedVerdict) AgentQuotaCooldownRequest {
	req := AgentQuotaCooldownRequest{Preset: v.Preset, Stated: v.ResetAt, Class: v.WindowClass}
	if v.Kind == ProceedSpendCap {
		// The verdict's instant here is spendCapCooldown measured from now —
		// pop's own hour, since a spend cap names no moment it will lift.
		req.Stated, req.Ceiling = time.Time{}, spendCapCooldown
	}
	return req
}

// recordAgentQuotaCooldown records one refusal against a preset in the store,
// creating the store on first write, and returns the expiry now in force —
// which is the row already there when this refusal was a guess against a live
// cooldown. unclassed is the configured ceiling for a refusal naming no window
// class (ADR-0055/0235).
func recordAgentQuotaCooldown(d *Deps, req AgentQuotaCooldownRequest, now time.Time, unclassed time.Duration) (time.Time, error) {
	req.Preset = strings.TrimSpace(req.Preset)
	if req.Preset == "" {
		return time.Time{}, nil
	}
	if err := migrateLegacyAgentCooldownFile(d); err != nil {
		return time.Time{}, err
	}
	s, err := openDrainStore(d)
	if err != nil {
		return time.Time{}, err
	}
	recorded, err := s.RecordAgentQuotaCooldown(agentQuotaCooldownRow(req, now, unclassed), now)
	if err != nil {
		return time.Time{}, err
	}
	return recorded.ExhaustedUntil, nil
}

// agentQuotaCooldownRow turns one refusal into the row it asks to record.
//
// A reset the provider stated is recorded as itself, in both columns, exactly as
// derived: the Quota assurance offset is already in it, and padding again here
// would put the row past the instant the recovery wait sleeps on — one wasted
// park per quota pause. A reset that is absent, already behind now, or further
// out than any subscription window runs is no statement at all, and the row
// becomes a guess dated from the ceiling of its window class (ADR-0235).
func agentQuotaCooldownRow(req AgentQuotaCooldownRequest, now time.Time, unclassed time.Duration) store.AgentCooldown {
	now = now.UTC()
	row := store.AgentCooldown{Preset: req.Preset, Class: string(req.Class)}
	if !req.Stated.IsZero() {
		stated := req.Stated.UTC()
		if stated.After(now) && stated.Sub(now) <= maxAgentQuotaResetHorizon {
			row.ExhaustedUntil, row.StatedUntil = stated, stated
			return row
		}
	}
	row.ExhaustedUntil = now.Add(guessedQuotaCeiling(req.Class, req.Ceiling, unclassed))
	return row
}

// guessedQuotaCeiling is how long a guessed cooldown holds: the span of the
// window the refusal named, so a session limit is not waited out for a week and
// a weekly one is not re-guessed every hour. A refusal naming no window falls
// back to the caller's own ceiling, then to the configured one.
func guessedQuotaCeiling(class AgentQuotaWindowClass, requested, unclassed time.Duration) time.Duration {
	if span, ok := class.Span(); ok {
		return span
	}
	if requested > 0 {
		return requested
	}
	if unclassed > 0 {
		return unclassed
	}
	return defaultUnclassedQuotaCeiling
}

// migrateLegacyAgentCooldownFile folds a surviving agent-cooldowns.json into the
// store and removes the file. A missing file is the steady state after the
// one-time migration and costs only the read miss — no store is opened. An entry
// already present in the store is left untouched (the store wins).
func migrateLegacyAgentCooldownFile(d *Deps) error {
	path := AgentCooldownPathWith(d)
	data, err := d.FS.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read agent cooldown store: %w", err)
	}
	var legacy map[string]AgentCooldownEntry
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &legacy); err != nil {
			return fmt.Errorf("parse agent cooldown store: %w", err)
		}
	}
	if len(legacy) > 0 {
		s, err := openDrainStore(d)
		if err != nil {
			return err
		}
		existing, err := s.AllAgentCooldowns()
		if err != nil {
			return err
		}
		for preset, entry := range legacy {
			preset = strings.TrimSpace(preset)
			if preset == "" || entry.ExhaustedUntil.IsZero() {
				continue
			}
			if _, ok := existing[preset]; ok {
				continue
			}
			if err := s.PutAgentCooldown(preset, entry.ExhaustedUntil.UTC()); err != nil {
				return err
			}
		}
	}
	// Retire the file once its contents are safely in the store.
	return d.FS.RemoveAll(path)
}

// updateAgentModelCooldown records (or refreshes) an Effort model skip
// (ADR-0168) — one model recorded as unrunnable for one preset — in the store,
// creating the store on first write. permanent records a skip that never
// expires (a Permanent recovery verdict); otherwise until is derived from
// resetAt via modelSkipCooldownUntil, the skip's own capped policy. This is a prefactor: nothing calls it until the Effort model skip is
// wired into dispatch.
func updateAgentModelCooldown(d *Deps, preset, model string, resetAt time.Time, permanent bool) error {
	preset = strings.TrimSpace(preset)
	model = strings.TrimSpace(model)
	if preset == "" || model == "" {
		return nil
	}
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	if permanent {
		return s.PutAgentModelCooldown(preset, model, time.Time{}, time.Time{})
	}
	now := time.Now().UTC()
	return s.PutAgentModelCooldown(preset, model, modelSkipCooldownUntil(resetAt, now), resetAt.UTC())
}

// modelSkipCooldownUntil is the Effort model skip's own expiry policy: the
// parsed reset when the adapter named one, the one hour default when it did
// not, and never more than maxModelSkipHorizon out (ADR-0168).
//
// It diverges from agentQuotaCooldownRow, which a preset cooldown uses, because
// the two are paying for different things. A paused preset costs a whole
// drain to re-test, so a distant reset is worth honouring; a skipped model costs
// one refusal that exits before the model is engaged, so re-probing daily is
// close to free and buys back every case where the stated date is stale — a
// top-up, a plan change, or a billing-cycle boundary that was never the moment
// the block lifts.
func modelSkipCooldownUntil(resetAt, now time.Time) time.Time {
	now = now.UTC()
	capped := now.Add(maxModelSkipHorizon)
	if resetAt.IsZero() {
		return now.Add(defaultModelSkipRetryAfter)
	}
	resetAt = resetAt.UTC()
	if !resetAt.After(now) {
		return now.Add(defaultModelSkipRetryAfter)
	}
	if resetAt.After(capped) {
		return capped
	}
	return resetAt
}

// ActiveAgentModelCooldownsWith returns every Effort model skip (ADR-0168)
// still in force at now: a permanent skip always qualifies, a timed one while
// now precedes its expiry. It is read-only and never materialises an empty
// store, mirroring ActiveAgentCooldownsWith.
func ActiveAgentModelCooldownsWith(d *Deps, now time.Time) ([]store.AgentModelCooldown, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	return s.ActiveAgentModelCooldowns(now)
}

func activeAgentCooldowns(store agentCooldownStore, now time.Time) map[string]time.Time {
	active := map[string]time.Time{}
	now = now.UTC()
	for preset, entry := range store {
		preset = strings.TrimSpace(preset)
		if preset == "" || entry.ExhaustedUntil.IsZero() {
			continue
		}
		until := entry.ExhaustedUntil.UTC()
		if until.After(now) {
			active[preset] = until
		}
	}
	return active
}
