package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	agentCooldownFileName       = "agent-cooldowns.json"
	agentQuotaResetSkew         = 2 * time.Minute
	maxAgentQuotaResetHorizon   = 8 * 24 * time.Hour
	defaultAgentQuotaRetryAfter = time.Hour
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
	return resetAt.Add(agentQuotaResetSkew)
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
