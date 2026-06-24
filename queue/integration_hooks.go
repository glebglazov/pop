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

func mergeabilityRecordToIntegration(rec MergeabilityRecord) integration.Record {
	return integration.Record(rec)
}

func mergeabilityRecordFromIntegration(rec integration.Record) MergeabilityRecord {
	return MergeabilityRecord(rec)
}
