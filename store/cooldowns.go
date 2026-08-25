package store

import (
	"database/sql"
	"time"
)

// AgentCooldown is one machine-global agent-preset quota cooldown: the preset
// whose subscription quota was exhausted and the instant it may be tried again.
type AgentCooldown struct {
	Preset string
	// ExhaustedUntil is the expiry pop enforces. For a stated row it is the
	// provider's own instant; for a guessed one it is the ceiling of the
	// refusal's window class — a backstop, not a retry time (ADR-0235).
	ExhaustedUntil time.Time
	// StatedUntil is the reset instant the provider's refusal named. Zero is
	// what marks the row a guess: nothing on the wire backs ExhaustedUntil.
	StatedUntil time.Time
	// Class is the Quota window class the refusal named, empty when it named
	// none. It is what a guessed expiry was dated from.
	Class string
	// NextProbeAt is when the exhausted preset is asked whether it will run yet.
	// Only a guess carries one: a stated instant is waited to exactly, so there
	// is nothing to ask about (ADR-0235).
	NextProbeAt time.Time
	// ProbeLeaseUntil is how long the checkout that took the current ask owns
	// it. The row is machine-global while parks are per-checkout, so the lease is
	// what keeps parallel worktrees from each asking the same question; it
	// expires on its own, so a prober that dies needs no sweep.
	ProbeLeaseUntil time.Time
}

// PutAgentCooldown upserts a guessed cooldown for one agent preset, overwriting
// whatever is there. An empty preset or zero instant is a no-op. It carries no
// stated instant and no window class, which is the truth about its only caller:
// the one-time fold of the retired agent-cooldowns.json file, whose records were
// a bare expiry (ADR-0055). Every refusal goes through RecordAgentQuotaCooldown
// instead, which is the one path that may not overwrite blindly.
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

// RecordAgentQuotaCooldown records one refusal against a preset and returns the
// row as it stands afterwards — which is not always the row passed in.
//
// A cooldown whose instant the provider stated (StatedUntil non-zero) always
// wins: reading beats guessing, and the fresher reading beats the older one. A
// guess, by contrast, never displaces a cooldown that is still in force at now.
// That single rule carries both halves of ADR-0235: a guess cannot overwrite a
// stated instant that has yet to pass, and a second refusal against a live guess
// cannot re-date its ceiling from the later moment. Only once the recorded
// expiry has elapsed does a guess write again, and it then writes a whole fresh
// row — which is also how a statement that proved wrong becomes a guess.
//
// The read and the write share one BEGIN IMMEDIATE transaction, because the row
// is machine-global and parallel checkouts refuse independently.
func (s *Store) RecordAgentQuotaCooldown(c AgentCooldown, now time.Time) (AgentCooldown, error) {
	if c.Preset == "" || c.ExhaustedUntil.IsZero() {
		return c, nil
	}
	now = now.UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return AgentCooldown{}, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(
		`SELECT exhausted_until, stated_until, class, next_probe_at, probe_lease_until
		   FROM agent_cooldowns WHERE preset = ?`,
		c.Preset)
	var until, stated, class, nextProbe, lease sql.NullString
	switch err := row.Scan(&until, &stated, &class, &nextProbe, &lease); err {
	case nil:
		existing := AgentCooldown{
			Preset:          c.Preset,
			ExhaustedUntil:  parseTime(until.String),
			StatedUntil:     parseTime(stated.String),
			Class:           class.String,
			NextProbeAt:     parseTime(nextProbe.String),
			ProbeLeaseUntil: parseTime(lease.String),
		}
		if c.StatedUntil.IsZero() && existing.ExhaustedUntil.After(now) {
			return existing, nil
		}
	case sql.ErrNoRows:
		// Nothing recorded — this refusal is the first one.
	default:
		return AgentCooldown{}, err
	}

	// The refusal's own schedule replaces whatever the displaced row was on, and
	// the lease goes with it: a probe in flight against the old row has nothing
	// left to answer about.
	if _, err := tx.Exec(
		`INSERT INTO agent_cooldowns (preset, exhausted_until, stated_until, class, next_probe_at, probe_lease_until)
		 VALUES (?, ?, ?, ?, ?, NULL)
		 ON CONFLICT(preset) DO UPDATE SET
		   exhausted_until   = excluded.exhausted_until,
		   stated_until      = excluded.stated_until,
		   class             = excluded.class,
		   next_probe_at     = excluded.next_probe_at,
		   probe_lease_until = NULL`,
		c.Preset,
		c.ExhaustedUntil.UTC().Format(timeLayout),
		nullTime(c.StatedUntil),
		c.Class,
		nullTime(c.NextProbeAt)); err != nil {
		return AgentCooldown{}, err
	}
	return c, tx.Commit()
}

// AllAgentCooldowns returns every recorded cooldown keyed by preset, regardless
// of whether it has elapsed.
func (s *Store) AllAgentCooldowns() (map[string]AgentCooldown, error) {
	rows, err := s.db.Query(`SELECT ` + agentCooldownColumns + ` FROM agent_cooldowns`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]AgentCooldown{}
	for rows.Next() {
		c, err := scanAgentCooldown(rows)
		if err != nil {
			return nil, err
		}
		out[c.Preset] = c
	}
	return out, rows.Err()
}

// agentCooldownColumns is the whole row, in the order scanAgentCooldown reads it.
const agentCooldownColumns = `preset, exhausted_until, stated_until, class, next_probe_at, probe_lease_until`

// scanAgentCooldown reads one agentCooldownColumns row from a query or a single
// row, so the two readers below cannot disagree about column order.
func scanAgentCooldown(row interface{ Scan(...any) error }) (AgentCooldown, error) {
	var preset string
	var until, stated, class, nextProbe, lease sql.NullString
	if err := row.Scan(&preset, &until, &stated, &class, &nextProbe, &lease); err != nil {
		return AgentCooldown{}, err
	}
	return AgentCooldown{
		Preset:          preset,
		ExhaustedUntil:  parseTime(until.String),
		StatedUntil:     parseTime(stated.String),
		Class:           class.String,
		NextProbeAt:     parseTime(nextProbe.String),
		ProbeLeaseUntil: parseTime(lease.String),
	}, nil
}

// GetAgentCooldown returns the recorded cooldown for one preset, or nil when
// none is recorded. A park reads it on every poll rather than trusting the
// instant it parked on: the row can be deleted under it by whichever checkout's
// probe found the window open again (ADR-0235).
func (s *Store) GetAgentCooldown(preset string) (*AgentCooldown, error) {
	if preset == "" {
		return nil, nil
	}
	c, err := scanAgentCooldown(s.db.QueryRow(
		`SELECT `+agentCooldownColumns+` FROM agent_cooldowns WHERE preset = ?`, preset))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ClaimQuotaProbe wins the exclusive right to ask one exhausted preset whether
// it will run yet, and answers the row to ask about. A nil row means do not ask,
// which covers every reason not to: no cooldown, an instant the provider stated
// (waited to exactly, never probed), an expiry that has already elapsed (the
// park ends on its own), an ask that is not due yet, and an ask another checkout
// currently owns.
//
// The claim is the conditional UPDATE itself, so two checkouts racing cannot
// both win it. lease is how long the winner owns the question: it outlives one
// probe invocation and nothing more, so a prober that dies frees the question by
// elapsing rather than by being noticed (ADR-0235).
//
// A guessed row with no next_probe_at at all is due now. The only rows like that
// are the ones the retired agent-cooldowns.json fold writes, which are bare
// expiries with nothing behind them — exactly the kind worth asking about.
func (s *Store) ClaimQuotaProbe(preset string, now time.Time, lease time.Duration) (*AgentCooldown, error) {
	if preset == "" || lease <= 0 {
		return nil, nil
	}
	now = now.UTC()
	nowStr := now.Format(timeLayout)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`UPDATE agent_cooldowns SET probe_lease_until = ?
		  WHERE preset = ?
		    AND stated_until IS NULL
		    AND exhausted_until > ?
		    AND (next_probe_at IS NULL OR next_probe_at <= ?)
		    AND (probe_lease_until IS NULL OR probe_lease_until <= ?)`,
		now.Add(lease).Format(timeLayout), preset, nowStr, nowStr, nowStr)
	if err != nil {
		return nil, err
	}
	claimed, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if claimed == 0 {
		return nil, nil
	}
	c, err := scanAgentCooldown(tx.QueryRow(
		`SELECT `+agentCooldownColumns+` FROM agent_cooldowns WHERE preset = ?`, preset))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &c, nil
}

// ScheduleNextQuotaProbe records a refused ask: the question is free again, the
// next one is due at nextProbeAt, and the expiry is left exactly as it was. A
// refusal reached through a probe is the same refusal the row already holds, so
// re-dating the ceiling from it is the compounding ADR-0235 removed.
func (s *Store) ScheduleNextQuotaProbe(preset string, nextProbeAt time.Time) error {
	if preset == "" {
		return nil
	}
	_, err := s.db.Exec(
		`UPDATE agent_cooldowns SET next_probe_at = ?, probe_lease_until = NULL WHERE preset = ?`,
		nullTime(nextProbeAt), preset)
	return err
}

// ClearAgentQuotaCooldown drops one preset's cooldown and makes every park that
// was waiting on it eligible at now. The two rows said one thing between them —
// this preset is exhausted until that instant — so an ask the agent answered
// with yes retires both: without the waiters' own reset_at moving, the parks
// would sit through a ceiling the preset has already outlived (ADR-0235).
func (s *Store) ClearAgentQuotaCooldown(preset string, now time.Time) error {
	if preset == "" {
		return nil
	}
	nowStr := now.UTC().Format(timeLayout)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM agent_cooldowns WHERE preset = ?`, preset); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE recovery_waiters SET reset_at = ? WHERE preset = ? AND reset_at > ?`,
		nowStr, preset, nowStr); err != nil {
		return err
	}
	return tx.Commit()
}

// AgentModelCooldown is one machine-global Effort model skip (ADR-0168): a
// model recorded as unrunnable for one preset, with the instant it may be
// tried again. A zero Until is a permanent skip that never expires.
type AgentModelCooldown struct {
	Preset string
	Model  string
	Until  time.Time
	// StatedUntil is the reset instant the provider's refusal named, kept as the
	// message gave it. Until is capped independently of it, so the two disagree
	// whenever the stated reset is further out than the cap; zero when the
	// refusal named no reset.
	StatedUntil time.Time
}

// PutAgentModelCooldown upserts the skip for one (preset, model) pair. A zero
// until records a permanent skip that never expires; a non-zero until is the
// instant the skip lifts. stated is what the provider's message claimed, zero
// when it claimed nothing. An empty preset or model is a no-op. The latest
// write for a (preset, model) pair wins, mirroring PutAgentCooldown
// (ADR-0055/0168). This table is separate from agent_cooldowns so a spent
// model never renders as a paused preset.
func (s *Store) PutAgentModelCooldown(preset, model string, until, stated time.Time) error {
	if preset == "" || model == "" {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO agent_model_cooldowns (preset, model, until, stated_until) VALUES (?, ?, ?, ?)
		 ON CONFLICT(preset, model) DO UPDATE SET until = excluded.until, stated_until = excluded.stated_until`,
		preset, model, nullTime(until), nullTime(stated))
	return err
}

// ActiveAgentModelCooldowns returns every (preset, model) skip still in force
// at now: a permanent skip (zero Until) always qualifies, and a timed skip
// qualifies while now is before its Until.
func (s *Store) ActiveAgentModelCooldowns(now time.Time) ([]AgentModelCooldown, error) {
	rows, err := s.db.Query(`SELECT preset, model, until, stated_until FROM agent_model_cooldowns`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	now = now.UTC()
	var out []AgentModelCooldown
	for rows.Next() {
		var preset, model string
		var until, stated sql.NullString
		if err := rows.Scan(&preset, &model, &until, &stated); err != nil {
			return nil, err
		}
		u := parseTime(until.String)
		if !u.IsZero() && !u.After(now) {
			continue
		}
		out = append(out, AgentModelCooldown{Preset: preset, Model: model, Until: u, StatedUntil: parseTime(stated.String)})
	}
	return out, rows.Err()
}
