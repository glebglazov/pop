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
