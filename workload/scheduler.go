package workload

import "fmt"

// MarkAutoPick marks the highest-priority runnable PRD row with AUTO.
// Non-runnable higher-priority rows are skipped.
func MarkAutoPick(rows []Row) {
	for i := range rows {
		if rows[i].Status != StatusReady {
			continue
		}
		rows[i].AutoPick = true
		rows[i].PriorityShow = fmt.Sprintf("%d AUTO", rows[i].Priority)
		return
	}
}
