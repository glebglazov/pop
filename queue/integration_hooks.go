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
	// integration emits the same event-name strings the queue journal records
	// (JournalEventIntegrated == "integrated", etc.), so the event copies across
	// verbatim — no per-event mapping is needed.
	return JournalEntry{
		Event:       e.Event,
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
}

func mergeabilityRecordToIntegration(rec MergeabilityRecord) integration.Record {
	return integration.Record(rec)
}

func mergeabilityRecordFromIntegration(rec integration.Record) MergeabilityRecord {
	return MergeabilityRecord(rec)
}
