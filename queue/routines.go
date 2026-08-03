package queue

import (
	"os"

	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// routineAdvancer is the Routine kind's advance seam, wired with the
// supervisor's own dependencies. There is no routine pipeline left in queue: the
// schedule, the drift safety net, the overlap check and the fire all live beside
// the Routine's other verbs, and this is only the wiring that hands them the
// daemon's tmux, project and clock seams.
func (d *Deps) routineAdvancer() work.Advancer {
	return routine.NewAdvancer(d.routineDeps(), d.reconcileOut())
}

func (d *Deps) routineDeps() *routine.Deps {
	rd := routine.DefaultDeps()
	rd.Now = d.now
	rd.Tmux = d.Tmux
	rd.Project = d.Project
	if d.Tasks != nil {
		rd.Tasks = d.Tasks
		if d.Tasks.FS != nil {
			rd.FS = d.Tasks.FS
		}
	}
	rd.ProcessAlive = func(pid int, procStart string) bool {
		return tasks.ProcessLiveWithToken(d.Tasks, pid, procStart)
	}
	rd.ProcStartToken = func(pid int) (string, bool) {
		if d.Tasks != nil && d.Tasks.ProcessStartToken != nil {
			return d.Tasks.ProcessStartToken(pid)
		}
		return "", false
	}
	rd.PID = os.Getpid
	return rd
}
