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
// tried again.
type AgentCooldownEntry struct {
	ExhaustedUntil time.Time `json:"exhausted_until"`
}

type agentCooldownStore map[string]AgentCooldownEntry

// AgentCooldownPathWith returns the machine-global task cooldown store path.
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

func readAgentCooldowns(d *Deps) (agentCooldownStore, error) {
	var store agentCooldownStore
	if err := withTaskStateLock(d, func() error {
		var err error
		store, err = loadAgentCooldownsUnlocked(d)
		return err
	}); err != nil {
		return nil, err
	}
	return store, nil
}

func updateAgentCooldown(d *Deps, preset string, until time.Time) error {
	preset = strings.TrimSpace(preset)
	if preset == "" || until.IsZero() {
		return nil
	}
	return withTaskStateLock(d, func() error {
		store, err := loadAgentCooldownsUnlocked(d)
		if err != nil {
			return err
		}
		store[preset] = AgentCooldownEntry{ExhaustedUntil: until.UTC()}
		return saveAgentCooldownsUnlocked(d, store)
	})
}

func loadAgentCooldownsUnlocked(d *Deps) (agentCooldownStore, error) {
	path := AgentCooldownPathWith(d)
	data, err := d.FS.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return agentCooldownStore{}, nil
		}
		return nil, fmt.Errorf("read agent cooldown store: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return agentCooldownStore{}, nil
	}
	var store agentCooldownStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parse agent cooldown store: %w", err)
	}
	if store == nil {
		store = agentCooldownStore{}
	}
	return store, nil
}

func saveAgentCooldownsUnlocked(d *Deps, store agentCooldownStore) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode agent cooldown store: %w", err)
	}
	data = append(data, '\n')
	if err := WriteAtomicWith(d, AgentCooldownPathWith(d), data, 0o644); err != nil {
		return fmt.Errorf("write agent cooldown store: %w", err)
	}
	return nil
}

func withTaskStateLock(d *Deps, fn func() error) error {
	noticeOut := noticeWriter(d)
	var lastErr error
	for attempt := 0; attempt < stateLockRetries; attempt++ {
		lock, err := acquireStateLock(d, noticeOut, false)
		if err != nil {
			if errors.Is(err, ErrStateLockBusy) && attempt < stateLockRetries-1 {
				lastErr = err
				time.Sleep(stateLockRetryDelay)
				continue
			}
			return err
		}
		err = func() error {
			defer lock.Release()
			return fn()
		}()
		if err != nil {
			return err
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("acquire task state lock: exceeded retry limit")
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
