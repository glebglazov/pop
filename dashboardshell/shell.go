package dashboardshell

import (
	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/queue"
	"github.com/glebglazov/pop/routine"
)

// View selects which dashboard the shared shell shows.
type View int

const (
	ViewQueue View = iota
	ViewRoutine
)

// Shell is the shared TUI hosting the Queue and Routine dashboards as sibling
// views toggled with v.
type Shell struct {
	active View
	queue  queue.QueueDashboard
	routine routine.RoutineDashboard
	width  int
	height int
}

// RunFromQueue opens the shell on the Work dashboard. It returns the bound
// checkout path chosen with Ctrl-g (empty otherwise), matching queue.RunDashboard.
func RunFromQueue(d *queue.Deps, cfg *config.Config) (string, error) {
	s, err := newShell(ViewQueue, d, cfg, routine.DefaultDeps())
	if err != nil {
		return "", err
	}
	final, err := runShell(s)
	if err != nil {
		return "", err
	}
	return final.queueOpenCheckout(), nil
}

// RunFromRoutine opens the shell on the Routine dashboard.
func RunFromRoutine(d *routine.Deps) error {
	if d == nil {
		d = routine.DefaultDeps()
	}
	qd := queue.DefaultDeps()
	if d.LoadConfig != nil {
		load := d.LoadConfig
		qd.LoadConfig = func(string) (*config.Config, error) {
			return load()
		}
	}
	cfg, err := qd.LoadConfig(config.DefaultConfigPath())
	if err != nil {
		return err
	}
	s, err := newShell(ViewRoutine, qd, cfg, d)
	if err != nil {
		return err
	}
	_, err = runShell(s)
	return err
}

func newShell(start View, qd *queue.Deps, cfg *config.Config, rd *routine.Deps) (Shell, error) {
	if qd == nil {
		qd = queue.DefaultDeps()
	}
	if rd == nil {
		rd = routine.DefaultDeps()
	}
	if cfg == nil {
		var err error
		cfg, err = qd.LoadConfig(config.DefaultConfigPath())
		if err != nil {
			return Shell{}, err
		}
	}
	qSnap, err := queue.BuildDashboard(qd, cfg)
	if err != nil {
		return Shell{}, err
	}
	rSnap, err := routine.BuildDashboard(rd)
	if err != nil {
		return Shell{}, err
	}
	return Shell{
		active:  start,
		queue:   queue.NewDashboard(qd, cfg, qSnap),
		routine: routine.NewDashboard(rd, rSnap),
	}, nil
}

func runShell(s Shell) (Shell, error) {
	program := tea.NewProgram(s)
	final, err := program.Run()
	if err != nil {
		return Shell{}, err
	}
	if sh, ok := final.(Shell); ok {
		return sh, nil
	}
	return Shell{}, nil
}

func (s Shell) Init() tea.Cmd {
	return s.initActiveView()
}

func (s Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		s.width = msg.Width
		s.height = msg.Height
		var qCmd, rCmd tea.Cmd
		var qModel tea.Model
		qModel, qCmd = s.queue.Update(msg)
		s.queue = qModel.(queue.QueueDashboard)
		var rModel tea.Model
		rModel, rCmd = s.routine.Update(msg)
		s.routine = rModel.(routine.RoutineDashboard)
		return s, tea.Batch(qCmd, rCmd)
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "v" && s.activeViewToggleAllowed() {
			s.toggleActive()
			return s, s.initActiveView()
		}
	}

	var cmd tea.Cmd
	switch s.active {
	case ViewQueue:
		var updated tea.Model
		updated, cmd = s.queue.Update(msg)
		s.queue = updated.(queue.QueueDashboard)
	case ViewRoutine:
		var updated tea.Model
		updated, cmd = s.routine.Update(msg)
		s.routine = updated.(routine.RoutineDashboard)
	}
	return s, cmd
}

func (s Shell) View() tea.View {
	switch s.active {
	case ViewRoutine:
		return s.routine.View()
	default:
		return s.queue.View()
	}
}

func (s *Shell) toggleActive() {
	if s.active == ViewQueue {
		s.active = ViewRoutine
	} else {
		s.active = ViewQueue
	}
}

func (s Shell) activeViewToggleAllowed() bool {
	switch s.active {
	case ViewRoutine:
		return s.routine.ViewToggleAllowed()
	default:
		return s.queue.ViewToggleAllowed()
	}
}

func (s Shell) initActiveView() tea.Cmd {
	switch s.active {
	case ViewRoutine:
		return s.routine.Init()
	default:
		return s.queue.Init()
	}
}

func (s Shell) queueOpenCheckout() string {
	return s.queue.OpenCheckout()
}

// ActiveView exposes the current view for tests.
func (s Shell) ActiveView() View {
	return s.active
}

// QueueDashboard exposes the queue model for tests.
func (s Shell) QueueDashboard() queue.QueueDashboard {
	return s.queue
}

// RoutineDashboard exposes the routine model for tests.
func (s Shell) RoutineDashboard() routine.RoutineDashboard {
	return s.routine
}
