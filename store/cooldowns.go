package store

import (
	"database/sql"
	"time"
)

// AgentCooldown is one machine-global agent-preset quota cooldown: the preset
// whose subscription quota was exhausted and the instant it may be tried again.
type AgentCooldown struct {
	Preset         string
	ExhaustedUntil time.Time
}

// PutAgentCooldown upserts the cooldown for one agent preset. An empty preset or
// zero instant is a no-op. The latest write for a preset wins (ADR-0055).
func (s *Store) PutAgentCooldown(preset string, until time.Time) error {
	if preset == "" || until.IsZero() {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO agent_cooldowns (preset, exhausted_until) VALUES (?, ?)
		 ON CONFLICT(preset) DO UPDATE SET exhausted_until = excluded.exhausted_until`,
		preset, until.UTC().Format(timeLayout))
	return err
}

// AllAgentCooldowns returns every recorded cooldown keyed by preset, regardless
// of whether it has elapsed.
func (s *Store) AllAgentCooldowns() (map[string]time.Time, error) {
	rows, err := s.db.Query(`SELECT preset, exhausted_until FROM agent_cooldowns`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]time.Time{}
	for rows.Next() {
		var preset string
		var until sql.NullString
		if err := rows.Scan(&preset, &until); err != nil {
			return nil, err
		}
		out[preset] = parseTime(until.String)
	}
	return out, rows.Err()
}

// AgentModelCooldown is one machine-global Effort model skip (ADR-0168): a
// model recorded as unrunnable for one preset, with the instant it may be
// tried again. A zero Until is a permanent skip that never expires.
type AgentModelCooldown struct {
	Preset string
	Model  string
	Until  time.Time
}

// PutAgentModelCooldown upserts the skip for one (preset, model) pair. A zero
// until records a permanent skip that never expires; a non-zero until is the
// instant the skip lifts. An empty preset or model is a no-op. The latest
// write for a (preset, model) pair wins, mirroring PutAgentCooldown
// (ADR-0055/0168). This table is separate from agent_cooldowns so a spent
// model never renders as a paused preset.
func (s *Store) PutAgentModelCooldown(preset, model string, until time.Time) error {
	if preset == "" || model == "" {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO agent_model_cooldowns (preset, model, until) VALUES (?, ?, ?)
		 ON CONFLICT(preset, model) DO UPDATE SET until = excluded.until`,
		preset, model, nullTime(until))
	return err
}

// ActiveAgentModelCooldowns returns every (preset, model) skip still in force
// at now: a permanent skip (zero Until) always qualifies, and a timed skip
// qualifies while now is before its Until.
func (s *Store) ActiveAgentModelCooldowns(now time.Time) ([]AgentModelCooldown, error) {
	rows, err := s.db.Query(`SELECT preset, model, until FROM agent_model_cooldowns`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	now = now.UTC()
	var out []AgentModelCooldown
	for rows.Next() {
		var preset, model string
		var until sql.NullString
		if err := rows.Scan(&preset, &model, &until); err != nil {
			return nil, err
		}
		u := parseTime(until.String)
		if !u.IsZero() && !u.After(now) {
			continue
		}
		out = append(out, AgentModelCooldown{Preset: preset, Model: model, Until: u})
	}
	return out, rows.Err()
}
