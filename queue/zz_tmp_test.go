package queue

import (
	"fmt"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

func TestTmpBucketCodes(t *testing.T) {
	for _, s := range []tasks.TaskSetStatus{tasks.StatusDone, tasks.StatusReady, tasks.StatusBlocked, tasks.StatusMalformed, tasks.StatusDeferred} {
		row := DashboardRow{SetRef: SetRef{RawStatus: s}}
		fmt.Printf("%s => %q\n", s, dashboardStatusCellStyled(row))
	}
	row := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady}, Started: true}
	fmt.Printf("IN PROGRESS => %q\n", dashboardStatusCellStyled(row))
}
