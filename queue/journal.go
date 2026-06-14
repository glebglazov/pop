package queue

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebglazov/pop/tasks"
)

const (
	JournalEventSpawn               = "spawn"
	JournalEventSpawnFailed         = "spawn_failed"
	JournalEventOutcome             = "outcome"
	JournalEventAgentSwitch         = "agent_switch"
	JournalEventAgentCooldown       = "agent_cooldown"
	JournalEventAgentUnavailable    = "agent_unavailable"
	JournalEventSetParked           = "set_parked"
	JournalEventMergeability        = "mergeability"
	JournalEventIntegrated          = "integrated"
	JournalEventIntegrationConflict = "integration_conflict"
	JournalEventIntegrationAttended = "integration_attended"
	JournalEventIntegrationOutcome  = "integration_outcome"
	JournalEventAbandoned           = "abandoned"
)

// JournalEntry is one append-only queue journal record.
type JournalEntry struct {
	Timestamp   time.Time          `json:"timestamp"`
	Event       string             `json:"event"`
	Project     string             `json:"project"`
	SetID       string             `json:"set_id"`
	RuntimePath string             `json:"runtime_path,omitempty"`
	Outcome     tasks.DrainOutcome `json:"outcome,omitempty"`
	PID         int                `json:"pid,omitempty"`
	Source      string             `json:"source,omitempty"`
	Agent       string             `json:"agent,omitempty"`
	Reason      string             `json:"reason,omitempty"`
	Until       time.Time          `json:"until,omitempty"`
	MergeStatus string             `json:"merge_status,omitempty"`
	Target      string             `json:"target,omitempty"`
	SourceRef   string             `json:"source_ref,omitempty"`
}

// QueueDataDir returns the data directory for queue-owned durable files.
func QueueDataDir(d *tasks.Deps) string {
	return SupervisorLockDir(d)
}

// JournalPath returns the durable queue journal path.
func JournalPath(d *tasks.Deps) string {
	return filepath.Join(QueueDataDir(d), "journal.jsonl")
}

// AppendJournalEntry appends one JSONL queue journal entry.
func AppendJournalEntry(d *tasks.Deps, entry JournalEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if err := d.FS.MkdirAll(QueueDataDir(d), 0o755); err != nil {
		return fmt.Errorf("create queue data dir: %w", err)
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode queue journal entry: %w", err)
	}
	f, err := os.OpenFile(JournalPath(d), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open queue journal: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write queue journal: %w", err)
	}
	return nil
}

// ReadJournal reads every valid queue journal entry in append order.
func ReadJournal(d *tasks.Deps) ([]JournalEntry, error) {
	data, err := d.FS.ReadFile(JournalPath(d))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []JournalEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry JournalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, fmt.Errorf("parse queue journal: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func journalHasOpenSpawn(entries []JournalEntry, project, runtimePath, setID string) bool {
	open := false
	for _, entry := range entries {
		if entry.Project != project || entry.RuntimePath != runtimePath || entry.SetID != setID {
			continue
		}
		switch entry.Event {
		case JournalEventSpawn:
			open = true
		case JournalEventOutcome:
			open = false
		}
	}
	return open
}

func journalHasOutcome(entries []JournalEntry, project, runtimePath, setID string, outcome tasks.DrainOutcome, timestamp time.Time) bool {
	for _, entry := range entries {
		if entry.Event != JournalEventOutcome || entry.Project != project || entry.RuntimePath != runtimePath || entry.SetID != setID || entry.Outcome != outcome {
			continue
		}
		if timestamp.IsZero() || entry.Timestamp.Equal(timestamp) {
			return true
		}
	}
	return false
}
