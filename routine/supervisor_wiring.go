package routine

import (
	"os"
	"time"

	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// SupervisorSeams is what a daemon or read surface hands the Routine kind: the
// clock, tmux, project and task seams it already holds, before they are
// projected onto the Routine's own dependencies. It is a parameter object rather
// than a long argument list because every caller passes the same four, and the
// projection below is the only place that knows how they map.
type SupervisorSeams struct {
	Tasks   *tasks.Deps
	Project *project.Deps
	Tmux    tmuxmod.Tmux
	Now     func() time.Time
}

// DepsFrom projects a caller's seams onto the Routine's own. The clock, tmux and
// project seams pass straight through; liveness is the shared PID-plus-start-token
// check so a recycled PID never reads as a live fire; everything the caller does
// not own keeps its real default.
//
// It lives here rather than beside the caller because what a Routine needs from
// the outside is the Routine's business: a second caller wiring the kind gets the
// same projection instead of a second copy of it.
func DepsFrom(s SupervisorSeams) *Deps {
	rd := DefaultDeps()
	rd.Now = s.Now
	rd.Tmux = s.Tmux
	rd.Project = s.Project
	if s.Tasks != nil {
		rd.Tasks = s.Tasks
		if s.Tasks.FS != nil {
			rd.FS = s.Tasks.FS
		}
	}
	rd.ProcessAlive = func(pid int, procStart string) bool {
		return tasks.ProcessLiveWithToken(s.Tasks, pid, procStart)
	}
	rd.ProcStartToken = func(pid int) (string, bool) {
		if s.Tasks != nil && s.Tasks.ProcessStartToken != nil {
			return s.Tasks.ProcessStartToken(pid)
		}
		return "", false
	}
	rd.PID = os.Getpid
	return rd
}
