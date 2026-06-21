package queue

import (
	"github.com/glebglazov/pop/tasks/integration"
)

func queueIntegrationDeps(d *Deps) *integration.Deps {
	if d == nil {
		d = DefaultDeps()
	}
	id := &integration.Deps{
		Tasks:   d.Tasks,
		Project: d.Project,
	}
	if d.ComputeMergeability != nil {
		id.ComputeMergeability = func(workingPath, runtimePath string) (integration.Record, error) {
			rec, err := d.ComputeMergeability(workingPath, runtimePath)
			if err != nil {
				return integration.Record{}, err
			}
			return mergeabilityRecordToIntegration(rec), nil
		}
	}
	if d.AcquireRuntimeLock != nil {
		id.AcquireRuntimeLock = func(runtimePath string) (integration.RuntimeLock, error) {
			return d.AcquireRuntimeLock(runtimePath)
		}
	}
	return id
}

func queueIntegrateHooks(d *Deps) integration.IntegrateHooks {
	return integration.IntegrateHooks{
		AppendJournal: func(e integration.JournalEntry) error {
			return AppendJournalEntry(d.Tasks, integrationJournalToQueue(e))
		},
	}
}

func integrationJournalToQueue(e integration.JournalEntry) JournalEntry {
	entry := JournalEntry{
		Project:     e.Project,
		SetID:       e.SetID,
		RuntimePath: e.RuntimePath,
		MergeStatus: e.MergeStatus,
		Target:      e.Target,
		SourceRef:   e.SourceRef,
		Source:      e.Source,
		Agent:       e.Agent,
		Reason:      e.Reason,
	}
	switch e.Event {
	case "integrated":
		entry.Event = JournalEventIntegrated
	case "integration_conflict":
		entry.Event = JournalEventIntegrationConflict
	case "integration_attended":
		entry.Event = JournalEventIntegrationAttended
	case "integration_outcome":
		entry.Event = JournalEventIntegrationOutcome
	case "mergeability":
		entry.Event = JournalEventMergeability
	default:
		entry.Event = e.Event
	}
	return entry
}

func mergeabilityRecordToIntegration(rec MergeabilityRecord) integration.Record {
	return integration.Record(rec)
}

func mergeabilityRecordFromIntegration(rec integration.Record) MergeabilityRecord {
	return MergeabilityRecord(rec)
}
