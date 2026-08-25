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
	agentCooldownFileName       = "agent-cooldowns.json"
	maxAgentQuotaResetHorizon   = 8 * 24 * time.Hour
	defaultAgentQuotaRetryAfter = time.Hour
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

type agentCooldownStore map[string]AgentCooldownEntry

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
	rows, err := s.AllAgentCooldowns()
	if err != nil {
		return nil, err
	}
	store := make(agentCooldownStore, len(rows))
	for preset, until := range rows {
		store[preset] = AgentCooldownEntry{ExhaustedUntil: until}
	}
	return store, nil
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

// updateAgentCooldown records (or refreshes) the cooldown for one agent preset
// in the store, creating the store on first write. It is what a quota_paused
// Drain calls to mark the exhausted preset's reset instant (ADR-0055).
func updateAgentCooldown(d *Deps, preset string, until time.Time) error {
	preset = strings.TrimSpace(preset)
	if preset == "" || until.IsZero() {
		return nil
	}
	if err := migrateLegacyAgentCooldownFile(d); err != nil {
		return err
	}
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	return s.PutAgentCooldown(preset, until.UTC())
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
	var legacy agentCooldownStore
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

func agentQuotaCooldownUntil(resetAt, now time.Time, fallback time.Duration) time.Time {
	if fallback <= 0 {
		fallback = defaultAgentQuotaRetryAfter
	}
	now = now.UTC()
	if resetAt.IsZero() {
		return now.Add(fallback)
	}
	resetAt = resetAt.UTC()
	if !resetAt.After(now) || resetAt.Sub(now) > maxAgentQuotaResetHorizon {
		return now.Add(fallback)
	}
	// Recorded exactly as derived. The Quota assurance offset is already in it,
	// and padding again here would put the row two minutes past the instant the
	// recovery wait sleeps on — one wasted park per quota pause (ADR-0235).
	return resetAt
}

// updateAgentModelCooldown records (or refreshes) an Effort model skip
// (ADR-0168) — one model recorded as unrunnable for one preset — in the store,
// creating the store on first write. permanent records a skip that never
// expires (a Permanent recovery verdict); otherwise until is derived from
// resetAt via the same parsed-instant-else-fallback policy preset cooldowns
// use (agentQuotaCooldownUntil), with a one hour default rather than a second
// policy. This is a prefactor: nothing calls it until the Effort model skip is
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
// It diverges from agentQuotaCooldownUntil, which a preset cooldown uses,
// because the two are paying for different things. A paused preset costs a whole
// drain to re-test, so a distant reset is worth honouring; a skipped model costs
// one refusal that exits before the model is engaged, so re-probing daily is
// close to free and buys back every case where the stated date is stale — a
// top-up, a plan change, or a billing-cycle boundary that was never the moment
// the block lifts.
func modelSkipCooldownUntil(resetAt, now time.Time) time.Time {
	now = now.UTC()
	capped := now.Add(maxModelSkipHorizon)
	if resetAt.IsZero() {
		return now.Add(defaultAgentQuotaRetryAfter)
	}
	resetAt = resetAt.UTC()
	if !resetAt.After(now) {
		return now.Add(defaultAgentQuotaRetryAfter)
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
